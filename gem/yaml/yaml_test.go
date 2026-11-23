package yaml_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/stdlib"
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
    rescue Psych::DisallowedClass
      true
    end
    alias_rejected = begin
      YAML.load("base: &anchor [1]\ncopy: *anchor\n")
      false
    rescue Psych::AliasesNotEnabled
      true
    end
    object_dump = YAML.dump(Object.new) == "--- !ruby/object {}\n"
    values = []
    values << values
    cyclic_dump = YAML.dump(values) == "--- &1\n- *1\n"
    [wrong_type, syntax_error, custom_tag, alias_rejected, object_dump, cyclic_dump].map { |check| check ? "1" : "0" }.join
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
    rescue Psych::BadAlias
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
    cycle = YAML.load("cycle: &cycle [*cycle]\n", aliases: true)["cycle"]
    cycle[0].equal?(cycle)
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("YAML cyclic alias = %v, %v; want true, nil", got, err)
	}
	got, err = mrb.Eval(`
    alone = YAML.safe_load("a: &anchor [one]\n") == {"a" => ["one"]}
    truthy = YAML.safe_load("a: &anchor [one]\nb: *anchor\n", aliases: 1)["b"] == ["one"]
    unknown = begin
      YAML.safe_load("a: 1", no_such_option: 1)
      false
    rescue ArgumentError
      true
    end
    alone && truthy && unknown
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("YAML anchor and option validation = %v, %v; want true, nil", got, err)
	}
	got, err = mrb.Eval(`
    source = "base: &anchor \"" + ("x" * 100000) + "\"\ncopies:\n" + ("  - *anchor\n" * 90)
    v = YAML.safe_load(source, aliases: true)
    v["copies"].all? { |c| c.equal?(v["base"]) }
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("YAML aliases share the anchored value = %v, %v; want true, nil", got, err)
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

func TestYAMLDumpDocumentAndIO(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	got, err := mrb.Eval(`
    require "yaml"
    class YAMLWriteCapture
      attr_reader :contents
      def initialize; @contents = ""; end
      def write(text); @contents << text; end
    end
    out = YAMLWriteCapture.new
    value = ["a", "b"]
    written = YAML.dump(value, out)
    written.equal?(out) && out.contents == value.to_yaml &&
      out.contents.start_with?("---\n") && YAML.load(out.contents) == value &&
      YAML.dump("str") == "--- str\n"
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("YAML dump document and IO = %v, %v; want true, nil", got, err)
	}
}

func TestYAMLDumpOpenStructAndFileWithoutEnablingObjectLoad(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	// ostruct is the upstream library ORuby ships in lib/oruby
	lib, _ := filepath.Abs("../../lib/oruby/" + oruby.RubyVersion)
	if _, err := mrb.Eval(`$LOAD_PATH.unshift(` + strconv.Quote(lib) + `)`); err != nil {
		t.Fatal(err)
	}
	got, err := mrb.Eval(`
    require "yaml"
    require "ostruct"
    require "io/file"
    object_yaml = YAML.dump(OpenStruct.new("age" => 20, "name" => "John"))
    file = File.new("yaml_test.go")
    begin
      file_yaml = YAML.dump(file)
    ensure
      file.close
    end
    safe_load = begin
      YAML.load(object_yaml)
      false
    rescue Psych::DisallowedClass
      true
    end
    [
      ["--- !ruby/object:OpenStruct\nage: 20\nname: John\n", "--- !ruby/object:OpenStruct\ntable:\n  :age: 20\n  :name: John\n"].include?(object_yaml),
      file_yaml == "--- !ruby/object:File {}\n",
      safe_load
    ].map { |check| check ? "1" : "0" }.join
  `)
	checks := mrb.String(got)
	if err != nil || checks != "111" {
		t.Fatalf("YAML object dump and safe load checks = %s, %v; want 111, nil", checks, err)
	}
}

func TestYAMLDumpSymbol(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	got, err := mrb.Eval(`require "yaml"; YAML.dump(:locked)`)
	if err != nil {
		t.Fatal(err)
	}
	if output := mrb.String(got); output != "--- :locked\n" {
		t.Fatalf("YAML.dump(:locked) = %q; want %q", output, "--- :locked\n")
	}
}

func TestPsychTagsAndMetadata(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`
		require "yaml"
		raise "load_tags" unless Psych.load_tags.is_a?(Hash)
		raise "dump_tags" unless Psych.dump_tags.is_a?(Hash)
		raise "domain_types" unless Psych.domain_types.is_a?(Hash)
		Psych.add_builtin_type("custom") { |type, val| val }
		raise "builtin added" unless Psych.domain_types.key?("yaml.org,2002:custom")
		Psych.remove_type("yaml.org,2002:custom")
		raise "builtin removed" if Psych.domain_types.key?("yaml.org,2002:custom")
		Psych.add_domain_type("example.com,2026", "thing") { |type, val| val }
		raise "domain added" unless Psych.domain_types.key?("example.com,2026:thing")
		raise "libyaml_version" unless Psych.libyaml_version.is_a?(Array) && Psych.libyaml_version.length == 3
	`)
	if err != nil {
		t.Fatalf("Psych tags/metadata failed: %v", err)
	}
}
