//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	uacServiceName = "LinkVideoMonitorCapture"

	serviceWin32OwnProcess = 0x00000010
	serviceStopped         = 0x00000001
	serviceStartPending    = 0x00000002
	serviceStopPending     = 0x00000003
	serviceRunning         = 0x00000004
	serviceAcceptStop      = 0x00000001
	serviceAcceptShutdown  = 0x00000004
	serviceControlStop     = 0x00000001
	serviceControlShutdown = 0x00000005
	errorServiceSpecific   = 1066

	tokenAssignPrimary    = 0x0001
	tokenDuplicate        = 0x0002
	tokenQuery            = 0x0008
	tokenAdjustPrivileges = 0x0020
	tokenAdjustSessionID  = 0x0100
	tokenAllForLaunch     = tokenAssignPrimary | tokenDuplicate | tokenQuery | tokenAdjustPrivileges | tokenAdjustSessionID
	securityImpersonation = 2
	tokenPrimary          = 1
	tokenSessionIDClass   = 12
	sePrivilegeEnabled    = 0x00000002
	createNoWindow        = 0x08000000
	stillActive           = 259
)

type serviceTableEntryW struct {
	Name *uint16
	Proc uintptr
}

type serviceStatusW struct {
	ServiceType             uint32
	CurrentState            uint32
	ControlsAccepted        uint32
	Win32ExitCode           uint32
	ServiceSpecificExitCode uint32
	CheckPoint              uint32
	WaitHint                uint32
}

type luid struct {
	LowPart  uint32
	HighPart int32
}

type luidAndAttributes struct {
	Luid       luid
	Attributes uint32
}

type tokenPrivileges struct {
	PrivilegeCount uint32
	Privileges     [1]luidAndAttributes
}

type startupInfoW struct {
	CB            uint32
	Reserved      *uint16
	Desktop       *uint16
	Title         *uint16
	X             uint32
	Y             uint32
	XSize         uint32
	YSize         uint32
	XCountChars   uint32
	YCountChars   uint32
	FillAttribute uint32
	Flags         uint32
	ShowWindow    uint16
	CBReserved2   uint16
	Reserved2     *byte
	StdInput      uintptr
	StdOutput     uintptr
	StdError      uintptr
}

type processInformation struct {
	Process   uintptr
	Thread    uintptr
	ProcessID uint32
	ThreadID  uint32
}

var (
	advapi32Service                   = syscall.NewLazyDLL("advapi32.dll")
	procStartServiceCtrlDispatcherW   = advapi32Service.NewProc("StartServiceCtrlDispatcherW")
	procRegisterServiceCtrlHandlerExW = advapi32Service.NewProc("RegisterServiceCtrlHandlerExW")
	procSetServiceStatusW             = advapi32Service.NewProc("SetServiceStatus")
	procOpenProcessTokenService       = advapi32Service.NewProc("OpenProcessToken")
	procDuplicateTokenExService       = advapi32Service.NewProc("DuplicateTokenEx")
	procSetTokenInformationService    = advapi32Service.NewProc("SetTokenInformation")
	procLookupPrivilegeValueWService  = advapi32Service.NewProc("LookupPrivilegeValueW")
	procAdjustTokenPrivilegesService  = advapi32Service.NewProc("AdjustTokenPrivileges")
	procCreateProcessAsUserWService   = advapi32Service.NewProc("CreateProcessAsUserW")
	procGetCurrentProcessService      = kernel32Service.NewProc("GetCurrentProcess")
	procGetExitCodeProcessService     = kernel32Service.NewProc("GetExitCodeProcess")
	procTerminateProcessService       = kernel32Service.NewProc("TerminateProcess")
	procCloseHandleService            = kernel32Service.NewProc("CloseHandle")
	kernel32Service                   = syscall.NewLazyDLL("kernel32.dll")
	serviceStopCh                     = make(chan struct{})
	serviceStopOnce                   sync.Once
	serviceStatusHandle               uintptr
	serviceMainCallback               = syscall.NewCallback(serviceMainEntry)
	serviceHandlerCallback            = syscall.NewCallback(serviceControlHandler)
)

type secureAgentProcess struct {
	process uintptr
	pid     uint32
	key     string
}

func isWindowsService() bool {
	return len(os.Args) > 1 && os.Args[1] == "--uac-service"
}

func runUACService() error {
	name, _ := syscall.UTF16PtrFromString(uacServiceName)
	table := []serviceTableEntryW{{Name: name, Proc: serviceMainCallback}, {}}
	ok, _, callErr := procStartServiceCtrlDispatcherW.Call(uintptr(unsafe.Pointer(&table[0])))
	if ok == 0 {
		return fmt.Errorf("StartServiceCtrlDispatcher: %v", callErr)
	}
	return nil
}

func serviceMainEntry(_ uintptr, _ uintptr) uintptr {
	name, _ := syscall.UTF16PtrFromString(uacServiceName)
	handle, _, _ := procRegisterServiceCtrlHandlerExW.Call(uintptr(unsafe.Pointer(name)), serviceHandlerCallback, 0)
	if handle == 0 {
		return 0
	}
	serviceStatusHandle = handle
	setServiceState(serviceStartPending, 0, 5000, 1)
	setServiceState(serviceRunning, serviceAcceptStop|serviceAcceptShutdown, 0, 0)

	err := serviceWorker(serviceStopCh)
	if err != nil {
		serviceLog("service stopped with error: " + err.Error())
		setServiceState(serviceStopped, 0, 0, 0)
		return 0
	}
	setServiceState(serviceStopped, 0, 0, 0)
	return 0
}

func serviceControlHandler(control, _ uintptr, _ uintptr, _ uintptr) uintptr {
	switch uint32(control) {
	case serviceControlStop, serviceControlShutdown:
		setServiceState(serviceStopPending, 0, 5000, 1)
		serviceStopOnce.Do(func() { close(serviceStopCh) })
	}
	return 0
}

func setServiceState(state, accepted, waitHint, checkPoint uint32) {
	if serviceStatusHandle == 0 {
		return
	}
	status := serviceStatusW{
		ServiceType: serviceWin32OwnProcess, CurrentState: state,
		ControlsAccepted: accepted, WaitHint: waitHint, CheckPoint: checkPoint,
	}
	procSetServiceStatusW.Call(serviceStatusHandle, uintptr(unsafe.Pointer(&status)))
}

func serviceWorker(stop <-chan struct{}) error {
	if err := enableLaunchPrivileges(); err != nil {
		serviceLog("privilege warning: " + err.Error())
	}
	sessionsDir := filepath.Join(os.Getenv("PROGRAMDATA"), "LinkVideo.Monitor", "Sessions")
	if err := os.MkdirAll(sessionsDir, 0o777); err != nil {
		return err
	}
	agents := make(map[uint32]*secureAgentProcess)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	defer func() {
		for _, agent := range agents {
			stopSecureAgent(agent)
		}
	}()

	serviceLog("service started")
	for {
		select {
		case <-stop:
			serviceLog("service stop requested")
			return nil
		case <-ticker.C:
			requests := loadSecureCaptureRequests(sessionsDir)
			for sessionID, agent := range agents {
				req, ok := requests[sessionID]
				if !ok || requestKey(req) != agent.key || !processStillRunning(agent.process) {
					stopSecureAgent(agent)
					delete(agents, sessionID)
				}
			}
			ids := make([]int, 0, len(requests))
			for id := range requests {
				ids = append(ids, int(id))
			}
			sort.Ints(ids)
			for _, rawID := range ids {
				sessionID := uint32(rawID)
				if _, ok := agents[sessionID]; ok {
					continue
				}
				req := requests[sessionID]
				agent, err := launchSecureAgent(req)
				if err != nil {
					serviceLog(fmt.Sprintf("session %d: %v", sessionID, err))
					continue
				}
				agents[sessionID] = agent
				serviceLog(fmt.Sprintf("secure capture agent started: session=%d pid=%d", sessionID, agent.pid))
			}
		}
	}
}

func loadSecureCaptureRequests(dir string) map[uint32]secureCaptureRequest {
	result := make(map[uint32]secureCaptureRequest)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return result
	}
	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "session-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var req secureCaptureRequest
		if json.Unmarshal(data, &req) != nil || !validSecureCaptureRequest(req) {
			continue
		}
		if now.Sub(time.UnixMilli(req.UpdatedUnix)) > 8*time.Second {
			_ = os.Remove(path)
			continue
		}
		result[req.SessionID] = req
	}
	return result
}

func validSecureCaptureRequest(req secureCaptureRequest) bool {
	if req.SessionID == 0xffffffff || !strings.HasPrefix(req.MappingName, `Local\LinkVideoMonitorSecure_`) {
		return false
	}
	if req.Width < 2 || req.Height < 2 || req.OutputWidth < 2 || req.OutputHeight < 2 {
		return false
	}
	if req.Width > 32768 || req.Height > 32768 || req.OutputWidth > 8192 || req.OutputHeight > 8192 {
		return false
	}
	return req.FPS >= 1 && req.FPS <= 30
}

func requestKey(req secureCaptureRequest) string {
	return strings.Join([]string{
		req.MappingName, strconv.Itoa(req.X), strconv.Itoa(req.Y), strconv.Itoa(req.Width), strconv.Itoa(req.Height),
		strconv.Itoa(req.OutputWidth), strconv.Itoa(req.OutputHeight), strconv.Itoa(req.FPS),
		strconv.FormatBool(req.Cursor), strconv.FormatBool(req.Privacy),
	}, "|")
}

func launchSecureAgent(req secureCaptureRequest) (*secureAgentProcess, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	currentProcess, _, _ := procGetCurrentProcessService.Call()
	var processToken uintptr
	ok, _, callErr := procOpenProcessTokenService.Call(currentProcess, tokenAllForLaunch, uintptr(unsafe.Pointer(&processToken)))
	if ok == 0 {
		return nil, fmt.Errorf("OpenProcessToken: %v", callErr)
	}
	defer procCloseHandleService.Call(processToken)

	var primaryToken uintptr
	ok, _, callErr = procDuplicateTokenExService.Call(
		processToken, tokenAllForLaunch, 0, securityImpersonation, tokenPrimary, uintptr(unsafe.Pointer(&primaryToken)),
	)
	if ok == 0 {
		return nil, fmt.Errorf("DuplicateTokenEx: %v", callErr)
	}
	defer procCloseHandleService.Call(primaryToken)

	sessionID := req.SessionID
	ok, _, callErr = procSetTokenInformationService.Call(primaryToken, tokenSessionIDClass, uintptr(unsafe.Pointer(&sessionID)), unsafe.Sizeof(sessionID))
	if ok == 0 {
		return nil, fmt.Errorf("SetTokenInformation(TokenSessionId): %v", callErr)
	}

	args := []string{
		"--secure-gdi-capture", req.MappingName,
		strconv.Itoa(req.X), strconv.Itoa(req.Y), strconv.Itoa(req.Width), strconv.Itoa(req.Height),
		strconv.Itoa(req.OutputWidth), strconv.Itoa(req.OutputHeight), strconv.Itoa(req.FPS),
		boolText(req.Cursor), boolText(req.Privacy),
	}
	commandLine := quoteWindowsCommand(append([]string{exe}, args...))
	cmdBuf, _ := syscall.UTF16FromString(commandLine)
	appPtr, _ := syscall.UTF16PtrFromString(exe)
	desktopPtr, _ := syscall.UTF16PtrFromString(`winsta0\Winlogon`)
	dirPtr, _ := syscall.UTF16PtrFromString(filepath.Dir(exe))
	si := startupInfoW{CB: uint32(unsafe.Sizeof(startupInfoW{})), Desktop: desktopPtr}
	var pi processInformation
	ok, _, callErr = procCreateProcessAsUserWService.Call(
		primaryToken,
		uintptr(unsafe.Pointer(appPtr)),
		uintptr(unsafe.Pointer(&cmdBuf[0])),
		0, 0, 0,
		createNoWindow,
		0,
		uintptr(unsafe.Pointer(dirPtr)),
		uintptr(unsafe.Pointer(&si)),
		uintptr(unsafe.Pointer(&pi)),
	)
	if ok == 0 {
		return nil, fmt.Errorf("CreateProcessAsUser on Winlogon desktop: %v", callErr)
	}
	if pi.Thread != 0 {
		procCloseHandleService.Call(pi.Thread)
	}
	return &secureAgentProcess{process: pi.Process, pid: pi.ProcessID, key: requestKey(req)}, nil
}

func quoteWindowsCommand(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		if arg == "" {
			quoted[i] = `""`
			continue
		}
		if !strings.ContainsAny(arg, " \t\"") {
			quoted[i] = arg
			continue
		}
		var b strings.Builder
		b.WriteByte('"')
		slashes := 0
		for _, r := range arg {
			if r == '\\' {
				slashes++
				continue
			}
			if r == '"' {
				b.WriteString(strings.Repeat("\\", slashes*2+1))
				b.WriteRune(r)
				slashes = 0
				continue
			}
			b.WriteString(strings.Repeat("\\", slashes))
			slashes = 0
			b.WriteRune(r)
		}
		b.WriteString(strings.Repeat("\\", slashes*2))
		b.WriteByte('"')
		quoted[i] = b.String()
	}
	return strings.Join(quoted, " ")
}

func processStillRunning(handle uintptr) bool {
	if handle == 0 {
		return false
	}
	var code uint32
	ok, _, _ := procGetExitCodeProcessService.Call(handle, uintptr(unsafe.Pointer(&code)))
	return ok != 0 && code == stillActive
}

func stopSecureAgent(agent *secureAgentProcess) {
	if agent == nil || agent.process == 0 {
		return
	}
	if processStillRunning(agent.process) {
		procTerminateProcessService.Call(agent.process, 0)
	}
	procCloseHandleService.Call(agent.process)
	agent.process = 0
}

func enableLaunchPrivileges() error {
	currentProcess, _, _ := procGetCurrentProcessService.Call()
	var token uintptr
	ok, _, callErr := procOpenProcessTokenService.Call(currentProcess, tokenQuery|tokenAdjustPrivileges, uintptr(unsafe.Pointer(&token)))
	if ok == 0 {
		return fmt.Errorf("OpenProcessToken privileges: %v", callErr)
	}
	defer procCloseHandleService.Call(token)
	var problems []string
	for _, name := range []string{"SeAssignPrimaryTokenPrivilege", "SeIncreaseQuotaPrivilege", "SeTcbPrivilege"} {
		namePtr, _ := syscall.UTF16PtrFromString(name)
		var id luid
		ok, _, callErr = procLookupPrivilegeValueWService.Call(0, uintptr(unsafe.Pointer(namePtr)), uintptr(unsafe.Pointer(&id)))
		if ok == 0 {
			problems = append(problems, name+": "+callErr.Error())
			continue
		}
		tp := tokenPrivileges{PrivilegeCount: 1}
		tp.Privileges[0] = luidAndAttributes{Luid: id, Attributes: sePrivilegeEnabled}
		ok, _, callErr = procAdjustTokenPrivilegesService.Call(token, 0, uintptr(unsafe.Pointer(&tp)), 0, 0, 0)
		if ok == 0 {
			problems = append(problems, name+": "+callErr.Error())
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func serviceLog(line string) {
	root := filepath.Join(os.Getenv("PROGRAMDATA"), "LinkVideo.Monitor")
	_ = os.MkdirAll(root, 0o755)
	f, err := os.OpenFile(filepath.Join(root, "uac-service.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%s %s\r\n", time.Now().Format("2006-01-02 15:04:05"), line)
}
