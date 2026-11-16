package regexp

import (
	"reflect"
	"testing"
)

// /(?<x>.)(?<y>.)?/.match("a").named_captures #=> #<MatchData "a" x:"a" y:nil>

func TestMatchData(t *testing.T) {
	r := MustCompileRuby("(?<x>.)(?<y>.)?", 0)
	if !reflect.DeepEqual(r.subexpNames(), []string{"", "x", "y"}) {
		t.Error(r.subexpNames())
	}
	results := r.find("a", 0)
	data := NewMatchData(r, "a", results, 0)
	if !reflect.DeepEqual(data.Names(), []string{"x", "y"}) {
		t.Error(data.Names())
	}
	if !reflect.DeepEqual(data.Captures(), []interface{}{"a", nil}) {
		t.Error(data.Captures()...)
	}
}
