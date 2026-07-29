package main

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.7.7", "0.7.6-beta", 1}, {"v0.7.6-beta", "0.7.6", 0}, {"0.7.5", "0.7.6", -1},
	}
	for _, tc := range cases {
		if got := compareVersions(tc.a, tc.b); got != tc.want {
			t.Fatalf("compareVersions(%q,%q)=%d want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
