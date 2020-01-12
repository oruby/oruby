package regexp

import (
	"reflect"
	"regexp"
	"testing"
)

// /(?<x>.)(?<y>.)?/.match("a").named_captures #=> #<MatchData "a" x:"a" y:nil>

func TestMatchData(t *testing.T) {
	r := regexp.MustCompile("(?P<x>.)(?P<y>.)?")
	if !reflect.DeepEqual(r.SubexpNames(), []string{"", "x", "y"}) {
		t.Error()
	}
	results := r.FindStringSubmatchIndex("a")
	data := NewMatchData(r, "a", results, 0)
	if !reflect.DeepEqual(data.Names(), []string{"x", "y"}) {
		t.Error()
	}
	if !reflect.DeepEqual(data.Captures(), []interface{}{"a", nil}) {
		t.Error(data.Captures()...)
	}

}