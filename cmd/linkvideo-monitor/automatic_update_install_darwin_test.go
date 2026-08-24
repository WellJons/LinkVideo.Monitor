//go:build darwin

package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMacOSPrivilegedInstallScriptSyntax(t *testing.T) {
	cmd := exec.Command("/bin/bash", "-n")
	cmd.Stdin = strings.NewReader(macOSPrivilegedInstallScript)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("privileged install script has invalid shell syntax: %v: %s", err, strings.TrimSpace(string(out)))
	}
}

func TestMacOSUpdateInstallerAppleScriptSyntax(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "installer.scpt")
	out, err := exec.Command("/usr/bin/osacompile", "-o", outPath, "-e", macOSUpdateInstallerAppleScript).CombinedOutput()
	if err != nil {
		t.Fatalf("updater AppleScript does not compile: %v: %s", err, strings.TrimSpace(string(out)))
	}
}

func TestMacOSPrivilegedInstallRevalidatesRootOwnedCopyBeforeInstall(t *testing.T) {
	checks := []string{
		`/bin/cp "$src" "$pkg"`,
		`/usr/sbin/pkgutil --check-signature "$pkg"`,
		`[ -n "$pkg_team" ] && [ "$pkg_team" = "$app_team" ]`,
		`/usr/sbin/spctl --assess --type install --verbose=4 "$pkg"`,
		`identifier="ru.linkvideo.monitor.pkg"`,
		`version=\"$expected_version\"`,
		`/usr/sbin/installer -pkg "$pkg" -target /`,
	}
	last := -1
	for _, needle := range checks {
		idx := strings.Index(macOSPrivilegedInstallScript, needle)
		if idx < 0 {
			t.Fatalf("privileged install script is missing required check %q", needle)
		}
		if idx <= last {
			t.Fatalf("privileged install check %q is out of order", needle)
		}
		last = idx
	}
	if strings.Contains(macOSPrivilegedInstallScript, `/usr/sbin/installer -pkg "$src"`) {
		t.Fatal("privileged installer must never install directly from the user-writable source path")
	}
}

func TestMacOSPrivilegedPackageMetadataQuotesAreLiteral(t *testing.T) {
	if !strings.Contains(macOSPrivilegedInstallScript, `identifier="ru.linkvideo.monitor.pkg"`) {
		t.Fatal("package identifier validation must match quoted PackageInfo metadata")
	}
}
