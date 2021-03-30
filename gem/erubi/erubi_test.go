package erubi

import (
	"github.com/oruby/oruby"
	"strings"
	"testing"
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

	err := e.Init(input)
	if err != nil {
		t.Fatal(err)
	}

	tsrc := e.src

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
   <td>&amp;&#39;&lt;&gt;&#34;2</td>
  </tr>
 </tbody>
</table>
1
`)
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
'; _buf << '  ';; _buf << '//
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
