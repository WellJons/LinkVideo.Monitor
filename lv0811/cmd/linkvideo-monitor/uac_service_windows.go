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

	tokenAssignPrimary       = 0x0001
	tokenDuplicate           = 0x0002
	tokenQuery               = 0x0008
	tokenAdjustPrivileges    = 0x0020
	tokenAdjustSessionID     = 0x0100
	tokenAllForLaunch        = tokenAssignPrimary | tokenDuplicate | tokenQuery | tokenAdjustPrivileges | tokenAdjustSessionID
	securityImpersonation    = 2
	tokenPrimary             = 1
	tokenSessionIDClass      = 12
	sePrivilegeEnabled       = 0x00000002
	createNoWindow           = 0x08000000
	createUnicodeEnvironment = 0x00000400
	stillActive              = 259
	invalidSessionID         = 0xffffffff
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

// processEntry32W mirrors PROCESSENTRY32W for locating the Winlogon process
// that belongs to the interactive session. Reusing its token is more reliable
// than changing the session id on the service token: Windows creates the helper
// with the exact LOCAL_SYSTEM/session security context of Winlogon.
type processEntry32W struct {
	Size              uint32
	Usage             uint32
	ProcessID         uint32
	DefaultHeapID     uintptr
	ModuleID          uint32
	Threads           uint32
	ParentProcessID   uint32
	PriorityClassBase int32
	Flags             uint32
	ExeFile           [260]uint16
}

var (
	advapi32Service                         = syscall.NewLazyDLL("advapi32.dll")
	procStartServiceCtrlDispatcherW         = advapi32Service.NewProc("StartServiceCtrlDispatcherW")
	procRegisterServiceCtrlHandlerExW       = advapi32Service.NewProc("RegisterServiceCtrlHandlerExW")
	procSetServiceStatusW                   = advapi32Service.NewProc("SetServiceStatus")
	procOpenProcessTokenService             = advapi32Service.NewProc("OpenProcessToken")
	procDuplicateTokenExService             = advapi32Service.NewProc("DuplicateTokenEx")
	procSetTokenInformationService          = advapi32Service.NewProc("SetTokenInformation")
	procLookupPrivilegeValueWService        = advapi32Service.NewProc("LookupPrivilegeValueW")
	procAdjustTokenPrivilegesService        = advapi32Service.NewProc("AdjustTokenPrivileges")
	procCreateProcessAsUserWService         = advapi32Service.NewProc("CreateProcessAsUserW")
	wtsapi32Service                         = syscall.NewLazyDLL("wtsapi32.dll")
	procWTSQueryUserTokenService            = wtsapi32Service.NewProc("WTSQueryUserToken")
	userenvService                          = syscall.NewLazyDLL("userenv.dll")
	procCreateEnvironmentBlockService       = userenvService.NewProc("CreateEnvironmentBlock")
	procDestroyEnvironmentBlockService      = userenvService.NewProc("DestroyEnvironmentBlock")
	procGetCurrentProcessService            = kernel32Service.NewProc("GetCurrentProcess")
	procWTSGetActiveConsoleSessionIDService = kernel32Service.NewProc("WTSGetActiveConsoleSessionId")
	procGetExitCodeProcessService           = kernel32Service.NewProc("GetExitCodeProcess")
	procTerminateProcessService             = kernel32Service.NewProc("TerminateProcess")
	procCloseHandleService                  = kernel32Service.NewProc("CloseHandle")
	procCreateToolhelp32SnapshotService     = kernel32Service.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstWService              = kernel32Service.NewProc("Process32FirstW")
	procProcess32NextWService               = kernel32Service.NewProc("Process32NextW")
	kernel32Service                         = syscall.NewLazyDLL("kernel32.dll")
	serviceStopCh                           = make(chan struct{})
	serviceStopOnce                         sync.Once
	serviceStatusHandle                     uintptr
	serviceMainCallback                     = syscall.NewCallback(serviceMainEntry)
	serviceHandlerCallback                  = syscall.NewCallback(serviceControlHandler)
)

type secureAgentProcess struct {
	process   uintptr
	pid       uint32
	key       string
	sessionID uint32
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
	var backgroundAgent *secureAgentProcess
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	defer func() {
		for _, agent := range agents {
			stopSecureAgent(agent)
		}
		stopSecureAgent(backgroundAgent)
	}()

	serviceLog("service started")
	for {
		select {
		case <-stop:
			serviceLog("service stop requested")
			return nil
		case <-ticker.C:
			activeSession := activeConsoleSessionID()
			if backgroundAgent != nil {
				if !processStillRunning(backgroundAgent.process) || (activeSession != invalidSessionID && backgroundAgent.sessionID != activeSession) {
					stopSecureAgent(backgroundAgent)
					backgroundAgent = nil
				}
			}
			if backgroundAgent == nil && activeSession != invalidSessionID && !runningInstanceAvailable() {
				appPath, pathErr := loadInstalledAppPath()
				if pathErr != nil {
					serviceLog("background agent path: " + pathErr.Error())
				} else if agent, launchErr := launchBackgroundAgent(activeSession, appPath); launchErr != nil {
					serviceLog(fmt.Sprintf("background agent session %d: %v", activeSession, launchErr))
				} else {
					backgroundAgent = agent
					serviceLog(fmt.Sprintf("background agent started: session=%d pid=%d", activeSession, agent.pid))
				}
			}

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
		nameID := strings.TrimSuffix(strings.TrimPrefix(entry.Name(), "session-"), ".json")
		parsedID, parseErr := strconv.ParseUint(nameID, 10, 32)
		info, statErr := entry.Info()
		if parseErr != nil || statErr != nil || info.Size() < 2 || info.Size() > 64<<10 {
			_ = os.Remove(path)
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var req secureCaptureRequest
		if json.Unmarshal(data, &req) != nil || req.SessionID != uint32(parsedID) || !validSecureCaptureRequest(req) {
			_ = os.Remove(path)
			continue
		}
		age := now.Sub(time.UnixMilli(req.UpdatedUnix))
		if age > 8*time.Second || age < -2*time.Second {
			_ = os.Remove(path)
			continue
		}
		result[req.SessionID] = req
	}
	return result
}

func validSecureCaptureRequest(req secureCaptureRequest) bool {
	expectedMapping := fmt.Sprintf(`Local\LinkVideoMonitorSecure_%d`, req.SessionID)
	if req.SessionID == 0xffffffff || req.ClientPID == 0 || !strings.EqualFold(req.MappingName, expectedMapping) {
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
		req.MappingName, strconv.FormatUint(uint64(req.ClientPID), 10), strconv.Itoa(req.X), strconv.Itoa(req.Y), strconv.Itoa(req.Width), strconv.Itoa(req.Height),
		strconv.Itoa(req.OutputWidth), strconv.Itoa(req.OutputHeight), strconv.Itoa(req.FPS),
		strconv.FormatBool(req.Cursor), strconv.FormatBool(req.Privacy),
	}, "|")
}

func isLinkVideoMonitorProcessName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "linkvideo.monitor.exe" {
		return true
	}
	return strings.HasPrefix(name, "linkvideo.monitor_") && strings.HasSuffix(name, ".exe")
}

func launchSecureAgent(req secureCaptureRequest) (*secureAgentProcess, error) {
	var clientSession uint32
	ok, _, callErr := procProcessIdToSessionIdSecure.Call(uintptr(req.ClientPID), uintptr(unsafe.Pointer(&clientSession)))
	if ok == 0 || clientSession != req.SessionID {
		return nil, fmt.Errorf("requesting process is not in session %d", req.SessionID)
	}
	if !isLinkVideoMonitorProcessName(processImageName(req.ClientPID)) {
		return nil, errors.New("secure capture request was not created by LinkVideo Monitor")
	}

	// Launch the installed application rather than the service copy. FFmpeg is
	// installed next to this executable and is used by the DXGI secure helper.
	exe, err := loadInstalledAppPath()
	if err != nil {
		exe, err = os.Executable()
		if err != nil {
			return nil, err
		}
	}

	primaryToken, err := duplicateWinlogonPrimaryToken(req.SessionID)
	if err != nil {
		serviceLog(fmt.Sprintf("session %d: Winlogon token unavailable, using service token: %v", req.SessionID, err))
		primaryToken, err = duplicateServicePrimaryTokenForSession(req.SessionID)
		if err != nil {
			return nil, err
		}
	}
	defer procCloseHandleService.Call(primaryToken)

	args := []string{
		"--secure-desktop-capture", req.MappingName,
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

	var environment uintptr
	creationFlags := uintptr(createNoWindow)
	if envOK, _, _ := procCreateEnvironmentBlockService.Call(uintptr(unsafe.Pointer(&environment)), primaryToken, 0); envOK != 0 && environment != 0 {
		defer procDestroyEnvironmentBlockService.Call(environment)
		creationFlags |= createUnicodeEnvironment
	}

	var pi processInformation
	ok, _, callErr = procCreateProcessAsUserWService.Call(
		primaryToken,
		uintptr(unsafe.Pointer(appPtr)),
		uintptr(unsafe.Pointer(&cmdBuf[0])),
		0, 0, 0,
		creationFlags,
		environment,
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
	return &secureAgentProcess{process: pi.Process, pid: pi.ProcessID, key: requestKey(req), sessionID: req.SessionID}, nil
}

func duplicateWinlogonPrimaryToken(sessionID uint32) (uintptr, error) {
	pid, err := findSessionWinlogonPID(sessionID)
	if err != nil {
		return 0, err
	}
	process, _, callErr := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if process == 0 {
		return 0, fmt.Errorf("OpenProcess(winlogon %d): %v", pid, callErr)
	}
	defer procCloseHandle.Call(process)

	var token uintptr
	ok, _, callErr := procOpenProcessTokenService.Call(process, tokenDuplicate|tokenQuery|tokenAssignPrimary, uintptr(unsafe.Pointer(&token)))
	if ok == 0 || token == 0 {
		return 0, fmt.Errorf("OpenProcessToken(winlogon %d): %v", pid, callErr)
	}
	defer procCloseHandleService.Call(token)

	var primary uintptr
	ok, _, callErr = procDuplicateTokenExService.Call(
		token, tokenAllForLaunch, 0, securityImpersonation, tokenPrimary, uintptr(unsafe.Pointer(&primary)),
	)
	if ok == 0 || primary == 0 {
		return 0, fmt.Errorf("DuplicateTokenEx(winlogon %d): %v", pid, callErr)
	}
	return primary, nil
}

func duplicateServicePrimaryTokenForSession(sessionID uint32) (uintptr, error) {
	currentProcess, _, _ := procGetCurrentProcessService.Call()
	var processToken uintptr
	ok, _, callErr := procOpenProcessTokenService.Call(currentProcess, tokenAllForLaunch, uintptr(unsafe.Pointer(&processToken)))
	if ok == 0 || processToken == 0 {
		return 0, fmt.Errorf("OpenProcessToken(service): %v", callErr)
	}
	defer procCloseHandleService.Call(processToken)

	var primaryToken uintptr
	ok, _, callErr = procDuplicateTokenExService.Call(
		processToken, tokenAllForLaunch, 0, securityImpersonation, tokenPrimary, uintptr(unsafe.Pointer(&primaryToken)),
	)
	if ok == 0 || primaryToken == 0 {
		return 0, fmt.Errorf("DuplicateTokenEx(service): %v", callErr)
	}
	ok, _, callErr = procSetTokenInformationService.Call(primaryToken, tokenSessionIDClass, uintptr(unsafe.Pointer(&sessionID)), unsafe.Sizeof(sessionID))
	if ok == 0 {
		procCloseHandleService.Call(primaryToken)
		return 0, fmt.Errorf("SetTokenInformation(TokenSessionId): %v", callErr)
	}
	return primaryToken, nil
}

func findSessionWinlogonPID(sessionID uint32) (uint32, error) {
	snapshot, _, callErr := procCreateToolhelp32SnapshotService.Call(0x00000002, 0) // TH32CS_SNAPPROCESS
	if snapshot == 0 || snapshot == ^uintptr(0) {
		return 0, fmt.Errorf("CreateToolhelp32Snapshot: %v", callErr)
	}
	defer procCloseHandleService.Call(snapshot)

	entry := processEntry32W{Size: uint32(unsafe.Sizeof(processEntry32W{}))}
	ok, _, callErr := procProcess32FirstWService.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	for ok != 0 {
		if strings.EqualFold(syscall.UTF16ToString(entry.ExeFile[:]), "winlogon.exe") {
			var candidateSession uint32
			if valid, _, _ := procProcessIdToSessionIdSecure.Call(uintptr(entry.ProcessID), uintptr(unsafe.Pointer(&candidateSession))); valid != 0 && candidateSession == sessionID {
				return entry.ProcessID, nil
			}
		}
		entry.Size = uint32(unsafe.Sizeof(processEntry32W{}))
		ok, _, callErr = procProcess32NextWService.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	}
	return 0, fmt.Errorf("winlogon.exe для сеанса %d не найден: %v", sessionID, callErr)
}

func activeConsoleSessionID() uint32 {
	id, _, _ := procWTSGetActiveConsoleSessionIDService.Call()
	return uint32(id)
}

func installedAppPathFile() string {
	return filepath.Join(os.Getenv("PROGRAMDATA"), "LinkVideo.Monitor", "Service", "app-path.txt")
}

func loadInstalledAppPath() (string, error) {
	pathFile := installedAppPathFile()
	data, err := os.ReadFile(pathFile)
	if err != nil {
		return "", err
	}
	appPath := strings.TrimSpace(string(data))
	if !filepath.IsAbs(appPath) || !strings.EqualFold(filepath.Base(appPath), "LinkVideo.Monitor.exe") {
		return "", errors.New("invalid installed application path")
	}
	if info, err := os.Stat(appPath); err != nil || info.IsDir() {
		if err != nil {
			return "", err
		}
		return "", errors.New("installed application path is a directory")
	}
	return appPath, nil
}

func launchBackgroundAgent(sessionID uint32, appPath string) (*secureAgentProcess, error) {
	if sessionID == invalidSessionID {
		return nil, errors.New("interactive Windows session is unavailable")
	}
	var userToken uintptr
	ok, _, callErr := procWTSQueryUserTokenService.Call(uintptr(sessionID), uintptr(unsafe.Pointer(&userToken)))
	if ok == 0 || userToken == 0 {
		return nil, fmt.Errorf("WTSQueryUserToken: %v", callErr)
	}
	defer procCloseHandleService.Call(userToken)

	var primaryToken uintptr
	ok, _, callErr = procDuplicateTokenExService.Call(
		userToken, tokenAllForLaunch, 0, securityImpersonation, tokenPrimary, uintptr(unsafe.Pointer(&primaryToken)),
	)
	if ok == 0 || primaryToken == 0 {
		return nil, fmt.Errorf("DuplicateTokenEx(user): %v", callErr)
	}
	defer procCloseHandleService.Call(primaryToken)

	var environment uintptr
	ok, _, callErr = procCreateEnvironmentBlockService.Call(uintptr(unsafe.Pointer(&environment)), primaryToken, 0)
	if ok == 0 {
		return nil, fmt.Errorf("CreateEnvironmentBlock: %v", callErr)
	}
	defer procDestroyEnvironmentBlockService.Call(environment)

	commandLine := quoteWindowsCommand([]string{appPath, "--background"})
	cmdBuf, _ := syscall.UTF16FromString(commandLine)
	appPtr, _ := syscall.UTF16PtrFromString(appPath)
	desktopPtr, _ := syscall.UTF16PtrFromString(`winsta0\default`)
	dirPtr, _ := syscall.UTF16PtrFromString(filepath.Dir(appPath))
	si := startupInfoW{CB: uint32(unsafe.Sizeof(startupInfoW{})), Desktop: desktopPtr}
	var pi processInformation
	ok, _, callErr = procCreateProcessAsUserWService.Call(
		primaryToken,
		uintptr(unsafe.Pointer(appPtr)),
		uintptr(unsafe.Pointer(&cmdBuf[0])),
		0, 0, 0,
		createUnicodeEnvironment,
		environment,
		uintptr(unsafe.Pointer(dirPtr)),
		uintptr(unsafe.Pointer(&si)),
		uintptr(unsafe.Pointer(&pi)),
	)
	if ok == 0 {
		return nil, fmt.Errorf("CreateProcessAsUser(background): %v", callErr)
	}
	if pi.Thread != 0 {
		procCloseHandleService.Call(pi.Thread)
	}
	return &secureAgentProcess{process: pi.Process, pid: pi.ProcessID, key: "background", sessionID: sessionID}, nil
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
	for _, name := range []string{"SeAssignPrimaryTokenPrivilege", "SeIncreaseQuotaPrivilege", "SeTcbPrivilege", "SeDebugPrivilege"} {
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
