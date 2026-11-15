package stdlib_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/stdlib"
)

func TestTSortLazyLoadAndOrdering(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	if _, err := mrb.Resolve("load"); err != nil {
		t.Fatal(err)
	}

	got, err := mrb.Eval(`require "tsort"`)
	if err != nil || !got.Bool() {
		t.Fatalf("first require(tsort) = %v, %v; want true, nil", got, err)
	}
	got, err = mrb.Eval(`require "tsort"`)
	if err != nil || got.Bool() {
		t.Fatalf("repeat require(tsort) = %v, %v; want false, nil", got, err)
	}

	got, err = mrb.Eval(`
    class TSortTestGraph
      include TSort
      def initialize(edges); @edges = edges; end
      def tsort_each_node; @edges.each_key { |node| yield node }; end
      def tsort_each_child(node); (@edges[node] || []).each { |child| yield child }; end
    end
    graph = TSortTestGraph.new({a: [:b, :c], b: [:c], c: []})
    order = graph.tsort
    order == [:c, :b, :a] && graph.tsort_each.to_a == order
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("TSort ordering and enumerator = %v, %v; want true, nil", got, err)
	}
}

func TestTSortCyclesAndStronglyConnectedComponents(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	if _, err := mrb.Resolve("load"); err != nil {
		t.Fatal(err)
	}
	if _, err := mrb.Eval(`require "tsort"`); err != nil {
		t.Fatal(err)
	}

	got, err := mrb.Eval(`
    class TSortComponentGraph
      include TSort
      def initialize(edges); @edges = edges; end
      def tsort_each_node; @edges.each_key { |node| yield node }; end
      def tsort_each_child(node); (@edges[node] || []).each { |child| yield child }; end
    end
    graph = TSortComponentGraph.new({a: [:b], b: [:a], z: [:b]})
    components = graph.strongly_connected_components
    enumerated = graph.each_strongly_connected_component.to_a
    normalized = components.map { |component| component.map(&:to_s).sort.join(",") }.sort
    cycle_raised = begin
      graph.tsort
      false
    rescue TSort::Cyclic
      true
    end
    normalized == ["a,b", "z"] && enumerated == components && cycle_raised
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("TSort SCC/cycle = %v, %v; want true, nil", got, err)
	}
}

func TestTSortIsAvailableIndependentlyPerState(t *testing.T) {
	first := oruby.MrbOpen()
	defer first.Close()
	second := oruby.MrbOpen()
	defer second.Close()

	if _, err := first.Resolve("load"); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Resolve("load"); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Eval(`require "tsort"`); err != nil {
		t.Fatal(err)
	}
	got, err := first.Eval(`defined?(TSort) != nil`)
	if err != nil || !got.Bool() {
		t.Fatalf("TSort in first state = %v, %v; want true, nil", got, err)
	}
	got, err = second.Eval(`defined?(TSort).nil?`)
	if err != nil || !got.Bool() {
		t.Fatalf("TSort in second state = %v, %v; want true, nil", got, err)
	}
}
