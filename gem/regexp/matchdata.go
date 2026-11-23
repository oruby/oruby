package regexp

import (
	"fmt"

	"github.com/oruby/oruby"
)

// MatchData helper struct
type MatchData struct {
	r      *Regexp
	s      string
	enc    string
	names  []string
	result []int
}

// Enc returns the source string's encoding name
func (m *MatchData) Enc() string {
	return m.enc
}

// NewMatchData creates MatchData for matched regex
func NewMatchData(regx *Regexp, s string, result []int, pos int) *MatchData {
	gonames := regx.subexpNames()
	names := make([]string, 0, len(gonames))
	hasNames := false
	for _, name := range gonames {
		hasNames = hasNames || (name != "")
	}

	var ret []int

	if hasNames {
		ret = make([]int, 0, len(result))
		for idx, name := range gonames {
			if idx == 0 || name != "" {
				names = append(names, name)
				ret = append(ret, result[2*idx], result[2*idx+1])
			}
		}
	} else {
		names = append(names, gonames...)
		ret = result
	}

	// Adjust result for ruby regex position parameter
	if pos > 0 {
		for i := range ret {
			ret[i] += pos
		}
	}

	return &MatchData{regx, s, "", names, ret}
}

func (m *MatchData) String() string {
	return m.s
}

// IsEqual compares two MatchData objects
func (m *MatchData) IsEqual(m2 *MatchData) bool {
	if len(m.result) != len(m2.result) {
		return false
	}

	for i, v := range m.result {
		if v != m2.result[i] {
			return false
		}
	}

	return m.r.source == m2.r.source && m.r.options == m2.r.options && m.s == m2.s
}

func (m *MatchData) toString(idx int) (string, bool) {
	if idx < 0 {
		idx = m.Size() + idx
	}
	if (idx < 0) || (idx >= m.Size()) {
		return "", true
	}
	if m.result[idx*2] < 0 {
		return "", true
	}

	return m.s[m.result[2*idx]:m.result[2*idx+1]], false
}

// StringOrNil returns string pointer, which s nil if match
// at given index does not exist
func (m *MatchData) StringOrNil(idx int) *string {
	result, isNil := m.toString(idx)
	if isNil {
		return nil
	}
	return &result
}

// byteBegin is the byte offset where group idx starts (-1 if unset)
func (m *MatchData) byteBegin(idx int) int {
	if idx < 0 || idx >= m.Size() {
		return -1
	}
	return m.result[idx*2]
}

// Begin is the character offset where group idx starts (MatchData#begin)
func (m *MatchData) charBegin(idx int) (interface{}, error) {
	if (idx < 0) || (idx >= m.Size()) {
		return nil, fmt.Errorf("index %v out of matches", idx)
	}
	if m.result[idx*2] < 0 {
		return nil, nil
	}
	return regexpCharIndex(m.s, m.result[idx*2], m.enc), nil
}

// End is the character offset where group idx ends (MatchData#end)
func (m *MatchData) charEnd(idx int) (interface{}, error) {
	if (idx < 0) || (idx >= m.Size()) {
		return nil, fmt.Errorf("index %v out of matches", idx)
	}
	if m.result[idx*2+1] < 0 {
		return nil, nil
	}
	return regexpCharIndex(m.s, m.result[idx*2+1], m.enc), nil
}

// Byteoffset returns [begin, end] byte offsets of group idx (MatchData#byteoffset)
func (m *MatchData) byteOffset(idx int) ([]interface{}, error) {
	if (idx < 0) || (idx >= m.Size()) {
		return nil, fmt.Errorf("index %v out of matches", idx)
	}
	if m.result[idx*2] < 0 {
		return []interface{}{nil, nil}, nil
	}
	return []interface{}{m.result[idx*2], m.result[idx*2+1]}, nil
}

// Size returns number of matches
func (m *MatchData) Size() int {
	return len(m.result) / 2
}

func (m *MatchData) allEnd() int {
	if (len(m.result) < 2) || (m.result[0] < 0) {
		return 0
	}
	return m.result[1]
}

// ToS string representation of match data
func (m *MatchData) ToS() string {
	ret, _ := m.toString(0)
	return ret
}

// ToA retuns slice of matches as string or nil values
func (m *MatchData) ToA() []interface{} {
	ret := make([]interface{}, m.Size())
	for i := 0; i < m.Size(); i++ {
		s, isNil := m.toString(i)
		if !isNil {
			ret[i] = s
		} else {
			ret[i] = nil
		}
	}
	return ret
}

// Captures returns slice of captured matches
func (m *MatchData) Captures() []interface{} {
	return m.ToA()[1:]
}

// Names of captures
func (m *MatchData) Names() []string {
	return m.names[1:]
}

// Inspect represents MatchData object as string
func (m *MatchData) Inspect() string {
	ret := "#<MatchData "
	a := m.ToA()
	for i, v := range a {
		q, isNil := m.toString(i)
		if isNil {
			q = "nil"
		} else {
			q = fmt.Sprintf("\"%v\"", v)
		}
		if i == 0 {
			ret += q
			continue
		}
		if m.names[i] == "" {
			ret += fmt.Sprintf(" %v:%v", i, q)
		} else {
			ret += fmt.Sprintf(" %v:%v", m.names[i], q)
		}

	}
	return ret + ">"
}

// NamedCaptures returns hash of captures, where
// keys are capture nameswith captures as values
func (m *MatchData) NamedCaptures() map[string]interface{} {
	ret := make(map[string]interface{}, len(m.names))
	for i, name := range m.names {
		if i == 0 {
			continue
		}
		s, isNil := m.toString(i)
		if !isNil {
			ret[name] = s
		} else {
			ret[name] = nil
		}

	}
	return ret
}

// Offset returns [begin, end] character offsets of group idx (MatchData#offset)
func (m *MatchData) charOffset(idx int) ([]interface{}, error) {
	if (idx < 0) || (idx >= m.Size()) {
		return nil, fmt.Errorf("index %v out of matches", idx)
	}
	if m.result[idx*2] < 0 {
		return []interface{}{nil, nil}, nil
	}
	return []interface{}{regexpCharIndex(m.s, m.result[idx*2], m.enc), regexpCharIndex(m.s, m.result[idx*2+1], m.enc)}, nil
}

// PostMatch returns string after match
func (m *MatchData) PostMatch() string {
	if len(m.result) < 2 || m.result[1] < 0 || m.result[1] >= len(m.s) {
		return ""
	}
	return m.s[m.result[1]:]
}

// PreMatch returns string before match
func (m *MatchData) PreMatch() string {
	if len(m.result) < 2 || m.result[0] <= 0 {
		return ""
	}
	if m.result[0] > len(m.s) {
		return m.s
	}
	return m.s[:m.result[0]]
}

// Regexp returns underlyng Regexp object
func (m *MatchData) Regexp() *Regexp {
	return m.r
}

// hasNames: the regexp has named groups (its numbered ones do not capture)
func (m *MatchData) hasNames() bool {
	return len(m.names) > 1 && m.names[1] != ""
}

// mdStr is s, a part of the subject, in the subject's encoding (as MRI's
// MatchData strings are): as it is for UTF-8, tagged by force_encoding for
// another
func mdStr(mrb *oruby.MrbState, m *MatchData, s string) oruby.Value {
	v := mrb.StrNew(s)
	if m.enc == "" || m.enc == "UTF-8" {
		return v
	}
	if m.enc == "ASCII-8BIT" {
		if binary, err := mrb.Funcall(v, oruby.Sym_b); err == nil {
			return binary.Value()
		}
	}
	forceEncoding(mrb, v.Value(), m.enc)
	return v
}

// mdGroup is group i, or nil
func mdGroup(mrb *oruby.MrbState, m *MatchData, i int) oruby.Value {
	if s, isNil := m.toString(i); !isNil {
		return mdStr(mrb, m, s)
	}
	return oruby.Nil
}

func mdToA(mrb *oruby.MrbState, m *MatchData, from int) oruby.Value {
	n := m.Size()
	ary := mrb.AryNewCapa(n - from)
	for i := from; i < n; i++ {
		ary.Push(mdGroup(mrb, m, i))
	}
	return ary.Value()
}

func matchDataToA(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mdToA(mrb, mrb.Data(self).(*MatchData), 0)
}

func matchDataCaptures(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mdToA(mrb, mrb.Data(self).(*MatchData), 1)
}

func matchDataPreMatch(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	m := mrb.Data(self).(*MatchData)
	return mdStr(mrb, m, m.PreMatch())
}

func matchDataPostMatch(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	m := mrb.Data(self).(*MatchData)
	return mdStr(mrb, m, m.PostMatch())
}

// MatchData#string: the subject, frozen, one object per MatchData
func matchDataString(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	sym := mrb.Intern("__oruby_string")
	if v := mrb.IVGet(self, sym); !v.IsNil() {
		return v
	}
	m := mrb.Data(self).(*MatchData)
	v := mdStr(mrb, m, m.s)
	mrb.Call(v, "freeze")
	_ = mrb.IVSet(self, sym, v)
	return v
}
