//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"
)

type winRect struct{ Left, Top, Right, Bottom int32 }
type monitorInfoEx struct {
	Size    uint32
	Monitor winRect
	Work    winRect
	Flags   uint32
	Device  [32]uint16
}

type displayDeviceW struct {
	Size         uint32
	DeviceName   [32]uint16
	DeviceString [128]uint16
	StateFlags   uint32
	DeviceID     [128]uint16
	DeviceKey    [128]uint16
}

type wmiMonitorID struct {
	InstanceName string `json:"instance_name"`
	Model        string `json:"model"`
	Manufacturer string `json:"manufacturer"`
}

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	procEnumDisplayMonitors = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW     = user32.NewProc("GetMonitorInfoW")
	procEnumDisplayDevicesW = user32.NewProc("EnumDisplayDevicesW")
)

func listMonitors() ([]Monitor, error) {
	wmi := queryWMIMonitorNames()
	var result []Monitor
	cb := syscall.NewCallback(func(hMon, hdc, rect, data uintptr) uintptr {
		var info monitorInfoEx
		info.Size = uint32(unsafe.Sizeof(info))
		ok, _, _ := procGetMonitorInfoW.Call(hMon, uintptr(unsafe.Pointer(&info)))
		if ok == 0 {
			return 1
		}
		name := syscall.UTF16ToString(info.Device[:])
		model, manufacturer, deviceID := displayIdentity(name, wmi)
		result = append(result, Monitor{
			Index: len(result), DisplayNumber: displayNumberFromName(name), Name: name, Model: model, Manufacturer: manufacturer, DeviceID: deviceID,
			X: int(info.Monitor.Left), Y: int(info.Monitor.Top),
			Width: int(info.Monitor.Right - info.Monitor.Left), Height: int(info.Monitor.Bottom - info.Monitor.Top),
			Primary: info.Flags&1 != 0, HMonitor: hMon, AdapterIndex: -1, OutputIndex: -1,
		})
		return 1
	})
	ok, _, err := procEnumDisplayMonitors.Call(0, 0, cb, 0)
	if ok == 0 {
		return nil, fmt.Errorf("EnumDisplayMonitors: %v", err)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("мониторы не найдены")
	}

	// EnumDisplayMonitors does not guarantee the same order as DXGI.  The old
	// implementation used the callback index as ddagrab output_idx, so frames
	// could be assigned to the wrong Windows coordinates.  Match every HMONITOR
	// to its real DXGI adapter/output before building xstack.
	if outputs, dxgiErr := enumerateDXGIOutputs(); dxgiErr == nil {
		for i := range result {
			for _, output := range outputs {
				if (result[i].HMonitor != 0 && output.Monitor == result[i].HMonitor) ||
					(strings.EqualFold(output.DeviceName, result[i].Name) && output.X == result[i].X && output.Y == result[i].Y) {
					result[i].AdapterIndex = output.AdapterIndex
					result[i].OutputIndex = output.OutputIndex
					break
				}
			}
		}
	}
	for i := range result {
		if result[i].AdapterIndex < 0 {
			result[i].AdapterIndex = 0
		}
		if result[i].OutputIndex < 0 {
			if result[i].DisplayNumber > 0 {
				result[i].OutputIndex = result[i].DisplayNumber - 1
			} else {
				result[i].OutputIndex = result[i].Index
			}
		}
	}
	return result, nil
}

func displayNumberFromName(name string) int {
	upper := strings.ToUpper(strings.TrimSpace(name))
	pos := strings.LastIndex(upper, "DISPLAY")
	if pos < 0 {
		return 0
	}
	n := 0
	for _, r := range upper[pos+len("DISPLAY"):] {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func displayIdentity(displayName string, wmi []wmiMonitorID) (model, manufacturer, deviceID string) {
	ptr, err := syscall.UTF16PtrFromString(displayName)
	if err != nil {
		return "", "", ""
	}
	for index := uint32(0); index < 8; index++ {
		var dd displayDeviceW
		dd.Size = uint32(unsafe.Sizeof(dd))
		ok, _, _ := procEnumDisplayDevicesW.Call(uintptr(unsafe.Pointer(ptr)), uintptr(index), uintptr(unsafe.Pointer(&dd)), 0)
		if ok == 0 {
			break
		}
		candidate := strings.TrimSpace(syscall.UTF16ToString(dd.DeviceString[:]))
		deviceID = strings.TrimSpace(syscall.UTF16ToString(dd.DeviceID[:]))
		code := monitorPNPCode(deviceID)
		for _, item := range wmi {
			if code != "" && code == monitorPNPCode(item.InstanceName) {
				if item.Model != "" {
					candidate = item.Model
				}
				manufacturer = item.Manufacturer
				break
			}
		}
		if !isGenericMonitorName(candidate) {
			return candidate, manufacturer, deviceID
		}
		if model == "" {
			model = candidate
		}
	}
	if isGenericMonitorName(model) {
		model = ""
	}
	return model, manufacturer, deviceID
}

func monitorPNPCode(value string) string {
	value = strings.ToUpper(strings.ReplaceAll(value, "/", `\`))
	parts := strings.Split(value, `\`)
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func isGenericMonitorName(value string) bool {
	v := strings.ToLower(strings.TrimSpace(value))
	return v == "" || strings.Contains(v, "generic pnp") || strings.Contains(v, "default monitor") || strings.Contains(v, "универсальный монитор") || strings.Contains(v, "монитор по умолчанию")
}

func queryWMIMonitorNames() []wmiMonitorID {
	const script = `$items = Get-CimInstance -Namespace root\wmi -ClassName WmiMonitorID -ErrorAction SilentlyContinue | ForEach-Object {` +
		`$n = -join ($_.UserFriendlyName | Where-Object { $_ -ne 0 } | ForEach-Object { [char]$_ });` +
		`$m = -join ($_.ManufacturerName | Where-Object { $_ -ne 0 } | ForEach-Object { [char]$_ });` +
		`[pscustomobject]@{instance_name=$_.InstanceName;model=$n;manufacturer=$m}};` +
		`@($items) | ConvertTo-Json -Compress`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	hideChildWindow(cmd)
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return nil
	}
	var result []wmiMonitorID
	if err := json.Unmarshal(out, &result); err == nil {
		return result
	}
	var one wmiMonitorID
	if err := json.Unmarshal(out, &one); err == nil && one.InstanceName != "" {
		return []wmiMonitorID{one}
	}
	return nil
}

func hideChildWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
