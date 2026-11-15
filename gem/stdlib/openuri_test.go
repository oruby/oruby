package stdlib

import (
	"net/http"
	"testing"
)

func TestOpenURIRedirectPolicy(t *testing.T) {
	request := func(raw string) *http.Request {
		req, err := http.NewRequest(http.MethodGet, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		return req
	}
	https := request("https://example.com/")
	httpURL := request("http://example.com/")
	ftp := request("ftp://example.com/")
	if err := checkOpenURIRedirect(https, []*http.Request{httpURL}); err != nil {
		t.Fatalf("HTTP to HTTPS redirect: %v", err)
	}
	if err := checkOpenURIRedirect(httpURL, []*http.Request{https}); err == nil {
		t.Fatal("HTTPS downgrade should fail")
	}
	if err := checkOpenURIRedirect(ftp, []*http.Request{httpURL}); err == nil {
		t.Fatal("non-HTTP redirect should fail")
	}
	if err := checkOpenURIRedirect(https, make([]*http.Request, 10)); err == nil {
		t.Fatal("redirect limit should fail")
	}
}
