package file

import (
	"io/ioutil"
	"os"
	"runtime"
	"testing"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/assert"
)

func Test_dirOpen(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval("Dir.open 'testdata'")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTCData)

	ret, err = mrb.Eval("Dir.open('testdata') {|dir| dir.path }")
	assert.NilError(t, err)
	assert.Equal(t, ret.String(), "testdata")
}

func Test_dirForeach(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	mrb.SetGV("$a", mrb.AryNew())
	_, err := mrb.Eval(`Dir.foreach('testdata') {|x| $a << x }`)
	assert.NilError(t, err)

	a := mrb.GetGV("$a")
	assert.Equal(t, a.Type(), oruby.MrbTTArray)
	assert.Include(t, ".", mrb.Intf(a))
	assert.Include(t, "..", mrb.Intf(a))
	assert.Include(t, "test.txt", mrb.Intf(a))
	assert.Include(t, "zero.txt", mrb.Intf(a))
}

func Test_dirEntries(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval("Dir.entries 'testdata'")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTArray)
	assert.Include(t, ".", mrb.Intf(ret))
	assert.Include(t, "..", mrb.Intf(ret))
	assert.Include(t, "test.txt", mrb.Intf(ret))
	assert.Include(t, "zero.txt", mrb.Intf(ret))
}

func Test_dirSEachChild(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	mrb.SetGV("$a", mrb.AryNew())
	_, err := mrb.Eval(`Dir.each_child('testdata') {|x| $a << x }`)
	assert.NilError(t, err)

	a := mrb.GetGV("$a")
	assert.Equal(t, a.Type(), oruby.MrbTTArray)
	assert.NotInclude(t, ".", mrb.Intf(a))
	assert.NotInclude(t, "..", mrb.Intf(a))
	assert.Include(t, "test.txt", mrb.Intf(a))
	assert.Include(t, "zero.txt", mrb.Intf(a))
}

func Test_dirChildren(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval("Dir.children 'testdata'")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTArray)
	assert.NotInclude(t, ".", mrb.Intf(ret))
	assert.NotInclude(t, "..", mrb.Intf(ret))
	assert.Include(t, "test.txt", mrb.Intf(ret))
	assert.Include(t, "zero.txt", mrb.Intf(ret))
}

func Test_dirInitialize(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval("Dir.open 'testdata'")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTCData)
}

func Test_dirFileno(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval("Dir.open('testdata').fileno")
	assert.NilError(t, err)

	if runtime.GOOS == "windows" {
		assert.True(t, ret.IsNil())
		return
	}

	assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)
}

func Test_dirPath(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval("Dir.open('testdata').path")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTString)
	assert.Equal(t, ret.String(), "testdata")
}

func Test_dirInspect(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval("Dir.open('testdata').inspect")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTString)
	assert.Equal(t, ret.String(), "#<Dir:testdata>")
}

func Test_dirRead(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	dir, err := mrb.Eval("Dir.open('testdata')")
	assert.NilError(t, err)

	ret := dir.Call("read")
	assert.NilError(t, mrb.Err())
	assert.Equal(t, mrb.Intf(ret), ".")

	ret = dir.Call("read")
	assert.NilError(t, mrb.Err())
	assert.Equal(t, mrb.Intf(ret), "..")

	ret = dir.Call("read")
	assert.NilError(t, mrb.Err())
	assert.Include(t, mrb.Intf(ret), "test.txt", "zero.txt", "glob")

	ret = dir.Call("read")
	assert.NilError(t, mrb.Err())
	assert.Include(t, mrb.Intf(ret), "test.txt", "zero.txt", "glob")
}

func Test_dirEach(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	mrb.SetGV("$a", mrb.AryNew())
	_, err := mrb.Eval(`Dir.open('testdata').each {|x| $a << x }`)
	assert.NilError(t, err)

	a := mrb.GetGV("$a")
	assert.Equal(t, a.Type(), oruby.MrbTTArray)
	assert.Include(t, ".", mrb.Intf(a))
	assert.Include(t, "..", mrb.Intf(a))
	assert.Include(t, "test.txt", mrb.Intf(a))
	assert.Include(t, "zero.txt", mrb.Intf(a))
}

func Test_dirEachChild(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	mrb.SetGV("$a", mrb.AryNew())
	_, err := mrb.Eval(`Dir.open('testdata').each_child {|x| $a << x }`)
	assert.NilError(t, err)

	a := mrb.GetGV("$a")
	assert.Equal(t, a.Type(), oruby.MrbTTArray)
	assert.NotInclude(t, ".", mrb.Intf(a))
	assert.NotInclude(t, "..", mrb.Intf(a))
	assert.Include(t, "test.txt", mrb.Intf(a))
	assert.Include(t, "zero.txt", mrb.Intf(a))
}

func Test_dirCollectChildren(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval("Dir.new('testdata').children")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTArray)
	assert.NotInclude(t, ".", mrb.Intf(ret))
	assert.NotInclude(t, "..", mrb.Intf(ret))
	assert.Include(t, "test.txt", mrb.Intf(ret))
	assert.Include(t, "zero.txt", mrb.Intf(ret))
}

func Test_dirRewind(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	dir, err := mrb.Eval("Dir.open('testdata')")
	assert.NilError(t, err)

	ret := dir.Call("tell")
	assert.NilError(t, mrb.Err())
	assert.Equal(t, ret.Int(), 0)

	r1 := dir.Call("read")
	assert.NilError(t, mrb.Err())

	ret = dir.Call("tell")
	assert.NilError(t, mrb.Err())
	assert.Equal(t, ret.Int(), 1)

	dir.Call("rewind")
	assert.NilError(t, mrb.Err())

	ret = dir.Call("tell")
	assert.NilError(t, mrb.Err())
	assert.Equal(t, ret.Int(), 0)

	r2 := dir.Call("read")
	assert.NilError(t, mrb.Err())
	assert.Equal(t, r2.String(), r1.String())
}

func Test_dirTell(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	dir, err := mrb.Eval("Dir.open('testdata')")
	assert.NilError(t, err)

	ret := dir.Call("tell")
	assert.NilError(t, mrb.Err())
	assert.Equal(t, ret.Int(), 0)

	dir.Call("read")
	assert.NilError(t, mrb.Err())

	ret = dir.Call("tell")
	assert.NilError(t, mrb.Err())
	assert.Equal(t, ret.Int(), 1)

	dir.Call("read")
	assert.NilError(t, mrb.Err())

	ret = dir.Call("tell")
	assert.NilError(t, mrb.Err())
	assert.Equal(t, ret.Int(), 2)
}

func Test_dirSeek(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	dir, err := mrb.Eval("Dir.open('testdata')")
	assert.NilError(t, err)

	dir.Call("read")
	assert.NilError(t, mrb.Err())

	ret := dir.Call("seek", 0)
	assert.NilError(t, mrb.Err())
	assert.Equal(t, ret.Type(), oruby.MrbTTCData)

	ret = dir.Call("tell")
	assert.NilError(t, mrb.Err())
	assert.Equal(t, ret.Int(), 0)
}

func Test_dirSetPos(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	dir, err := mrb.Eval("Dir.open('testdata')")
	assert.NilError(t, err)

	ret := dir.Call("tell")
	assert.NilError(t, mrb.Err())
	assert.Equal(t, ret.Int(), 0)

	dir.Call("read")
	assert.NilError(t, mrb.Err())

	ret = dir.Call("tell")
	assert.NilError(t, mrb.Err())
	assert.Equal(t, ret.Int(), 1)

	dir.Call("pos=", 0)
	assert.NilError(t, mrb.Err())

	ret = dir.Call("tell")
	assert.NilError(t, mrb.Err())
	assert.Equal(t, ret.Int(), 0)

	dir.Call("pos=", 2)
	assert.NilError(t, mrb.Err())

	ret = dir.Call("tell")
	assert.NilError(t, mrb.Err())
	assert.Equal(t, ret.Int(), 2)
}

func Test_dirClose(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval("Dir.open('testdata')")
	assert.NilError(t, err)

	fd := ret.Data().(*os.File)

	ret.Call("close")
	err = fd.Close()
	assert.Error(t, err, "should be already closed")
}

func Test_dirChdir(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval("Dir.pwd")
	assert.NilError(t, err)
	wd := ret.String()

	ret, err = mrb.Eval("Dir.chdir 'testdata'")
	assert.NilError(t, err)

	ret, err = mrb.Eval("Dir.pwd")
	assert.NilError(t, err)
	assert.Expect(t, ret.String() != wd, "Should change directory")

	ret, err = mrb.Eval("Dir.chdir '..'")
	assert.NilError(t, err)

	ret, err = mrb.Eval("Dir.pwd")
	assert.NilError(t, err)
	assert.Equal(t, ret.String(), wd)

	ret, err = mrb.Eval("Dir.chdir('testdata') {|dir| dir }")
	assert.NilError(t, err)

	ret, err = mrb.Eval("Dir.pwd")
	assert.NilError(t, err)
	assert.Equal(t, ret.String(), wd)

	home, err := os.UserHomeDir()
	assert.NilError(t, err)

	ret, err = mrb.Eval("Dir.chdir {|dir| dir }")
	assert.NilError(t, err)
	assert.Equal(t, ret.String(), home)

	ret, err = mrb.Eval("Dir.pwd")
	assert.NilError(t, err)
	assert.Equal(t, ret.String(), wd)
}

func Test_dirGetwd(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	wd, err := os.Getwd()
	assert.NilError(t, err)

	ret, err := mrb.Eval("Dir.getwd")
	assert.NilError(t, err)
	assert.Equal(t, ret.String(), wd)
}

func Test_dirChroot(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	if os.Geteuid() == 0 {
		_, err := mrb.Eval(`Dir.chroot "testdata"`)
		assert.NilError(t, err)

		t.Fatalf("root not allowed")
	}

	_, err := mrb.Eval(`Dir.chroot "testdata"`)
	assert.Error(t, err, "chroot should fail for non-root user")
}

func Test_dirMkdir(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	dir, err := ioutil.TempDir("", "")
	assert.NilError(t, err)
	defer os.RemoveAll(dir)
	mrb.SetGV("$dir", dir)

	ret, err := mrb.Eval(`Dir.exist? "#{$dir}/test"`)
	assert.NilError(t, err)
	assert.False(t, ret.Bool())

	ret, err = mrb.Eval(`Dir.mkdir "#{$dir}/test"`)
	assert.NilError(t, err)
	assert.True(t, ret.Bool())

	ret, err = mrb.Eval(`Dir.exist? "#{$dir}/test"`)
	assert.NilError(t, err)
	assert.True(t, ret.Bool())
}

func Test_dirRmdir(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	dir, err := ioutil.TempDir("", "")
	assert.NilError(t, err)
	defer os.RemoveAll(dir)
	mrb.SetGV("$dir", dir)

	ret, err := mrb.Eval("Dir.exist? $dir")
	assert.NilError(t, err)
	assert.True(t, ret.Bool())

	ret, err = mrb.Eval("Dir.rmdir $dir")
	assert.NilError(t, err)
	assert.Equal(t, ret.Int(), 0)

	ret, err = mrb.Eval("Dir.exist? $dir")
	assert.NilError(t, err)
	assert.False(t, ret.Bool())
}

func Test_dirHome(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	home, err := os.UserHomeDir()
	assert.NilError(t, err)

	ret, err := mrb.Eval("Dir.home")
	assert.NilError(t, err)
	assert.Equal(t, ret.String(), home)
}

func Test_dirExist(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval("Dir.exist? 'testdata'")
	assert.NilError(t, err)
	assert.True(t, ret.Bool())

	ret, err = mrb.Eval("Dir.exist? 'NON_EXISTSNT'")
	assert.NilError(t, err)
	assert.False(t, ret.Bool())
}

func Test_dirIsEmpty(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval("Dir.empty? 'testdata'")
	assert.NilError(t, err)
	assert.False(t, ret.Bool())

	dir, err := ioutil.TempDir("", "")
	assert.NilError(t, err)
	defer os.RemoveAll(dir)
	mrb.SetGV("$dir", dir)

	ret, err = mrb.Eval("Dir.empty? $dir")
	assert.NilError(t, err)
	assert.True(t, ret.Bool())
}

func Test_dirGlob(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	doGlobFlag := func(pattern string, flag int, f ...string) {
		t.Helper()
		mrb.SetGV("$pattern", pattern)
		mrb.SetGV("$flag", flag)
		ret, err := mrb.Eval("Dir.glob($pattern, $flag)")
		assert.NilError(t, err)
		assert.True(t, ret.Type() == oruby.MrbTTArray)
		for _, name := range f {
			assert.Include(t, name, ret.Interface())
		}
	}

	doGlob := func(pattern string, f ...string) {
		t.Helper()
		doGlobFlag(pattern, 0, f...)
	}

	doGlob("testdata/test.tx?", "testdata/test.txt")
	doGlob("testdata/*", "testdata/test.txt", "testdata/zero.txt")
	doGlob("testdata/*.[a-z][a-z][a-z]", "testdata/test.txt", "testdata/zero.txt")
	doGlob("testdata/{test,zero}.txt", "testdata/test.txt", "testdata/zero.txt")
	doGlob("**", "testdata")
	doGlob("**/testdata", "testdata")
	doGlob("**/testdata/**/*.txt",
		"testdata/zero.txt",
		"testdata/test.txt",
		"testdata/glob/globfile.txt",
		"testdata/glob/a/afile.txt")

	doGlobFlag("testdata/*", fnmDotmatch, "testdata/.", "testdata/..", "testdata/test.txt", "testdata/zero.txt")

	os.Chdir("testdata")
	defer os.Chdir("..")

	doGlob("*", "test.txt", "zero.txt")
	doGlob("*", "test.txt", "zero.txt")
	doGlobFlag("*", fnmDotmatch, ".", "..", "test.txt", "zero.txt")
}

func Test_dirAref(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	err := os.Chdir("testdata")
	assert.NilError(t, err)
	defer os.Chdir("..")

	doAref := func(pattern string, f ...string) {
		ret, err := mrb.Eval(pattern)
		assert.NilError(t, err)
		assert.True(t, ret.Type() == oruby.MrbTTArray)
		retSlice := ret.Interface()
		for _, name := range f {
			assert.Include(t, name, retSlice)
		}
	}
	doAref(`Dir["test.tx?"]`, "test.txt")
	doAref(`Dir["test.tx?", "zero.???"]`, "test.txt", "zero.txt")
}
