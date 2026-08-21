//go:build darwin

package main

/*
#cgo LDFLAGS: -framework Foundation -framework CoreServices
#include <stdlib.h>

char *lv_register_url_handler(void);
char *lv_url_handler_status(void);
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func syncURLProtocolRegistration() error {
	message := C.lv_register_url_handler()
	if message == nil {
		return nil
	}
	defer C.free(unsafe.Pointer(message))
	return fmt.Errorf("Launch Services: %s", C.GoString(message))
}

func urlProtocolRegistrationStatus() (string, error) {
	value := C.lv_url_handler_status()
	if value == nil {
		return "", fmt.Errorf("Launch Services не вернул обработчик linkvideomonitor")
	}
	defer C.free(unsafe.Pointer(value))
	return C.GoString(value), nil
}
