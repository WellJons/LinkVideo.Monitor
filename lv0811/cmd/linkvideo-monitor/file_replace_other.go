//go:build !windows

package main

import "os"

func replaceFileAtomically(tempPath, targetPath string) error {
	return os.Rename(tempPath, targetPath)
}
