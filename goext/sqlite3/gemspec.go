package sqlite3

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "sqlite3", Version: "2.6.0", Feature: "sqlite3",
		Summary: "ORuby SQLite3 driver",
		Files:   []string{"lib/sqlite3.rb"},
	})
}
