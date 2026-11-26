package erubi

import (
	"strings"
	"testing"

	"github.com/oruby/oruby"
)

var options *Engine

func setup(t *testing.T, input string, options map[string]interface{}) (*Engine, func()) {
	t.Helper()
	e := New(options)
	return e, func() { return }
}

func checkOutput(t *testing.T, input, binding, src, result string) {
	t.Helper()

	e, teardown := setup(t, input, nil)
	defer teardown()

	_, closer := checkOutputE(t, e, input, binding, src, result)
	defer closer()
}

func checkOutputE(t *testing.T, e *Engine, input, binding, src, result string) (*oruby.MrbState, func()) {
	t.Helper()
	mrb := oruby.MrbOpen()
	mrb.Require("erubi")

	err := e.Init(input)
	if err != nil {
		t.Fatal(err)
	}

	tsrc := e.Src()

	v, err := mrb.Eval(binding + "\n" + tsrc)
	if err != nil {
		t.Fatal(err)
	}

	s := mrb.String(v)
	if s != result {
		t.Errorf("expected eval '%v' to equal '%v'", mrb.String(v), result)
	}

	tsrc = strings.ReplaceAll(tsrc, "'.freeze;", "';")
	if tsrc != src {
		t.Errorf("expected tsrc '%v' to equal '%v'", tsrc, src)
	}

	return mrb, mrb.Close
}

// Tests ported from

func TestShouldHandleNoOptions(t *testing.T) {
	list := `list = ['&\'<>"2']`
	checkOutput(t,
		`<table>
 <tbody>
  <% i = 0
     list.each_with_index do |item, i| %>
  <tr>
   <td><%= i+1 %></td>
   <td><%== item %></td>
  </tr>
 <% end %>
 </tbody>
</table>
<%== i+1 %>
`, list,
		`_buf = ::String.new; _buf << '<table>
 <tbody>
';   i = 0
     list.each_with_index do |item, i| 
 _buf << '  <tr>
   <td>'; _buf << ( i+1 ).to_s; _buf << '</td>
   <td>'; _buf << ::Erubi.h(( item )); _buf << '</td>
  </tr>
';  end 
 _buf << ' </tbody>
</table>
'; _buf << ::Erubi.h(( i+1 )); _buf << '
';
_buf.to_s
`,
		`<table>
 <tbody>
  <tr>
   <td>1</td>
   <td>&amp;&#39;&lt;&gt;&quot;2</td>
  </tr>
 </tbody>
</table>
1
`)
}

func TestERBResultWithBinding(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	if _, err := mrb.Resolve("erb"); err != nil {
		t.Fatal(err)
	}
	got, err := mrb.Eval(`
    name = "ORuby"
    template = ERB.new("hello <%= name %>!")
    template.src.include?("name") && template.result(binding) == "hello ORuby!"
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("ERB binding render = %v, %v; want true, nil", got, err)
	}
}

func TestERBOptionsAndRun(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	mrb.Require("erb")
	got, err := mrb.Eval(`
    template = ERB.new("hello", eoutvar: "_output")
    template.src.include?("_output") && template.result == "hello"
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("ERB eoutvar = %v, %v; want true, nil", got, err)
	}
	got, err = mrb.Eval(`
    class ERBCapture
      attr_reader :value
      def initialize; @value = ""; end
      def write(s); @value << s; end
    end
    out = ERBCapture.new
    original = ERBCapture.new
    $stdout = original
    begin
      $stdout = out
      result = ERB.new("hello").run
    ensure
      $stdout = original
    end
    result.nil? && out.value == "hello"
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("ERB run = %v, %v; want true, nil", got, err)
	}
	got, err = mrb.Eval(`
    template = ERB.new("hello")
    checks = [proc { template.def_method(Module.new, "foo") },
              proc { template.def_module }, proc { template.def_class },
              proc { ERB.new("hello", trim_mode: "-") }]
    checks.all? do |operation|
      begin
        operation.call
        false
      rescue NotImplementedError
        true
      end
    end
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("ERB unsupported operations = %v, %v; want true, nil", got, err)
	}
}

func TestERBBindingHashSupport(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	got, err := mrb.Eval(`
    b = binding
    b.local_variable_set(:erb_item, "value")
    eval("erb_item", b) == "value"
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("binding local_variable_set = %v, %v; want true, nil", got, err)
	}
}

func TestERBEscape(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	mrb.Require("erb")
	got, err := mrb.Eval(`
    ERB::Escape.html_escape(%(<a href="x&y">'z'</a>)) == "&lt;a href=&quot;x&amp;y&quot;&gt;&#39;z&#39;&lt;/a&gt;" &&
      ERB::Util.h("a < b") == "a &lt; b" &&
      ERB::Util.h(42) == "42" &&
      ERB::Util.h("plain") == "plain" &&
      ERB::Util.u("a b/c") == "a%20b%2Fc"
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("ERB::Escape = %v, %v; want true, nil", got, err)
	}
}

func TestERBHashLocals(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	mrb.Require("erb")
	got, err := mrb.Eval(`
    $erb_expected = Object.new
    template = ERB.new("<%= item.equal?($erb_expected) %>:<%= value %>")
    template.result_with_hash(item: $erb_expected, value: 42) == "true:42" &&
      ERB.new("<%= defined?(item) %>").result_with_hash({}) == ""
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("ERB hash locals = %v, %v; want true, nil", got, err)
	}
	got, err = mrb.Eval(`
    begin
      ERB.new("x").result_with_hash({"not valid" => 1})
      false
    rescue NameError
      true
    end
  `)
	if err != nil || !got.Bool() {
		t.Fatalf("ERB invalid local name = %v, %v; want true, nil", got, err)
	}
}

func TestShouldStripOnlyWhitespaceForSpecificTags(t *testing.T) {
	list := `list = ['&\'<>"2']`
	checkOutput(t,
		`  <% 1 %>  
a
  <%- 2 %>  
b
  <%# 3 %>  
c
 /<% 1 %>  
a
/ <%- 2 %>  
b
//<%# 3 %>  
c
  <% 1 %> /
a
  <%- 2 %>/ 
b
  <%# 3 %>//
c
`,
		list,
		`_buf = ::String.new;   1   
 _buf << 'a
';   2   
 _buf << 'b
';
 _buf << 'c
 /'; 1 ; _buf << '  
'; _buf << 'a
/ '; 2 ; _buf << '  
'; _buf << 'b
//';
 _buf << '  
'; _buf << 'c
'; _buf << '  '; 1 ; _buf << ' /
a
'; _buf << '  '; 2 ; _buf << '/ 
b
'; _buf << '  '; _buf << '//
c
';
_buf.to_s
`,
		`a
b
c
 /  
a
/   
b
//  
c
   /
a
  / 
b
  //
c
`)
}

func TestShouldHandleEnsure(t *testing.T) {
	list := `list = ['&\'<>"2'] ; @a = 'bar'`
	e := New(nil)
	e.ensure = true
	e.bufvar = "@a"

	mrb, closer := checkOutputE(t, e,
		`<table>
 <tbody>
  <% i = 0
     list.each_with_index do |item, i| %>
  <tr>
   <td><%= i+1 %></td>
   <td><%== item %></td>
  </tr>
 <% end %>
 </tbody>
</table>
<%== i+1 %>
`, list, `begin; __original_outvar = @a if defined?(@a); @a = ::String.new; @a << '<table>
 <tbody>
';   i = 0
     list.each_with_index do |item, i| 
 @a << '  <tr>
   <td>'; @a << ( i+1 ).to_s; @a << '</td>
   <td>'; @a << ::Erubi.h(( item )); @a << '</td>
  </tr>
';  end 
 @a << ' </tbody>
</table>
'; @a << ::Erubi.h(( i+1 )); @a << '
';
@a.to_s
; ensure
  @a = __original_outvar
end
`, `<table>
 <tbody>
  <tr>
   <td>1</td>
   <td>&amp;&#39;&lt;&gt;&quot;2</td>
  </tr>
 </tbody>
</table>
1
`)
	defer closer()

	v, _ := mrb.Eval("@a")
	if mrb.String(v) != "bar" {
		t.Errorf(" @a.must_equal 'bar'. Got: %v", mrb.String(v))
	}
}
