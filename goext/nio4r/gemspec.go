package nio4r

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "nio4r", Version: "2.7.5", Feature: "nio4r",
		Summary: "ORuby pure-Go NIO4r selector",
		Files:   []string{"lib/nio.rb", "lib/nio4r.rb"},
	})
}
