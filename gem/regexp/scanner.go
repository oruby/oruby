package regexp

import "regexp"

type StringScanner struct {
	Pos   int
	str   string
	prev  int
	match *MatchData
}

func NewStringScanner(str string) *StringScanner {
	return &StringScanner{ str: str	}
}

func (s *StringScanner) Concat(str string) {
	s.str += str
}

func (s *StringScanner) IsBol(str string) bool {
	if s.Pos >= len(s.str) {
		return false
	}
	return (s.Pos == 0) || (s.str[s.Pos] == '\n')
}

func (s *StringScanner) Captures() []interface{} {
	if s.match == nil {
		return nil
	}
	return s.match.Captures()
}

func (s *StringScanner) Charpos() int {
	return s.Pos
}

func (s *StringScanner) Check(pattern *regexp.Regexp) interface{} {
	oldpos := s.Pos
	oldprev := s.prev
	result := s.Scan(pattern)

	s.Pos = oldpos
	s.prev = oldprev

	return result
}

func (s *StringScanner) CheckUntil(pattern *regexp.Regexp) interface{} {
	oldpos := s.Pos
	oldprev := s.prev
	result := s.ScanUntil(pattern)

	s.Pos = oldpos
	s.prev = oldprev

	return result
}

func (s *StringScanner) IsEos() bool {
	return s.Pos >= len(s.str)
}

func (s *StringScanner) GetByte() byte {
	b := s.str[s.Pos]
	s.setPos(s.Pos + 1)
	return b
}

func (s *StringScanner) Getch() rune {
	return []rune(s.str)[s.Pos]
}

func (s *StringScanner) setPos(newpos int) {
	s.prev = s.Pos
	s.Pos = newpos
}

func (s *StringScanner) Match(pattern *regexp.Regexp) interface{} {
	results := pattern.FindStringSubmatchIndex(s.str[s.Pos:])
	if len(results) == 0 {
		s.match = nil
		return nil
	}

	s.match = NewMatchData(pattern, s.str[s.Pos:], results, s.Pos)

	result, _ := s.match.toString(0)
	return len(result)
}

func (s *StringScanner) Matched(pattern *regexp.Regexp) interface{} {
	if s.match == nil {
		return nil
	}

	return s.match.StringOrNil(0)
}

func (s *StringScanner) MatchedSize(pattern *regexp.Regexp) interface{} {
	if s.match == nil {
		return nil
	}

	result, _ := s.match.toString(0)
	return len(result)
}

func (s *StringScanner) Peek(length int) string {
	if (s.Pos + length) >= len(s.str) {
		return s.str[s.Pos:]
	}

	return s.str[s.Pos:s.Pos + length]
}

func (s *StringScanner) Pointer() int {
	return s.Pos
}

func (s *StringScanner) Termiate() {
	s.Pos = 0
	s.prev = 0
	s.match = nil
}

func (s *StringScanner) Scan(pattern *regexp.Regexp) interface{} {
	r := regexp.MustCompile("^"+pattern.String())
	results := r.FindStringSubmatchIndex(s.str[s.Pos:])
	if len(results) == 0 {
		s.match = nil
		return nil
	}

	s.match = NewMatchData(r, s.str[s.Pos:], results, s.Pos)
	s.setPos(s.match.allEnd())

	return s.match.StringOrNil(0)
}

func (s *StringScanner) Exist(pattern *regexp.Regexp) interface{} {
	in := s.CheckUntil(pattern)
	switch v := in.(type) {
	case string:
		return len(v)
	default:
		return nil
	}
}

func (s *StringScanner) ScanUntil(pattern *regexp.Regexp) interface{} {
	results := pattern.FindStringSubmatchIndex(s.str[s.Pos:])
	if len(results) == 0 {
		s.match = nil
		return nil
	}

	s.match = NewMatchData(pattern, s.str[s.Pos:], results, s.Pos)
	s.setPos(s.match.allEnd())

	return s.match.StringOrNil(0)
}

func (s *StringScanner) Skip(pattern *regexp.Regexp) interface{} {
	in := s.Scan(pattern)
	switch v := in.(type) {
	case string:
		return len(v)
	default:
		return nil
	}
}

func (s *StringScanner) SkipUntil(pattern *regexp.Regexp) interface{} {
	in := s.ScanUntil(pattern)
	switch v := in.(type) {
	case string:
		return len(v)
	default:
		return nil
	}
}

func (s *StringScanner) String() string {
	return s.str
}

func (s *StringScanner) setString(str string) string {
	s.str = str
	s.Termiate()

	return str
}

func (s *StringScanner) Reset() {
	s.Pos = 0
	s.match = nil
}

func (s *StringScanner) Unscan() {
	s.Pos = s.prev
}

func (s *StringScanner) Size() int {
	if s.match == nil {
		return 0
	}
	return len(s.match.Captures())
}

func (s *StringScanner) Rest() string {
	if len(s.str) < (s.Pos +1) {
		return ""
	}
	return s.str[s.Pos+1:]
}

func (s *StringScanner) RestSize() int {
	return len(s.Rest())
}

func (s *StringScanner) PreMatch() string {
	if (s.match == nil) || (s.match.result[0] < 2) {
		return ""
	}
	return s.str[:s.match.result[0]-1]
}

func (s *StringScanner) PostMatch() string {
	if (s.match == nil) || ((s.match.result[1]+1) >= len(s.str)) {
		return ""
	}

	return s.str[s.match.result[1]+1:]
}
