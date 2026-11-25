package orubycbor

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "oruby-cbor", Version: "0.5.9.8", Feature: "oruby-cbor",
		Summary: "ORuby pure-Go CBOR",
		Files:   []string{"lib/oruby-cbor.rb", "lib/cbor.rb"},
	})
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "cbor", Version: "0.5.9.8", Feature: "oruby-cbor",
		Summary: "ORuby CBOR compatibility provider",
		Files:   []string{"lib/cbor.rb"},
	})
}
