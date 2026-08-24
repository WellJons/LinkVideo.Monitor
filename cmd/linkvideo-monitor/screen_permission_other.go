//go:build !darwin

package main

type screenPermissionStatus struct {
	Supported bool `json:"supported"`
	Granted   bool `json:"granted"`
}

func screenCapturePermissionStatus() screenPermissionStatus {
	return screenPermissionStatus{}
}

func requestScreenCapturePermission() (screenPermissionStatus, error) {
	return screenPermissionStatus{}, nil
}
