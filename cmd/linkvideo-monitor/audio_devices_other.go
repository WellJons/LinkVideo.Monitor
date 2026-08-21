//go:build !windows && !darwin

package main

import "fmt"

func listAudioDevices(ffmpegPath string) ([]string, error) {
	return nil, fmt.Errorf("поиск аудиоустройств не поддерживается на этой платформе")
}
