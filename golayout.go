package oruby

import (
	"os"
	"path/filepath"
	"runtime"
)

// RubyVersion is the MRI version ORuby targets (RUBY_VERSION).
const RubyVersion = "4.1.0"

// RubyArch is the architecture name of RbConfig::CONFIG["arch"], MRI-style
// (x86_64-linux, arm64-darwin).
func RubyArch() string {
	cpu := runtime.GOARCH
	switch cpu {
	case "amd64":
		cpu = "x86_64"
	case "386":
		cpu = "i386"
	}
	return cpu + "-" + runtime.GOOS
}

// InstallPrefix is the directory ORuby is installed in, found from the
// executable as MRI's layout puts it: <prefix>/bin/oruby, with the Ruby
// libraries under <prefix>/lib/oruby/<version>.
func InstallPrefix() string {
	exe, err := os.Executable()
	if err != nil {
		return "/usr/local"
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return filepath.Dir(filepath.Dir(exe))
}

// RubyLibPrefix is RbConfig::CONFIG["rubylibprefix"]: <prefix>/lib/oruby.
func RubyLibPrefix() string {
	return filepath.Join(InstallPrefix(), "lib", "oruby")
}

// DefaultLoadPath is the $LOAD_PATH MRI starts with: the site and vendor
// directories, then the Ruby library directory, each with its arch dir.
func DefaultLoadPath() []string {
	lib := RubyLibPrefix()
	arch := RubyArch()
	site := filepath.Join(lib, "site_ruby")
	vendor := filepath.Join(lib, "vendor_ruby")
	return []string{
		filepath.Join(site, RubyVersion),
		filepath.Join(site, RubyVersion, arch),
		site,
		filepath.Join(vendor, RubyVersion),
		filepath.Join(vendor, RubyVersion, arch),
		vendor,
		filepath.Join(lib, RubyVersion),
		filepath.Join(lib, RubyVersion, arch),
	}
}
