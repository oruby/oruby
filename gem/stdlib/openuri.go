//go:build !oruby_slim

package stdlib

import (
	"fmt"
	"io"
	nethttp "net/http"
	"net/url"
	"time"

	"github.com/oruby/oruby"
)

const openURIMaxBytes = 64 << 20

func initOpenURI(mrb *oruby.MrbState) interface{} {
	mod := mrb.DefineModule("OpenURI")
	mod.DefineClassMethod("__open", openURI, mrb.ArgsArg(1, 1))
	mod.DefineClassUnder("HTTPError", mrb.ClassGet("StandardError"))
	load(mrb, "open-uri")
	return nil
}

func openURI(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	if args.Len() > 1 && !args.Item(1).IsNil() {
		return mrb.Raise(mrb.EArgumentError(), "open-uri options are not supported")
	}
	if !mrb.KeywordArgs().EmptyP() {
		return mrb.Raise(mrb.EArgumentError(), "open-uri keyword options are not supported")
	}
	input := args.Item(0)
	if !input.IsString() {
		input = mrb.Call(input, "to_s")
		if exc := mrb.Exc(); exc != nil {
			return exc.Value()
		}
	}
	text := mrb.String(input)
	parsed, err := url.Parse(text)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return mrb.Raise(mrb.EArgumentError(), "open-uri supports only absolute HTTP and HTTPS URLs")
	}

	request, err := nethttp.NewRequest(nethttp.MethodGet, parsed.String(), nil)
	if err != nil {
		return mrb.RaiseError(err)
	}
	client := &nethttp.Client{Timeout: 30 * time.Second, CheckRedirect: checkOpenURIRedirect}
	response, err := client.Do(request)
	if err != nil {
		return mrb.RaiseError(err)
	}
	defer response.Body.Close()
	if response.StatusCode < nethttp.StatusOK || response.StatusCode >= nethttp.StatusMultipleChoices {
		httpError := mrb.ClassGetUnder(mrb.ModuleGet("OpenURI"), "HTTPError")
		return mrb.Raise(httpError, fmt.Sprintf("HTTP %s for %s", response.Status, parsed.Redacted()))
	}
	if response.ContentLength > openURIMaxBytes {
		return mrb.Raise(mrb.EArgumentError(), "open-uri response exceeds size limit")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, openURIMaxBytes+1))
	if err != nil {
		return mrb.RaiseError(err)
	}
	if len(body) > openURIMaxBytes {
		return mrb.Raise(mrb.EArgumentError(), "open-uri response exceeds size limit")
	}
	stream, err := mrb.ObjNew(mrb.ClassGet("StringIO"), mrb.BytesValue(body))
	if err != nil {
		return mrb.RaiseError(err)
	}
	return stream
}

func checkOpenURIRedirect(request *nethttp.Request, via []*nethttp.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("too many redirects")
	}
	if request.URL.Scheme != "http" && request.URL.Scheme != "https" {
		return fmt.Errorf("open-uri redirect scheme is not supported")
	}
	if len(via) != 0 && via[len(via)-1].URL.Scheme == "https" && request.URL.Scheme != "https" {
		return fmt.Errorf("open-uri HTTPS redirect downgrade is not allowed")
	}
	return nil
}
