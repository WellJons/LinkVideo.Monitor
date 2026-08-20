from pathlib import Path


def text(path): return Path(path).read_text(encoding='utf-8')
def write(path, s): Path(path).write_text(s, encoding='utf-8')

def between(path, start, end, replacement=''):
    s=text(path); i=s.find(start); j=s.find(end, i)
    if i<0 or j<0: raise SystemExit(f'{path}: marker not found: {start!r} / {end!r}')
    write(path, s[:i]+replacement+s[j:])

def once(path, old, new):
    s=text(path)
    if s.count(old)!=1: raise SystemExit(f'{path}: expected one match for {old!r}, got {s.count(old)}')
    write(path, s.replace(old,new,1))

# Superseded encoder selection path; runtime_encoder_fallback.go is the sole
# selection/performance path now.
between('cmd/linkvideo-monitor/encoder_auto.go', 'func (a *app) selectVideoEncoder', 'func automaticEncoderCandidates')
between('cmd/linkvideo-monitor/encoder_auto.go', 'func (a *app) setVideoEncoder', 'func (a *app) setCaptureBackend')

# Legacy wrappers/features no longer reachable from the product UI.
between('cmd/linkvideo-monitor/gdi_capture_windows.go', 'func runSecureDesktopGDICapture', 'func runGDICaptureInternal')
between('cmd/linkvideo-monitor/main.go', 'func (a *app) watchCaptureTarget', 'func (a *app) runLoop')
between('cmd/linkvideo-monitor/main.go', 'func findWindowForConfig', 'func selectedMonitor')
once('cmd/linkvideo-monitor/privacy_rules.go', '\nfunc containsAnyPrivacy(value string, terms ...string) bool {\n\treturn privacyIDMatchesAny(normalizePrivacyText(value), terms...)\n}\n', '\n')

# Remove the now-unused top-level-window discovery subsystem. Process image
# lookup is kept because other Windows features still use it.
p='cmd/linkvideo-monitor/platform_windows.go'; s=text(p)
s=s.replace('\t"sort"\n','')
s=s.replace('\n\tgwOwner           = 4\n\tappWsExToolWindow = 0x00000080\n\twsExAppWindow     = 0x00040000\n\tdwmwaCloaked      = 14\n','\n')
for line in [
'\tprocEnumWindows              = user32.NewProc("EnumWindows")\n',
'\tprocIsWindowVisible          = user32.NewProc("IsWindowVisible")\n',
'\tprocGetWindowTextLengthW     = user32.NewProc("GetWindowTextLengthW")\n',
'\tprocGetWindowTextW           = user32.NewProc("GetWindowTextW")\n',
'\tprocGetWindowRect            = user32.NewProc("GetWindowRect")\n',
'\tprocGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")\n',
'\tprocGetWindowLongPtrW        = user32.NewProc("GetWindowLongPtrW")\n',
'\tprocGetWindow                = user32.NewProc("GetWindow")\n',
'\tprocGetClassNameW            = user32.NewProc("GetClassNameW")\n',
'\n\tdwmapi                    = syscall.NewLazyDLL("dwmapi.dll")\n\tprocDwmGetWindowAttribute = dwmapi.NewProc("DwmGetWindowAttribute")\n',
]: s=s.replace(line,'')
i=s.find('func windowClass('); j=s.find('func processImageName(',i)
if i<0 or j<0: raise SystemExit('platform: windowClass markers missing')
s=s[:i]+s[j:]
i=s.find('func isCloakedWindow('); j=s.find('func runUninstaller(',i)
if i<0 or j<0: raise SystemExit('platform: listWindows markers missing')
s=s[:i]+s[j:]
write(p,s)

# Staticcheck correctness findings and clearly unused Win32 declarations.
once('cmd/linkvideo-monitor/product_features_test.go', '\titems := a.appendReconnectEventLocked("сервер закрыл RTSP-соединение", 32, time.Date(2026, 7, 28, 12, 0, 0, 0, time.Local))\n\titems = a.appendReconnectEventLocked("сеть временно недоступна", 1, time.Date(2026, 7, 28, 12, 5, 0, 0, time.Local))\n', '\t_ = a.appendReconnectEventLocked("сервер закрыл RTSP-соединение", 32, time.Date(2026, 7, 28, 12, 0, 0, 0, time.Local))\n\titems := a.appendReconnectEventLocked("сеть временно недоступна", 1, time.Date(2026, 7, 28, 12, 5, 0, 0, time.Local))\n')
once('cmd/linkvideo-monitor/secure_capture_windows.go', 'ioErrShortBuffer', 'errShortBuffer')
once('cmd/linkvideo-monitor/uac_service_windows.go', '\nfunc isWindowsService() bool {\n\treturn len(os.Args) > 1 && os.Args[1] == "--uac-service"\n}\n', '\n')
once('cmd/linkvideo-monitor/uac_service_windows.go', 'ok, _, callErr := procProcessIdToSessionIdSecure.Call', 'ok, _, _ := procProcessIdToSessionIdSecure.Call')
once('cmd/linkvideo-monitor/windows_ui_windows.go', '\n\thtTransparent = -1\n', '\n')
for line in [
'\tprocSetTimer                   = user32.NewProc("SetTimer")\n',
'\tprocKillTimer                  = user32.NewProc("KillTimer")\n',
'\tprocEllipse            = gdi32.NewProc("Ellipse")\n',
]:
    s=text('cmd/linkvideo-monitor/windows_ui_windows.go'); write('cmd/linkvideo-monitor/windows_ui_windows.go', s.replace(line,''))

# Russian error strings are deliberately user-facing and start with a capital
# letter, so ST1005 is not a useful quality rule for this product. Keep all
# correctness/performance checks, only exclude that style diagnostic.
p='.github/workflows/full-audit.yml'; s=text(p)
s=s.replace('run: staticcheck ./cmd/linkvideo-monitor','run: staticcheck -checks=all,-ST1005 ./cmd/linkvideo-monitor')
s=s.replace('run: staticcheck ./...','run: staticcheck -checks=all,-ST1005 ./...')
write(p,s)
