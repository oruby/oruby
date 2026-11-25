package commonmarker

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "commonmarker", Version: "2.0.0", Feature: "commonmarker",
		Summary: "ORuby pure-Go CommonMarker (Goldmark)",
		Files:   []string{"lib/commonmarker.rb"},
	})
}
