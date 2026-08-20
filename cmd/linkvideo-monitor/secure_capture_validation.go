package main

const (
	secureCaptureMaxInputDimension  = 32768
	secureCaptureMaxOutputDimension = 8192
	secureCaptureMaxFPS             = 30
)

func validSecureCaptureDimensions(width, height, outputWidth, outputHeight, fps int) bool {
	if width < 2 || height < 2 || outputWidth < 2 || outputHeight < 2 {
		return false
	}
	if width > secureCaptureMaxInputDimension || height > secureCaptureMaxInputDimension ||
		outputWidth > secureCaptureMaxOutputDimension || outputHeight > secureCaptureMaxOutputDimension {
		return false
	}
	return fps >= 1 && fps <= secureCaptureMaxFPS
}
