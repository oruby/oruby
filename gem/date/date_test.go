package date_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/date"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/regexp"
	_ "github.com/oruby/oruby/gem/time"
)

func evalDate(t *testing.T, code string) {
	t.Helper()
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	if _, err := mrb.Eval(code); err != nil {
		t.Fatalf("ruby eval failed:\n%s\nerror: %v", code, err)
	}
}

func TestDateConstructorsAndCalendar(t *testing.T) {
	evalDate(t, `
require "date"

d = Date.new(2024, 2, 29)
raise "leap" unless d.leap?
raise "year/mon/mday" unless [d.year, d.mon, d.mday] == [2024, 2, 29]
raise "wday" unless d.thursday? && d.wday == 4
raise "yday" unless d.yday == 60
raise "iso8601" unless d.iso8601 == "2024-02-29"

raise "ordinal" unless Date.ordinal(2024, 60) == d
raise "jd roundtrip" unless Date.jd(d.jd) == d
raise "commercial" unless Date.commercial(2024, 9, 4) == d

raise "+1 day" unless (d + 1) == Date.new(2024, 3, 1)
raise ">> 12 months" unless (d >> 12) == Date.new(2025, 2, 28)
raise "<< 1 month" unless (Date.new(2024, 3, 31) << 1) == Date.new(2024, 2, 29)
raise "diff" unless (Date.new(2024, 3, 10) - d) == 10

begin
  Date.new(2023, 2, 29)
  raise "should have raised Date::Error"
rescue Date::Error
end
`)
}

func TestDateParsingAndStrftime(t *testing.T) {
	evalDate(t, `
require "date"

d = Date.parse("2026-11-23")
raise "parse iso" unless d == Date.new(2026, 11, 23)
raise "iso8601 parse" unless Date.iso8601("2026-11-23") == d
raise "strptime" unless Date.strptime("23/11/2026", "%d/%m/%Y") == d
raise "strftime" unless d.strftime("%Y-%m-%d (%a)") == "2026-11-23 (Mon)"
raise "rfc2822" unless Date.rfc2822("Mon, 23 Nov 2026 00:00:00 +0000") == d
raise "httpdate" unless Date.httpdate("Mon, 23 Nov 2026 12:00:00 GMT") == d
`)
}

func TestDateTimeOperations(t *testing.T) {
	evalDate(t, `
require "date"

dt = DateTime.iso8601("2026-11-23T15:30:45+02:00")
raise "ymd" unless [dt.year, dt.month, dt.day] == [2026, 11, 23]
raise "hms" unless [dt.hour, dt.min, dt.sec] == [15, 30, 45]
raise "zone" unless dt.zone == "+02:00"

utc = dt.new_offset(0)
raise "utc hour" unless utc.hour == 13 && utc.min == 30 && utc.sec == 45
raise "to_date" unless dt.to_date == Date.new(2026, 11, 23)
`)
}
