//go:build !windows

package main

import "fmt"

func listAudioDevices(ffmpegPath string) ([]string, error) {
	return nil, fmt.Errorf("поиск аудиоустройств поддерживается только в Windows")
}
