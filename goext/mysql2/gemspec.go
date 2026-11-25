package mysql2

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "mysql2", Version: "0.5.6", Feature: "mysql2",
		Summary: "ORuby pure-Go MySQL driver",
		Files:   []string{"lib/mysql2.rb"},
	})
}
