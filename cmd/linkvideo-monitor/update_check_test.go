package main

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.7.7", "0.7.6-beta", 1},
		{"v0.7.6-beta", "0.7.6", -1},
		{"0.7.6", "0.7.6-beta", 1},
		{"0.7.5", "0.7.6", -1},
		{"0.8.12.1", "0.8.12", 1},
		{"0.8.13-beta", "0.8.12-beta", 1},
		{"0.8.13-beta.2", "0.8.13-beta.1", 1},
		{"1.0.0+build.2", "1.0.0+build.1", 0},
	}
	for _, tc := range cases {
		if got := compareVersions(tc.a, tc.b); got != tc.want {
			t.Fatalf("compareVersions(%q,%q)=%d want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestParseVersionRejectsMalformedVersions(t *testing.T) {
	for _, value := range []string{"", "beta", "0..8", "0.8.x", "0.8.12-"} {
		if _, err := parseVersion(value); err == nil {
			t.Fatalf("parseVersion(%q) unexpectedly succeeded", value)
		}
	}
}
