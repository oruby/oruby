package orubyyajl

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "oruby-yajl", Version: "1.4.3", Feature: "oruby-yajl",
		Summary: "ORuby pure-Go Yajl (Go encoding/json/v2)",
		Files:   []string{"lib/oruby-yajl.rb", "lib/yajl.rb"},
	})
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "yajl-ruby", Version: "1.4.3", Feature: "oruby-yajl",
		Summary: "ORuby yajl-ruby compatibility provider",
		Files:   []string{"lib/yajl.rb"},
	})
}
