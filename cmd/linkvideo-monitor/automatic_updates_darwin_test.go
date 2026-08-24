//go:build darwin

package main

import (
	"strings"
	"testing"
)

func TestParseMacOSCodeSignIdentity(t *testing.T) {
	input := `Executable=/Applications/LinkVideo.Monitor.app/Contents/MacOS/LinkVideo.Monitor
Authority=Developer ID Application: LinkVideo LLC (ABC123TEAM)
Authority=Developer ID Certification Authority
TeamIdentifier=ABC123TEAM
Runtime Version=26.0.0`
	team, authority, err := parseMacOSCodeSignIdentity(input)
	if err != nil {
		t.Fatal(err)
	}
	if team != "ABC123TEAM" || authority != "Developer ID Application: LinkVideo LLC (ABC123TEAM)" {
		t.Fatalf("identity=%q %q", team, authority)
	}
}

func TestParseMacOSCodeSignIdentityRejectsAdHoc(t *testing.T) {
	if _, _, err := parseMacOSCodeSignIdentity("TeamIdentifier=not set\nSignature=adhoc\n"); err == nil {
		t.Fatal("ad-hoc signature was accepted")
	}
}

func TestParseMacOSPackageSignature(t *testing.T) {
	input := `Package "LinkVideo.Monitor_macOS_0.1.2.pkg":
   Status: signed by a developer certificate issued by Apple for distribution
   Certificate Chain:
    1. Developer ID Installer: LinkVideo LLC (ABC123TEAM)
       Expires: 2030-01-01 00:00:00 +0000`
	team, authority, err := parseMacOSPackageSignature(input)
	if err != nil {
		t.Fatal(err)
	}
	if team != "ABC123TEAM" || !strings.HasPrefix(authority, "Developer ID Installer:") {
		t.Fatalf("package identity=%q %q", team, authority)
	}
}

func TestMacOSAutomaticUpdateURLRequiresPKG(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	good := "https://github.com/WellJons/LinkVideo.Monitor.Updates/releases/download/v0.1.2/LinkVideo.Monitor_macOS_0.1.2.pkg"
	if err := validateAutomaticUpdateDownload(good, sha, "0.1.2"); err != nil {
		t.Fatalf("valid macOS PKG rejected: %v", err)
	}
	bad := "https://github.com/WellJons/LinkVideo.Monitor.Updates/releases/download/v0.1.2/LinkVideo.Monitor_0.1.2_Setup.exe"
	if validateAutomaticUpdateDownload(bad, sha, "0.1.2") == nil {
		t.Fatal("Windows installer was accepted by Darwin updater")
	}
}

func TestMacOSUpdateInstallerAppleScriptDoesNotInterpolatePackagePath(t *testing.T) {
	if !strings.Contains(macOSUpdateInstallerAppleScript, "quoted form of pkgPath") {
		t.Fatal("installer AppleScript must shell-quote package path")
	}
	if strings.Contains(macOSUpdateInstallerAppleScript, "${") || strings.Contains(macOSUpdateInstallerAppleScript, "%s") {
		t.Fatal("installer AppleScript must not interpolate package path into source")
	}
}

func TestMacOSAutomaticUpdateRetryBackoff(t *testing.T) {
	if macOSAutomaticUpdateRetryDelay(1) != macOSAutoUpdateRetryBase {
		t.Fatal("first retry delay mismatch")
	}
	if macOSAutomaticUpdateRetryDelay(20) != macOSAutoUpdateRetryMax {
		t.Fatal("retry delay must be capped")
	}
}
