package vips

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "oruby-vips", Version: "2.3.0", Feature: "oruby-vips",
		Summary: "ORuby united libvips/pure-Go image processor",
		Files:   []string{"lib/oruby-vips.rb", "lib/vips.rb", "lib/ruby-vips.rb"},
	})
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "ruby-vips", Version: "2.3.0", Feature: "oruby-vips",
		Summary: "ORuby ruby-vips compatibility provider",
		Files:   []string{"lib/ruby-vips.rb", "lib/vips.rb"},
	})
}
