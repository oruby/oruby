package compiler

import (
	"regexp"
	"sort"
	"strings"
)

// Which gems a compiled program links. The Go gems that make up a large part
// of a binary (HTTP, Rack, Net::HTTP with TLS, sockets, open-uri: about 7 MB of
// the 29 MB of a hello world) are behind the oruby_slim build tag
// (gem/stdlib/gems_web.go). `-gems all` (the default) links everything;
// `-gems core` builds slim; `-gems auto` builds slim unless the program
// requires something that needs them, or requires by a name only run time
// knows (a dynamic require), or came without source.
const (
	GemsAll  = "all"
	GemsCore = "core"
	GemsAuto = "auto"
)

// webFeatures are the require names that need the web gems; webPrefixes the
// ones that start a family of them. The standard library's own files that
// need sockets are in the list (ipaddr, resolv, drb).
var webFeatures = map[string]bool{
	"socket": true, "openssl": true, "open-uri": true, "http": true, "resolv": true,
	"ipaddr": true, "drb": true, "webrick": true, "rack": true, "rackup": true,
	"oruby/rack": true,
}

var webPrefixes = []string{"net/", "rack/", "rackup/", "drb/", "oruby/rack/"}

// dynamicRequireRe: a require whose argument does not start with a quote
var dynamicRequireRe = regexp.MustCompile(`(?m)^\s*(?:Kernel\.)?require\s*\(?\s*[^'"\s)]`)

// NeedsWebGems reports whether a program that requires features needs the web
// gems, and why. dynamic says it requires by a name known only at run time.
func NeedsWebGems(features []string, dynamic bool) (bool, string) {
	if dynamic {
		return true, "it requires by a name only run time knows (or came without source)"
	}
	sorted := append([]string(nil), features...)
	sort.Strings(sorted)
	for _, f := range sorted {
		if webFeatures[f] {
			return true, "it requires " + f
		}
		for _, p := range webPrefixes {
			if strings.HasPrefix(f, p) {
				return true, "it requires " + f
			}
		}
	}
	return false, ""
}

// SlimBuild decides whether to build with the oruby_slim tag under the -gems
// mode, with a one line reason for the log.
func SlimBuild(mode string, features []string, dynamic bool) (slim bool, why string) {
	switch mode {
	case GemsCore:
		return true, "-gems core: without the web gems"
	case GemsAuto:
		if need, reason := NeedsWebGems(features, dynamic); need {
			return false, "-gems auto: with the web gems, " + reason
		}
		return true, "-gems auto: without the web gems, the program requires none"
	}
	return false, ""
}
