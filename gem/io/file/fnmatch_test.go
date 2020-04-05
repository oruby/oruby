package file

import (
	"github.com/oruby/oruby/gem/assert"
	"runtime"
	"testing"
)

func fnm(t *testing.T, pattern, name string, flags ...int) bool {
	t.Helper()
	f := 0
	for _, flag := range flags {
		f |= flag
	}
	result, err := fnmatch(name, pattern, f)
	assert.NilError(t, err)
	return result
}

func flagsToStr(flags ...int) (result string) {
	f := 0
	for _, flag := range flags {
		f |= flag
	}
	if f&fnmPathname != 0 { result += " fnmPathname" }
	if f&fnmDotmatch != 0 { result += " fnmDotmatch" }
	if f&fnmNoescape != 0 { result += " fnmNoescape" }
	if f&fnmCasefold != 0 { result += " fnmCasefold" }
	if f&fnmExtglob  != 0 { result += " fnmExtglob"  }
	if result != "" { result = " flags:" + result }
	return
}

func fnmTrue(t *testing.T, pattern, name string, flags ...int)  {
	t.Helper()
	if !fnm(t, pattern, name, flags...) {
		t.Errorf(`"%v" expected to match "%v" %v`, pattern, name, flagsToStr(flags...))
	}
}

func fnmFalse(t *testing.T, pattern, name string, flags ...int)  {
	t.Helper()
	if fnm(t, pattern, name, flags...) {
		t.Errorf(`"%v" expected NOT to match "%v" %v`, pattern, name, flagsToStr(flags...))
	}
}

func Test_fnmatch(t *testing.T) {
	fnmTrue(t, "cat", "cat")
	fnmFalse(t, "cat", "category")
	fnmFalse(t, "c{at,ub}s", "cats")
	fnmTrue(t, "c{at,ub}s", "cats", fnmExtglob)
	fnmTrue(t, "c?t", "cat")
	fnmFalse(t, "c??t", "cat")
	fnmTrue(t, "c*", "cats")
	fnmTrue(t, "c*t", "c/a/b/t")
	fnmTrue(t, "ca[a-z]", "cat")
	fnmFalse(t, "ca[^t]", "cat")
	fnmFalse(t, "cat", "CAT")
	fnmTrue(t, "cat", "CAT", fnmCasefold)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		fnmTrue(t, "cat", "CAT", fnmSyscase)
	} else {
		fnmFalse(t, "cat", "CAT", fnmSyscase)
	}
	fnmFalse(t, "?", "/", fnmPathname)
	fnmFalse(t, "*", "/", fnmPathname)
	fnmFalse(t, "[/]", "/", fnmPathname)

	fnmTrue(t, "\\?", "?")
	fnmTrue(t, "\\a", "a")
	fnmTrue(t, "\\a", "\\a", fnmNoescape)
	fnmTrue(t, "[\\?]", "?")

	fnmFalse(t, "*", ".profile")
	fnmTrue(t, "*", ".profile", fnmDotmatch)
	fnmTrue(t, ".*", ".profile")

	fnmFalse(t, "**/*.rb", "main.rb")
	fnmFalse(t, "**/*.rb", "./main.rb")
	fnmTrue(t, "**/*.rb", "lib/song.rb")
	fnmTrue(t, "**.rb", "main.rb")
	//TODO: this fails - I have no idea wath is the logic behing this
	fnmFalse(t, "**.rb", "./main.rb")
	fnmTrue(t, "**.rb", "lib/song.rb")
	fnmTrue(t, "*", "dave/.profile")

	fnmFalse(t, "**/foo", "foo")
	fnmTrue(t, "**/foo", "foo", fnmPathname)

	fnmTrue(t, "**/foo", "a/b/c/foo", fnmPathname)
	fnmTrue(t, "**/foo", "/a/b/c/foo", fnmPathname)
	fnmTrue(t, "**/foo", "c:/a/b/c/foo", fnmPathname)

	fnmFalse(t, "**/foo", "a/.b/c/foo", fnmPathname)
	fnmTrue(t, "**/foo", "a/.b/c/foo", fnmPathname|fnmDotmatch)
}

func Test_fnmatchDotmatch(t *testing.T)	{
	fnmFalse(t, "*/*", "dave/.profile", fnmPathname)
	fnmTrue(t, "*/*", "dave/.profile", fnmPathname|fnmDotmatch)
}

func Test_fnmatchComplex(t *testing.T) {
	fnmTrue(t ,"**/testdata/**/*.txt","testdata/zero.txt", fnmPathname)
	fnmTrue(t, "**/testdata/**/*.txt", "testdata/test.txt",fnmPathname)
	fnmTrue(t, "**/testdata/**/*.txt", "testdata/glob/globfile.txt",fnmPathname)
	fnmTrue(t, "**/testdata/**/*.txt", "testdata/glob/a/afile.txt",fnmPathname)

	fnmTrue(t ,"**/testdata/*.txt","testdata/zero.txt",fnmPathname)
	fnmTrue(t, "**/testdata/*.txt","testdata/test.txt",fnmPathname)
	fnmFalse(t,"**/testdata/*.txt", "testdata/glob/globfile.txt",fnmPathname)
	fnmFalse(t,"**/testdata/*.txt", "testdata/glob/a/afile.txt",fnmPathname)
}

func Test_StarPath(t *testing.T) {
	fnmTrue(t,"*", "/")
	fnmFalse(t,"*", "/", fnmPathname)
}

func Test_Subdir(t *testing.T) {
	fnmTrue(t,"testdata/test.tx?", "testdata/test.txt", fnmPathname)
}

