package regexp

import "regexp"

// StringScanner helper struct
type StringScanner struct {
	Pos   int
	str   string
	prev  int
	match *MatchData
}

// NewStringScanner constuctor for StringScaner object
func NewStringScanner(str string) *StringScanner {
	return &StringScanner{str: str}
}

// Concat adds string
func (s *StringScanner) Concat(str string) {
	s.str += str
}

// IsBol check begin of line
func (s *StringScanner) IsBol(str string) bool {
	if s.Pos >= len(s.str) {
		return false
	}
	return (s.Pos == 0) || (s.str[s.Pos] == '\n')
}

// Captures returns string captures
func (s *StringScanner) Captures() []interface{} {
	if s.match == nil {
		return nil
	}
	return s.match.Captures()
}

// Charpos position
func (s *StringScanner) Charpos() int {
	return s.Pos
}

// Check using regex pattern
func (s *StringScanner) Check(pattern *regexp.Regexp) interface{} {
	oldpos := s.Pos
	oldprev := s.prev
	result := s.Scan(pattern)

	s.Pos = oldpos
	s.prev = oldprev

	return result
}

// CheckUntil regex pattern match
func (s *StringScanner) CheckUntil(pattern *regexp.Regexp) interface{} {
	oldpos := s.Pos
	oldprev := s.prev
	result := s.ScanUntil(pattern)

	s.Pos = oldpos
	s.prev = oldprev

	return result
}

// IsEos chek if position at the end of string
func (s *StringScanner) IsEos() bool {
	return s.Pos >= len(s.str)
}

// GetByte at the position
func (s *StringScanner) GetByte() byte {
	b := s.str[s.Pos]
	s.setPos(s.Pos + 1)
	return b
}

// Getch returns rune at position
func (s *StringScanner) Getch() rune {
	return []rune(s.str)[s.Pos]
}

func (s *StringScanner) setPos(newpos int) {
	s.prev = s.Pos
	s.Pos = newpos
}

// Match pattern
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

// Matched data
func (s *StringScanner) Matched(pattern *regexp.Regexp) interface{} {
	if s.match == nil {
		return nil
	}

	return s.match.StringOrNil(0)
}

// MatchedSize returns length of match or nil if there is no match
func (s *StringScanner) MatchedSize(pattern *regexp.Regexp) interface{} {
	if s.match == nil {
		return nil
	}

	result, _ := s.match.toString(0)
	return len(result)
}

// Peek string of given length at position
func (s *StringScanner) Peek(length int) string {
	if (s.Pos + length) >= len(s.str) {
		return s.str[s.Pos:]
	}

	return s.str[s.Pos : s.Pos+length]
}

// Pointer represents current position
func (s *StringScanner) Pointer() int {
	return s.Pos
}

// Terminate scan
func (s *StringScanner) Terminate() {
	s.Pos = 0
	s.prev = 0
	s.match = nil
}

// Scan pattern and return matchdata, or nil
func (s *StringScanner) Scan(pattern *regexp.Regexp) interface{} {
	r := regexp.MustCompile("^" + pattern.String())
	results := r.FindStringSubmatchIndex(s.str[s.Pos:])
	if len(results) == 0 {
		s.match = nil
		return nil
	}

	s.match = NewMatchData(r, s.str[s.Pos:], results, s.Pos)
	s.setPos(s.match.allEnd())

	return s.match.StringOrNil(0)
}

// Exist check if pattern match exists
func (s *StringScanner) Exist(pattern *regexp.Regexp) interface{} {
	in := s.CheckUntil(pattern)
	switch v := in.(type) {
	case string:
		return len(v)
	default:
		return nil
	}
}

// ScanUntil scans string using given patern until match
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

// Skip pattern, returns len or nil
func (s *StringScanner) Skip(pattern *regexp.Regexp) interface{} {
	in := s.Scan(pattern)
	switch v := in.(type) {
	case string:
		return len(v)
	default:
		return nil
	}
}

// SkipUntil skips string using given pattern until match
func (s *StringScanner) SkipUntil(pattern *regexp.Regexp) interface{} {
	in := s.ScanUntil(pattern)
	switch v := in.(type) {
	case string:
		return len(v)
	default:
		return nil
	}
}

// String returns scaned string
func (s *StringScanner) String() string {
	return s.str
}

func (s *StringScanner) setString(str string) string {
	s.str = str
	s.Terminate()

	return str
}

// Reset position to zero
func (s *StringScanner) Reset() {
	s.Pos = 0
	s.match = nil
}

// Unscan last scan
func (s *StringScanner) Unscan() {
	s.Pos = s.prev
}

// Size returns number of captures
func (s *StringScanner) Size() int {
	if s.match == nil {
		return 0
	}
	return len(s.match.Captures())
}

// Rest returns rest part of the string after match
func (s *StringScanner) Rest() string {
	if len(s.str) < (s.Pos + 1) {
		return ""
	}
	return s.str[s.Pos+1:]
}

// RestSize returns size of rest
func (s *StringScanner) RestSize() int {
	return len(s.Rest())
}

// PreMatch returns prematch string
func (s *StringScanner) PreMatch() string {
	if (s.match == nil) || (s.match.result[0] < 2) {
		return ""
	}
	return s.str[:s.match.result[0]-1]
}

// PostMatch returns postmatch string
func (s *StringScanner) PostMatch() string {
	if (s.match == nil) || ((s.match.result[1] + 1) >= len(s.str)) {
		return ""
	}

	return s.str[s.match.result[1]+1:]
}
