package main

import "testing"

func TestDefaultUpdateManifestForPlatform(t *testing.T) {
	cases := map[string]string{
		"windows": "update-manifest.json",
		"darwin":  "update-manifest-macos.json",
		"linux":   "update-manifest-linux.json",
	}
	for goos, suffix := range cases {
		got := defaultUpdateManifestForPlatform(goos)
		if got == "" || len(got) < len(suffix) || got[len(got)-len(suffix):] != suffix {
			t.Fatalf("defaultUpdateManifestForPlatform(%q)=%q, want suffix %q", goos, got, suffix)
		}
	}
	if got := defaultUpdateManifestForPlatform("plan9"); got != "" {
		t.Fatalf("unexpected manifest for unsupported platform: %q", got)
	}
}

func TestArchitectureAllowed(t *testing.T) {
	if !architectureAllowed(nil, "arm64") {
		t.Fatal("empty architecture list must allow current architecture")
	}
	if !architectureAllowed([]string{"arm64", "amd64"}, "arm64") {
		t.Fatal("arm64 should be allowed")
	}
	if architectureAllowed([]string{"amd64"}, "arm64") {
		t.Fatal("arm64 should not be allowed by amd64-only manifest")
	}
}

func TestPlatformBuildVersionOverride(t *testing.T) {
	previous := platformBuildVersion
	platformBuildVersion = "9.8.7-test"
	defer func() { platformBuildVersion = previous }()
	if got := currentReleaseVersion(); got != "9.8.7-test" {
		t.Fatalf("currentReleaseVersion()=%q", got)
	}
}
