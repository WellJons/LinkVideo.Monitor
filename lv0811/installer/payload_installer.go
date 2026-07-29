//go:build !uninstaller

package main

import _ "embed"

//go:embed payload.zip
var payload []byte

const uninstallerBuild = false
