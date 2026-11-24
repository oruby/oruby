package main

import (
	"os"
	"path/filepath"
)

// embeddedOrb is the .orb bundle an executable built by `oruby build
// --target=exe` carries (set by its generated zz_oruby_build.go), run as
// `oruby <name>.orb ARGS` runs one: every argument is the program's.
var embeddedOrb []byte

// embeddedOrbName is the bundle's file name, which the program runs as
var embeddedOrbName string

// embeddedOrbPath is the path the embedded bundle runs as: beside the
// executable, under the bundle's name (no file is read there)
func embeddedOrbPath() string {
	exe, err := os.Executable()
	if err != nil {
		return embeddedOrbName
	}
	return filepath.Join(filepath.Dir(exe), embeddedOrbName)
}
