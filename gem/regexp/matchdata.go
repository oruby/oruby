package regexp

import (
	"fmt"
	"regexp"
)

// MatchData helper struct
type MatchData struct {
	r      *regexp.Regexp
	s      string
	names  []string
	result []int
}

// NewMatchData creates MatchData for matched regex
func NewMatchData(regx *regexp.Regexp, s string, result []int, pos int) *MatchData {
	gonames := regx.SubexpNames()
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
		copy(names, gonames)
		ret = result
	}

	// Adjust result for ruby regex position parameter
	if pos > 0 {
		for i := range ret {
			ret[i] += pos
		}
	}

	return &MatchData{regx, s, names, ret}
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

	return (m.r.String() == m2.r.String()) && (m.s == m2.s)
}

func (m *MatchData) toString(idx int) (string, bool) {
	if idx < 0 {
		idx = m.Size() - idx
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

// Begin position of match at index
func (m *MatchData) Begin(idx int) (int, error) {
	if (idx < 0) || (idx >= m.Size()) {
		return -1, fmt.Errorf("index %v out of matches", idx)
	}

	return m.result[idx*2], nil
}

// End position of match at index
func (m *MatchData) End(idx int) (int, error) {
	if (idx < 0) || (idx >= m.Size()) {
		return -1, fmt.Errorf("index %v out of matches", idx)
	}

	return m.result[idx*2+1], nil
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

// Offset returns capture offsets
func (m *MatchData) Offset(idx int) ([]interface{}, error) {
	if (idx < 0) || (idx >= m.Size()) {
		return nil, fmt.Errorf("index %v out of matches", idx)
	}

	if m.result[idx*2] < 0 {
		return []interface{}{nil, nil}, nil
	}

	return []interface{}{m.result[idx*2], m.result[idx*2+1]}, nil
}

// PostMatch returns string after match
func (m *MatchData) PostMatch() string {
	last := len(m.s) - 1
	for i := len(m.result) - 1; i >= 0; i-- {
		if m.result[i] >= 0 {
			last = m.result[i]
			break
		}
	}

	if last >= len(m.s)-1 {
		return ""
	}

	return m.s[last+1:]
}

// PreMatch returns string before match
func (m *MatchData) PreMatch() string {
	first := 0
	for i := 0; i < len(m.result); i++ {
		if m.result[i] >= 0 {
			first = m.result[i]
			break
		}
	}

	if first == 0 {
		return ""
	}

	return m.s[:first-1]
}

// Regexp returns underlyng Regexp object
func (m *MatchData) Regexp() *regexp.Regexp {
	return m.r
}
