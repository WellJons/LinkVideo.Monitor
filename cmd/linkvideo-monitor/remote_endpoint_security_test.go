package main

import "testing"

func TestValidateRemoteAPIEndpoint(t *testing.T) {
	accepted := []string{
		"https://api.example.test/monitor/sync",
		"http://127.0.0.1:8080/sync",
		"http://[::1]:8080/sync",
		"http://localhost:8080/sync",
	}
	for _, raw := range accepted {
		if err := validateRemoteAPIEndpoint(raw); err != nil {
			t.Fatalf("%s rejected: %v", raw, err)
		}
	}

	rejected := []string{
		"http://api.example.test/sync",
		"ftp://api.example.test/sync",
		"https://user:pass@api.example.test/sync",
		"not-a-url",
	}
	for _, raw := range rejected {
		if err := validateRemoteAPIEndpoint(raw); err == nil {
			t.Fatalf("unsafe endpoint accepted: %s", raw)
		}
	}
}
