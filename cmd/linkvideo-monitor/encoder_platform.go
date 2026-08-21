package main

import "runtime"

// platformHardwareEncoderCandidates returns native hardware encoders that are
// meaningful on the current operating system. Windows GPU encoders keep their
// existing adapter-based discovery path; macOS exposes VideoToolbox directly
// and lets the normal FFmpeg capability probe determine actual availability.
func platformHardwareEncoderCandidates(codec string) []encoderOption {
	if runtime.GOOS != "darwin" {
		return nil
	}
	name := "h264_videotoolbox"
	if codec == "h265" {
		name = "hevc_videotoolbox"
	}
	return []encoderOption{{Name: name, Label: encoderLabel(name)}}
}

func defaultEncoderForPlatform(codec string) string {
	if candidates := platformHardwareEncoderCandidates(codec); len(candidates) > 0 {
		return candidates[0].Name
	}
	return softwareEncoderForCodec(codec)
}
