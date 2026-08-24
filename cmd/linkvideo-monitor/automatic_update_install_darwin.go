//go:build darwin

package main

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const macOSPrivilegedInstallScript = `set -euo pipefail
src="$1"
case "$src" in
  */linkvideo.monitor_macos_*.pkg) ;;
  *) echo "unexpected update package path" >&2; exit 20 ;;
esac
[ -f "$src" ] || { echo "update package is not a regular file" >&2; exit 21; }

tmpdir="$(/usr/bin/mktemp -d /var/tmp/linkvideo-monitor-update.XXXXXX)"
trap '/bin/rm -rf "$tmpdir"' EXIT
pkg="$tmpdir/update.pkg"
/bin/cp "$src" "$pkg"
/bin/chmod 600 "$pkg"

app="/Applications/LinkVideo.Monitor.app"
/usr/bin/codesign --verify --deep --strict "$app"
app_details="$(/usr/bin/codesign -dv --verbose=4 "$app" 2>&1)"
app_team="$(/usr/bin/printf '%s\n' "$app_details" | /usr/bin/sed -n 's/^TeamIdentifier=//p' | /usr/bin/head -n 1)"
app_authority="$(/usr/bin/printf '%s\n' "$app_details" | /usr/bin/sed -n 's/^Authority=\(Developer ID Application:.*\)$/\1/p' | /usr/bin/head -n 1)"
[ -n "$app_team" ] && [ "$app_team" != "not set" ] || { echo "installed app has no Developer ID Team ID" >&2; exit 22; }
case "$app_authority" in
  *"($app_team)"*) ;;
  *) echo "installed app Developer ID Application does not match Team ID" >&2; exit 23 ;;
esac

pkg_details="$(/usr/sbin/pkgutil --check-signature "$pkg" 2>&1)"
/usr/bin/printf '%s\n' "$pkg_details" | /usr/bin/grep -q 'Developer ID Installer:' || { echo "package has no Developer ID Installer signature" >&2; exit 24; }
pkg_team="$(/usr/bin/printf '%s\n' "$pkg_details" | /usr/bin/sed -n 's/.*Developer ID Installer:.*(\([^)]*\)).*/\1/p' | /usr/bin/head -n 1)"
[ -n "$pkg_team" ] && [ "$pkg_team" = "$app_team" ] || { echo "package Team ID does not match installed app" >&2; exit 25; }
/usr/sbin/spctl --assess --type install --verbose=4 "$pkg"

expanded="$tmpdir/expanded"
/usr/sbin/pkgutil --expand "$pkg" "$expanded"
package_info="$expanded/PackageInfo"
[ -f "$package_info" ] || { echo "package metadata is missing" >&2; exit 26; }
/usr/bin/grep -Fq 'identifier="ru.linkvideo.monitor.pkg"' "$package_info" || { echo "unexpected package identifier" >&2; exit 27; }

base="${src##*/}"
expected_full="${base#linkvideo.monitor_macos_}"
expected_full="${expected_full%.pkg}"
expected_version="${expected_full%%-*}"
expected_version="${expected_version%%+*}"
[ -n "$expected_version" ] || { echo "package version is missing from filename" >&2; exit 28; }
/usr/bin/grep -Fq "version=\"$expected_version\"" "$package_info" || { echo "package version does not match filename" >&2; exit 29; }

/usr/sbin/installer -pkg "$pkg" -target /`

const macOSUpdateInstallerAppleScript = `on run argv
set pkgPath to item 1 of argv
set privilegedScript to item 2 of argv
try
    do shell script "/bin/bash -c " & quoted form of privilegedScript & " -- " & quoted form of pkgPath with administrator privileges
on error errMsg number errNum
    try
        do shell script "/usr/bin/open -gja '/Applications/LinkVideo.Monitor.app' --args --background"
    end try
    error errMsg number errNum
end try
return "installed"
end run`

func launchMacOSUpdateInstaller(pkgPath string) error {
	cmd := exec.Command("/usr/bin/osascript", "-e", macOSUpdateInstallerAppleScript, "--", pkgPath, macOSPrivilegedInstallScript)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("osascript: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if !strings.Contains(strings.ToLower(string(out)), "installed") {
		return errors.New("системный установщик не подтвердил завершение")
	}
	return nil
}
