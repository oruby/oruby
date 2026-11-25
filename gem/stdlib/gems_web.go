//go:build !oruby_slim

package stdlib

// The gems that are a large part of a binary (HTTP, Rack, Net::HTTP with its
// TLS, sockets: about 13 MB of the 29 MB of a hello world). A binary built
// with the oruby_slim tag leaves them out; the oruby compiler's -gems option
// then imports the ones a program needs (compiler/gems.go).
import (
	_ "github.com/oruby/oruby/gem/http"
	_ "github.com/oruby/oruby/gem/nethttp"
	_ "github.com/oruby/oruby/gem/rack"
	_ "github.com/oruby/oruby/gem/socket"
)
