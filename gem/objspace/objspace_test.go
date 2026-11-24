package objspace_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/json"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/objspace"
	_ "github.com/oruby/oruby/gem/stdlib"
)

func TestObjSpace(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`
		require "objspace"
		require "json"

		# Immediates have memsize 0
		raise "nil memsize" unless ObjectSpace.memsize_of(nil) == 0
		raise "int memsize" unless ObjectSpace.memsize_of(42) == 0
		raise "sym memsize" unless ObjectSpace.memsize_of(:foo) == 0
		raise "bool memsize" unless ObjectSpace.memsize_of(true) == 0

		# Heap objects have positive memsize scaling with contents
		small_str = "hi"
		big_str = "x" * 1000
		raise "small_str memsize" unless ObjectSpace.memsize_of(small_str) > 0
		raise "big_str memsize" unless ObjectSpace.memsize_of(big_str) > ObjectSpace.memsize_of(small_str) + 900

		ary = Array.new(50, "a")
		raise "ary memsize" unless ObjectSpace.memsize_of(ary) >= 50 * 16

		h = { "a" => 1, "b" => 2 }
		raise "hash memsize" unless ObjectSpace.memsize_of(h) > 48

		# memsize_of_all
		all_sz = ObjectSpace.memsize_of_all
		str_sz = ObjectSpace.memsize_of_all(String)
		raise "memsize_of_all" unless all_sz > str_sz && str_sz > 0

		# count_objects_size
		sizes = ObjectSpace.count_objects_size
		raise "TOTAL missing" unless sizes[:TOTAL] > 0
		raise "T_STRING missing" unless sizes[:T_STRING] > 0

		# count_tdata_objects / count_symbols / count_imemo_objects / count_nodes
		raise "count_tdata_objects" unless ObjectSpace.count_tdata_objects.is_a?(Hash)
		raise "count_symbols" unless ObjectSpace.count_symbols[:immortal_symbol] > 0
		raise "count_imemo_objects" unless ObjectSpace.count_imemo_objects.is_a?(Hash)
		raise "count_nodes" unless ObjectSpace.count_nodes.is_a?(Hash)

		# internal_class_of / internal_super_of
		raise "internal_class_of" unless ObjectSpace.internal_class_of("abc") == String
		sup = ObjectSpace.internal_super_of(String)
		while sup.is_a?(ObjectSpace::InternalObjectWrapper)
			raise "wrapper type" unless sup.type == :T_ICLASS
			sup = ObjectSpace.internal_super_of(sup)
		end
		raise "internal_super_of chain: #{sup.inspect}" unless sup == Object

		# reachable_objects_from & reachable_objects_from_root
		raise "immediate reachable" unless ObjectSpace.reachable_objects_from(123).nil?
		child = "target_string"
		holder = [child]
		refs = ObjectSpace.reachable_objects_from(holder)
		raise "reachable missing child" unless refs.include?(child)
		roots = ObjectSpace.reachable_objects_from_root
		raise "roots hash" unless roots.is_a?(Hash) && !roots.empty?

		# dump & dump_all -> valid JSON!
		dumped = ObjectSpace.dump("hello\nworld")
		parsed = JSON.parse(dumped)
		raise "dump type" unless parsed["type"] == "STRING"
		raise "dump value" unless parsed["value"] == "hello\nworld"
		raise "dump bytesize" unless parsed["bytesize"] == 11
		raise "dump memsize" unless parsed["memsize"] > 0

		all_dump = ObjectSpace.dump_all(output: :string)
		lines = all_dump.lines
		raise "dump_all empty" unless lines.length > 10
		first = JSON.parse(lines.first)
		raise "dump_all json" unless first["address"] && first["type"]

		# Allocation tracing
		class Widget
			def initialize(name)
				@name = name
			end
		end

		traced = nil
		ObjectSpace.trace_object_allocations do
			traced = Widget.new("sprocket")
		end
		raise "allocation_sourceline" unless ObjectSpace.allocation_sourceline(traced).is_a?(Integer)
		raise "allocation_class_path" unless ObjectSpace.allocation_class_path(traced).nil?
		raise "allocation_generation" unless ObjectSpace.allocation_generation(traced).is_a?(Integer)

		ObjectSpace.trace_object_allocations_clear
		raise "cleared" unless ObjectSpace.allocation_sourceline(traced).nil?
	`)
	if err != nil {
		t.Fatalf("ObjSpace test failed: %v", err)
	}
}

type sizedData struct{ n int }

func (s *sizedData) Memsize() int { return s.n }

func TestObjSpaceCDataMemsizeHook(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	cls := mrb.DefineClass("SizedData", mrb.ObjectClass())
	oruby.MrbSetInstanceTT(cls, oruby.MrbTTCData)
	d := &sizedData{n: 4096}
	mrb.GVSet(mrb.Intern("$sized"), mrb.DataWrapInterface(cls, d))

	_, err := mrb.Eval(`
		require "objspace"
		sz = ObjectSpace.memsize_of($sized)
		raise "expected memsize >= 4096, got #{sz}" unless sz >= 4096 + 48
	`)
	if err != nil {
		t.Fatalf("CData Memsize hook test failed: %v", err)
	}
}
