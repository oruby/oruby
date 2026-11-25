//go:build oruby_slim

package stdlib

import (
	"errors"

	"github.com/oruby/oruby"
)

// open-uri needs Go's net/http and TLS, which a slim binary leaves out
// (gems_web.go): the oruby compiler's -gems option links it in.
func initOpenURI(mrb *oruby.MrbState) interface{} {
	return errors.New("open-uri is not in this binary: it was built without the web gems (-gems web or auto)")
}
