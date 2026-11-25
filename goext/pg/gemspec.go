package pg

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "pg", Version: "1.5.9", Feature: "pg",
		Summary: "ORuby pure-Go PostgreSQL driver (pgx)",
		Files:   []string{"lib/pg.rb"},
	})
}
