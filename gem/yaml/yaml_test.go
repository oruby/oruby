package yaml_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/yaml"
)

func TestYAMLLazyLoadAndRoundTrip(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	got, err := mrb.Eval(`defined?(YAML).nil? && defined?(Psych).nil?`)
	if err != nil || !got.Bool() {
		t.Fatalf("YAML before require = %v, %v; want true, nil", got, err)
	}
	got, err = mrb.Eval(`require "yaml"`)
	if err != nil || !got.Bool() {
		t.Fatalf("first require(yaml) = %v, %v; want true, nil", got, err)
	}
	got, err = mrb.Eval(`require "yaml"`)
	if err != nil || got.Bool() {
		t.Fatalf("repeat require(yaml) = %v, %v; want false, nil", got, err)
	}
	got, err = mrb.Eval(`YAML.load("name: ORuby")["name"] == "ORuby"`)
	if err != nil || !got.Bool() {
		t.Fatalf("YAML.load string = %v, %v; want true, nil", got, err)
	}
	got, err = mrb.Eval(`YAML.load(YAML.dump({"name" => "ORuby"}))["name"] == "ORuby"`)
	if err != nil || !got.Bool() {
		t.Fatalf("YAML dump/load = %v, %v; want true, nil", got, err)
	}

	got, err = mrb.Eval(`
    same_module = YAML.equal?(Psych)
    psych_loaded = require("psych")
    input = "name: ORuby\nenabled: true\ncount: 7\nitems:\n  - one\n  - 2\nnested:\n  value: 1.5\nnothing: null\n"
    data = YAML.load(input)
    same_module && psych_loaded && data["name"] == "ORuby" && data["enabled"] == true &&
      data["count"] == 7 && data["items"] == ["one", 2] &&
      data["nested"]["value"] == 1.5 && data["nothing"].nil? &&
      YAML.load(YAML.dump(data)) == data
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("YAML round trip = %v, %v; want true, nil", got, err)
	}
}

func TestYAMLRejectsUnsupportedAndUnsafeValues(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	if _, err := mrb.Eval(`require "yaml"`); err != nil {
		t.Fatal(err)
	}

	got, err := mrb.Eval(`
    wrong_type = begin
      YAML.load(12)
      false
    rescue TypeError
      true
    end
    syntax_error = begin
      YAML.load("a: [")
      false
    rescue Psych::SyntaxError
      true
    end
    custom_tag = begin
      YAML.load("object: !ruby/object:Object {}")
      false
    rescue ArgumentError
      true
    end
    alias_rejected = begin
      YAML.load("base: &anchor [1]\ncopy: *anchor\n")
      false
    rescue ArgumentError
      true
    end
    unsupported_dump = begin
      YAML.dump(Object.new)
      false
    rescue ArgumentError
      true
    end
    cyclic_dump = begin
      values = []
      values << values
      YAML.dump(values)
      false
    rescue ArgumentError
      true
    end
    [wrong_type, syntax_error, custom_tag, alias_rejected, unsupported_dump, cyclic_dump].map { |check| check ? "1" : "0" }.join
  `)
	checks := mrb.String(got)
	if err != nil || checks != "111111" {
		t.Fatalf("YAML rejected-input checks = %s, %v; want 111111, nil", checks, err)
	}
}

func TestYAMLAliasesAndLoadFile(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	if _, err := mrb.Eval(`require "yaml"`); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "values.yml")
	if err := os.WriteFile(path, []byte("base: &anchor [one, two]\ncopy: *anchor\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := mrb.Eval(`
    source = "base: &anchor [one, two]\ncopy: *anchor\n"
    disabled = begin
      YAML.load(source)
      false
    rescue ArgumentError
      true
    end
    decoded = YAML.safe_load(source, aliases: true)
    disabled && decoded["base"] == ["one", "two"] && decoded["copy"] == ["one", "two"]
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("YAML explicit alias option = %v, %v; want true, nil", got, err)
	}
	got, err = mrb.Eval(`
    value = YAML.load_file(` + strconv.Quote(path) + `, aliases: true)
    value["base"] == value["copy"] && value["copy"] == ["one", "two"]
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("YAML.load_file = %v, %v; want true, nil", got, err)
	}
	got, err = mrb.Eval(`
    begin
      YAML.load("cycle: &cycle [*cycle]\n", aliases: true)
      false
    rescue ArgumentError
      true
    end
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("YAML cyclic alias rejection = %v, %v; want true, nil", got, err)
	}
}

func TestYAMLLazyActivationIsPerState(t *testing.T) {
	first := oruby.MrbOpen()
	defer first.Close()
	second := oruby.MrbOpen()
	defer second.Close()

	if _, err := first.Resolve("yaml"); err != nil {
		t.Fatal(err)
	}
	got, err := first.Eval(`defined?(YAML) != nil`)
	if err != nil || !got.Bool() {
		t.Fatalf("YAML in first state = %v, %v; want true, nil", got, err)
	}
	got, err = second.Eval(`defined?(YAML).nil?`)
	if err != nil || !got.Bool() {
		t.Fatalf("YAML in second state = %v, %v; want true, nil", got, err)
	}
}
