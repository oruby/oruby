package orubydigestcrc

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "oruby-digest-crc", Version: "0.7.0", Feature: "oruby-digest-crc",
		Summary: "ORuby pure-Go digest-crc",
		Files:   []string{"lib/digest/crc.rb", "lib/digest/crc32.rb", "lib/digest/crc32c.rb"},
	})
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "digest-crc", Version: "0.7.0", Feature: "oruby-digest-crc",
		Summary: "ORuby digest-crc compatibility provider",
		Files:   []string{"lib/digest/crc.rb", "lib/digest/crc32.rb", "lib/digest/crc32c.rb"},
	})
}
