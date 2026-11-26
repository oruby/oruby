package oruby

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// RubyVersion is the MRI version ORuby targets (RUBY_VERSION).
const RubyVersion = "4.1.0"

// RubyArch is the architecture name of RbConfig::CONFIG["arch"] and
// RUBY_PLATFORM, MRI-style (x86_64-linux, aarch64-linux, arm64-darwin).
func RubyArch() string {
	cpu := runtime.GOARCH
	switch cpu {
	case "amd64":
		cpu = "x86_64"
	case "386":
		cpu = "i386"
	case "arm64":
		if runtime.GOOS != "darwin" {
			cpu = "aarch64" // uname -m; Apple's is arm64
		}
	}
	return cpu + "-" + runtime.GOOS
}

// InstallPrefix is the directory ORuby is installed in, found from the
// executable as MRI's layout puts it: <prefix>/bin/oruby, with the Ruby
// libraries under <prefix>/lib/oruby/<version>.
func InstallPrefix() string {
	if installPrefix != "" {
		return installPrefix
	}
	defaultPrefixOnce.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			defaultInstallPrefix = "/usr/local"
			return
		}
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		defaultInstallPrefix = filepath.Dir(filepath.Dir(exe))
	})
	return defaultInstallPrefix
}

var (
	installPrefix        string
	defaultPrefixOnce    sync.Once
	defaultInstallPrefix string

	stdlibOrbMu     sync.Mutex
	stdlibOrbDir    string
	stdlibOrbLoaded bool
	stdlibOrbFile   *OrbFile
)

// StdlibOrb returns the parsed rubygems.orb archive for the current
// RubyLibPrefix()/RubyVersion directory, or nil if absent/invalid.
func StdlibOrb() *OrbFile {
	libDir := filepath.Join(RubyLibPrefix(), RubyVersion)
	stdlibOrbMu.Lock()
	defer stdlibOrbMu.Unlock()
	if stdlibOrbLoaded && stdlibOrbDir == libDir {
		return stdlibOrbFile
	}
	stdlibOrbDir = libDir
	stdlibOrbLoaded = true
	stdlibOrbFile = nil
	orbPath := filepath.Join(libDir, "rubygems.orb")
	data, err := os.ReadFile(orbPath)
	if err != nil {
		return nil
	}
	orb, err := ParseOrb(orbPath, data)
	if err != nil {
		return nil
	}
	stdlibOrbFile = orb
	return orb
}

var (
	stdlibExtraMu     sync.Mutex
	stdlibExtraDir    string
	stdlibExtraLoaded bool
	stdlibExtraFile   *OrbFile
)

// StdlibExtraOrb returns the parsed stdlib.orb archive for the current
// RubyLibPrefix()/RubyVersion directory, or nil if absent/invalid.
func StdlibExtraOrb() *OrbFile {
	libDir := filepath.Join(RubyLibPrefix(), RubyVersion)
	stdlibExtraMu.Lock()
	defer stdlibExtraMu.Unlock()
	if stdlibExtraLoaded && stdlibExtraDir == libDir {
		return stdlibExtraFile
	}
	stdlibExtraDir = libDir
	stdlibExtraLoaded = true
	stdlibExtraFile = nil
	orbPath := filepath.Join(libDir, "stdlib.orb")
	data, err := os.ReadFile(orbPath)
	if err != nil {
		return nil
	}
	orb, err := ParseOrb(orbPath, data)
	if err != nil {
		return nil
	}
	stdlibExtraFile = orb
	return orb
}

// SetInstallPrefix names the directory ORuby is installed in, for an
// executable that is not under it: a project's own bin/oruby, built with
// its C extensions (`oruby build --target=dev`), uses the libraries of the
// ORuby that built it.
func SetInstallPrefix(prefix string) { installPrefix = prefix }

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
