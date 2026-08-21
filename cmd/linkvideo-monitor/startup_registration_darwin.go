//go:build darwin

package main

/*
#cgo LDFLAGS: -framework Foundation -framework ServiceManagement
#include <stdlib.h>

char *lv_sync_startup(int enabled);
char *lv_startup_status_name(void);
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func syncStartupRegistration(enabled bool) error {
	value := C.int(0)
	if enabled {
		value = 1
	}
	message := C.lv_sync_startup(value)
	if message == nil {
		return nil
	}
	defer C.free(unsafe.Pointer(message))
	return fmt.Errorf("ServiceManagement: %s", C.GoString(message))
}

func startupRegistrationStatus() (string, error) {
	value := C.lv_startup_status_name()
	if value == nil {
		return "", fmt.Errorf("ServiceManagement не вернул статус login item")
	}
	defer C.free(unsafe.Pointer(value))
	return C.GoString(value), nil
}
