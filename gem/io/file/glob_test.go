package file

import (
	"github.com/oruby/oruby/gem/assert"
	"testing"
)

// GlobFull test helper with only one string pattern
func GlobFull(t *testing.T, pattern string, flags int, base string, onFound func(f string)error) {
	t.Helper()
	err := glob([]string{pattern}, flags, base, onFound)
	if err != nil {
		t.Error(err)
	}
}

// Glob test helper with only one string pattern, no flags and current dir base, and items list
func Glob(t *testing.T, pattern string, items ...string) {
	t.Helper()
	found := make([]string, 0, 15)
	err := glob([]string{pattern}, 0, "",  func(f string)error {
		t.Helper()
		found = append(found, f)
		return nil
	})
	if err != nil {
		t.Error(err)
	}
	for _, item := range items {
		assert.Include(t, item, found)
	}
}

// Glob test helper with only one string pattern, no flags and current dir base, and items list
func GlobFlag(t *testing.T, pattern string, flag int, items ...string) {
	t.Helper()
	found := make([]string, 0, 15)
	err := glob([]string{pattern}, flag, "",  func(f string)error {
		t.Helper()
		found = append(found, f)
		return nil
	})
	if err != nil {
		t.Error(err)
	}
	for _, item := range items {
		assert.Include(t, item, found)
	}
}

// Glob test helper with only one string pattern, no flags and current dir base, and items list
func GlobNot(t *testing.T, pattern string, items ...string) {
	t.Helper()
	found := make([]string, 0, 15)
	err := glob([]string{pattern}, 0, "",  func(f string)error {
		t.Helper()
		found = append(found, f)
		return nil
	})
	if err != nil {
		t.Error(err)
	}
	for _, item := range items {
		assert.NotInclude(t, item, found)
	}
}

func f(t *testing.T, items ...string) func(f string)error{
	t.Helper()
	return func(f string)error {
		t.Helper()
		for _, item := range items {
			assert.Equal(t, item, f)
		}
		return nil
	}
}

func Test_glob(t *testing.T) {
	Glob(t, "testdata/*.txt", "testdata/test.txt")
	Glob(t, "testdata/[^z]*.txt",  "testdata/test.txt")
	GlobNot(t, "testdata/*.txt", "testdata/glob/globfile.txt")
}

func Test_globPath2(t *testing.T) {
	Glob(t,"testdata/*.[a-z][a-z][a-z]", "testdata/test.txt", "testdata/zero.txt")
}

func Test_globPath(t *testing.T) {
	GlobFlag(t,"**/testdata" ,fnmPathname, "testdata")
}

func Test_globPath3(t *testing.T) {
	GlobFlag(t,"**/testdata/**/*.txt", fnmPathname,
		"testdata/zero.txt",
		"testdata/test.txt",
		"testdata/glob/globfile.txt",
		"testdata/glob/a/afile.txt",
	)
}

func Test_globPath4(t *testing.T) {
	GlobFlag(t,"**/testdata/*.txt", fnmPathname,
		"testdata/zero.txt",
		"testdata/test.txt",
	)
}

func Test_globPath5(t *testing.T) {
	//GlobFlag(t,"testdata/test.tx?", 0,	 "testdata/test.txt")
	GlobFlag(t,"testdata/test.tx?", fnmPathname,	 "testdata/test.txt")
}
