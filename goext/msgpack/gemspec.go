package msgpack

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "msgpack", Version: "1.8.7", Feature: "msgpack",
		Summary: "ORuby pure-Go MessagePack",
		Files:   []string{"lib/msgpack.rb"},
	})
}
