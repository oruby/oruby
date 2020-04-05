package file

import (
	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/assert"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

func Test_fileAbsolutePath(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	file := mrb.ClassGet("File")

	absPath := func(of string) string {
		return file.Call("absolute_path", of).String()
	}

	cwd,_ := os.Getwd()
	_=os.Chdir(os.TempDir())
	defer os.Chdir(cwd)

	target, _ := filepath.Abs("~oracle/bin")
	assert.Equal(t, absPath("~oracle/bin"), target)
}

func Test_fileIsAbsolutePath(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	file := mrb.ClassGet("File")

	isAbsPath := func(of string) bool {
		return file.Call("absolute_path?", of).Bool()
	}

	cwd,_ := os.Getwd()
	_=os.Chdir(os.TempDir())
	defer os.Chdir(cwd)

	assert.Equal(t, isAbsPath("~oracle/bin"), false)
	assert.Equal(t, isAbsPath(os.TempDir()), true)
}

func Test_fileBasename(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	file := mrb.ClassGet("File")
	baseName := func(of string) string {
		return file.Call("basename", of).String()
	}
	baseName2 := func(of, ext string) string {
		return file.Call("basename", of, ext).String()
	}

	assert.Equal(t, baseName("/home/gumby/work/ruby.rb"), "ruby.rb")
	assert.Equal(t, baseName2("/home/gumby/work/ruby.rb", ".rb"), "ruby")
	assert.Equal(t, baseName2("/home/gumby/work/ruby.rb", ".*"), "ruby")
}

func Test_fileChmod(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	file := mrb.ClassGet("File")

	tmp, err := ioutil.TempFile("", "ft*.tmp")
	assert.NilError(t, err)
	defer os.Remove(tmp.Name())

	ret := file.Call("chmod", 0700, tmp.Name())
	assert.NilError(t, mrb.Err())
	assert.Equal(t, ret.Int(), 1)

	stat, _ := tmp.Stat()
	assert.Equal(t, stat.Mode().Perm(), os.FileMode(0700))
}

func Test_fileChown(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	file := mrb.ClassGet("File")

	tmp, err := ioutil.TempFile("", "ft*.tmp")
	assert.NilError(t, err)
	defer os.Remove(tmp.Name())

	ret := file.Call("chown", -1,-1, tmp.Name())
	assert.NilError(t, mrb.Err())
	assert.Equal(t, ret.Int(), 1)

	ret = file.Call("chown", 0,0, tmp.Name())
	assert.Equal(t, ret.Type(), oruby.MrbTTException)
	assert.Expect(t, mrb.Err() != nil, "not permited")
}

func Test_fileLchown(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	file := mrb.ClassGet("File")

	tmp, err := ioutil.TempFile("", "ft*.tmp")
	assert.NilError(t, err)
	name := tmp.Name()
	_= tmp.Close()

	err = os.Symlink(name, name+"2")
	assert.NilError(t, err)

	ret := file.Call("lchown", -1,-1, name+"2")
	assert.NilError(t, mrb.Err())
	assert.Equal(t, ret.Int(), 1)

	ret = file.Call("lchown", 0,0, name+"2")
	assert.Equal(t, ret.Type(), oruby.MrbTTException)
	assert.Expect(t, mrb.Err() != nil, "not permited")

	_=os.Remove(name)
	_=os.Remove(name+"2")
}

func Test_fileUnlink(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	file := mrb.ClassGet("File")

	tmp, err := ioutil.TempFile("", "ft*.tmp")
	name := tmp.Name()
	assert.NilError(t, err)
	_= tmp.Close()

	ret := file.Call("unlink", name)
	assert.NilError(t, mrb.Err())
	assert.Equal(t, ret.Int(), 1)
	_, err = os.Open(name)
	assert.Error(t, err, "file hould be deleted")
}

func Test_fileDirname(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	f := mrb.ClassGet("File")
	dirname := func(s string) string {
		t.Helper()
		ret, err := mrb.FuncallWithBlock(f, mrb.Intern("dirname"), s)
		assert.NilError(t, err)
		assert.Equal(t, ret.Type(), oruby.MrbTTString)
		return ret.String()
	}

	assert.Equal(t, dirname("/home/gumby/work/ruby.rb"), "/home/gumby/work")
}


func Test_fileExist(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	f := mrb.ClassGet("File")

	tmp, err := ioutil.TempFile("", "ft*.tmp")
	assert.NilError(t, err)
	name := tmp.Name()

	ret, err := mrb.FuncallWithBlock(f, mrb.Intern("exist?"), name)
	assert.NilError(t, err)
	assert.Equal(t, ret.IsNil(),false)
	assert.Expect(t, ret.Type() == oruby.MrbTTTrue || ret.Type() == oruby.MrbTTFalse, "")
	assert.Equal(t, ret.Bool(),true)

	_=tmp.Close()
	_=os.Remove(name)

	ret, err = mrb.FuncallWithBlock(f, mrb.Intern("exist?"), name)
	assert.NilError(t, err)
	assert.Equal(t, ret.IsNil(),false)
	assert.Expect(t, ret.Type() == oruby.MrbTTTrue || ret.Type() == oruby.MrbTTFalse, "")
	assert.Equal(t, ret.Bool(),false)
}

func Test_fileExpandPath(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	f := mrb.ClassGet("File")
	expandPath := func(s, d string) string {
		t.Helper()
		ret, err := mrb.FuncallWithBlock(f, mrb.Intern("expand_path"), s, d)
		assert.NilError(t, err)
		assert.Equal(t, ret.Type(), oruby.MrbTTString)
		return ret.String()
	}

	home,err := os.UserHomeDir()
	assert.NilError(t, err)

	assert.Equal(t, expandPath("~oracle/bin", ""),  home+"/oracle/bin")
	assert.Equal(t, expandPath(":~oracle/bin", ""),  ":"+home+"/oracle/bin")
	assert.Equal(t, expandPath("ruby", "/usr/bin"),  "/usr/bin/ruby")

	//file, err := mrb.Eval("__FILE__")
	//assert.NilError(t, err)
	//assert.Equal(t, expandPath("../../lib/mygem.rb", file.String()),  "/usr/bin/ruby")
}

func Test_fileExtname(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	f := mrb.ClassGet("File")
	extName := func(s string) string {
		t.Helper()
		ret, err := mrb.FuncallWithBlock(f, mrb.Intern("extname"), s)
		assert.NilError(t, err)
		assert.Equal(t, ret.Type(), oruby.MrbTTString)
		return ret.String()
	}

	assert.Equal(t, extName("test.rb"), ".rb")
	assert.Equal(t, extName("a/b/d/test.rb"), ".rb")
	assert.Equal(t, extName(".a/b/d/test.rb"), ".rb")
	if runtime.GOOS == "windows" {
		assert.Equal(t, extName("foo."), "")
	} else {
		assert.Equal(t, extName("foo."), ".")
	}
	assert.Equal(t, extName("test"), "")
	assert.Equal(t, extName(".profile"), "")
	assert.Equal(t, extName(".profile.sh"), ".sh")
}


func Test_fileMatch(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	f := mrb.ClassGet("File")
	fnmatch := func(s, p string, flags ...int) bool {
		t.Helper()
		flgs := 0
		if len(flags) > 0 {
			flgs = flags[0]
		}

		ret, err := mrb.FuncallWithBlock(f, mrb.Intern("fnmatch"), s, p, flgs)
		assert.NilError(t, err)
		assert.Expect(t, ret.Type() == oruby.MrbTTTrue || ret.Type() == oruby.MrbTTFalse, "")
		return ret.Bool()
	}
	assert.Equal(t, fnmatch("cat","cat")  ,true)
	assert.Equal(t, fnmatch("cat","category"),false)
	assert.Equal(t, fnmatch("c{at,ub}s", "cats")  ,false)
	assert.Equal(t, fnmatch("c{at,ub}s","cats", fnmExtglob) ,true)
	assert.Equal(t, fnmatch("c?t","cat") ,true)
	assert.Equal(t, fnmatch("c??t","cat") ,false)
	assert.Equal(t, fnmatch("c*", "cats"),true)
	assert.Equal(t, fnmatch("c*t","c/a/b/t"),true)
	assert.Equal(t, fnmatch("ca[a-z]","cat") ,true)
	assert.Equal(t, fnmatch("ca[^t]","cat") ,false)
	assert.Equal(t, fnmatch("cat", "CAT"),false)
	assert.Equal(t, fnmatch("cat", "CAT", fnmCasefold) ,true)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		assert.Equal(t, fnmatch("cat", "CAT", fnmSyscase), true)
	} else {
		assert.Equal(t, fnmatch("cat", "CAT", fnmSyscase), false)
	}
	assert.Equal(t, fnmatch("?","/", fnmPathname)  ,false)
	assert.Equal(t, fnmatch("*", "/", fnmPathname)  ,false)
	assert.Equal(t, fnmatch("[/]","/", fnmPathname)  ,false)

	assert.Equal(t, fnmatch("\\?", "?")  ,true)
	assert.Equal(t, fnmatch("\\a", "a")  ,true)
	assert.Equal(t, fnmatch("\\a", "\\a", fnmNoescape)  ,true)
	assert.Equal(t, fnmatch("[\\?]","?")  ,true)

	assert.Equal(t, fnmatch("*", ".profile") ,false)
	assert.Equal(t, fnmatch("*",".profile", fnmDotmatch)  ,true)
	assert.Equal(t, fnmatch(".*",  ".profile") ,true)

	rbfiles := "**/*.rb"
	assert.Equal(t, fnmatch(rbfiles, "main.rb")  ,false)
	assert.Equal(t, fnmatch(rbfiles, "./main.rb"),false)
	assert.Equal(t, fnmatch(rbfiles, "lib/song.rb") ,true)
	assert.Equal(t, fnmatch("**.rb", "main.rb")  ,true)
	//TODO: this fails - I have no idea wath is the logic behing this
	assert.Equal(t, fnmatch("**.rb", "./main.rb"),false)
	assert.Equal(t, fnmatch("**.rb", "lib/song.rb") ,true)
	assert.Equal(t, fnmatch("*",  "dave/.profile") ,true)

	pattern := "*/*"
	assert.Equal(t, fnmatch(pattern, "dave/.profile", fnmPathname)  ,false)
	assert.Equal(t, fnmatch(pattern, "dave/.profile", fnmPathname|fnmDotmatch) ,true)

	pattern = "**/foo"
	assert.Equal(t, fnmatch(pattern, "foo", fnmPathname)  ,true)
	assert.Equal(t, fnmatch(pattern, "a/b/c/foo", fnmPathname)  ,true)
	assert.Equal(t, fnmatch(pattern, "/a/b/c/foo", fnmPathname) ,true)
	assert.Equal(t, fnmatch(pattern, "c:/a/b/c/foo", fnmPathname)  ,true)
	assert.Equal(t, fnmatch(pattern, "a/.b/c/foo", fnmPathname) ,false)
	assert.Equal(t, fnmatch(pattern, "a/.b/c/foo", fnmPathname|fnmDotmatch) ,true)
}

func Test_fileIdentical(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	f := mrb.ClassGet("File")
	identical := func(s, s2 string) bool {
		t.Helper()
		ret := f.Call("identical?", s, s2)
		assert.NilError(t, mrb.Err())
		assert.Expect(t, ret.Type() == oruby.MrbTTTrue || ret.Type() == oruby.MrbTTFalse, "")
		return ret.Bool()
	}

	dir, err := ioutil.TempDir("", "fileTestt")
	assert.NilError(t, err)
	defer os.RemoveAll(dir)

	cwd,_ := os.Getwd()
	err = os.Chdir(dir)
	defer os.Chdir(cwd)
	assert.NilError(t, err)

	err = ioutil.WriteFile("a", []byte("temp content"), 0666)
	assert.NilError(t, err)

	assert.Equal(t, identical("a", "a"),true)
	assert.Equal(t, identical("a", "./a"), true)

	err = os.Symlink("a", "b")
	assert.NilError(t, err)
	assert.Equal(t, identical("a", "b"), true)

	err = os.Symlink("a", "c")
	assert.NilError(t, err)
	assert.Equal(t, identical("a", "c"), true)

	err = ioutil.WriteFile("d", []byte("temp 2 content"), 0666)
	assert.NilError(t, err)
	assert.Equal(t, identical("a", "d"), false)
}

func Test_fileJoin(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	f := mrb.ClassGet("File")
	join := func(ss ...interface{}) string {
		t.Helper()
		ret, err := mrb.FuncallWithBlock(f, mrb.Intern("join"), ss...)
		assert.NilError(t, err)
		assert.Equal(t, ret.Type(), oruby.MrbTTString)
		return ret.String()
	}

	assert.Equal(t, join("usr", "mail", "gumby"), "usr/mail/gumby")
}

//lchmod
//lchown
func Test_fileLink(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	file := mrb.ClassGet("File")

	tmp, err := ioutil.TempFile("", "ft*.tmp")
	assert.NilError(t, err)
	name := tmp.Name()
	defer tmp.Close()
	defer os.Remove(name)

	ret, err := mrb.FuncallWithBlock(file ,mrb.Intern("link"), name, name+"2")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)
	assert.Equal(t, ret.Int(), 0)

	_= os.Remove(name+"2")
}

func Test_fileLStat(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	f := mrb.ClassGet("File")

	tmp, err := ioutil.TempFile("", "ft*.tmp")
	assert.NilError(t, err)
	name := tmp.Name()
	_= tmp.Close()

	err = os.Symlink(name, name+"2")
	assert.NilError(t, err)

	ret := f.Call("lstat", name+"2")
	assert.Equal(t, mrb.Data(ret).(os.FileInfo).Name(), filepath.Base(name+"2"))

	_= os.Remove(name)
	_= os.Remove(name+"2")
}

//lutime
//mkfifo
func Test_filePath(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	file := mrb.ClassGet("File")

	ret := file.Call("path", "/dev/null")
	assert.Equal(t, ret.Type(), oruby.MrbTTString)
	assert.Equal(t, ret.String(), "/dev/null")
   //File.path(Pathname.new("/tmp")),  "/tmp"
}

func Test_fileReadlink(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	file := mrb.ClassGet("File")

	tmp, err := ioutil.TempFile("", "ft*.tmp")
	assert.NilError(t, err)
	name := tmp.Name()
	_= tmp.Close()

	err = os.Symlink(name, name+"2")
	assert.NilError(t, err)

	ret := file.Call("readlink", name+"2")
	assert.Equal(t, ret.Type(), oruby.MrbTTString)
	assert.Equal(t, ret.String(), name)

	_= os.Remove(name+"2")
	_= os.Remove(name)
}

func Test_fileRealdirpath(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	file := mrb.ClassGet("File")

	abs, _ := filepath.Abs("testdata")

	ret := file.Call("realdirpath", "testdata/test.txt")
	assert.Equal(t, ret.Type(), oruby.MrbTTString)
	assert.Equal(t, mrb.String(ret), filepath.Join(abs, "test.txt"))

	ret = file.Call("realdirpath", "testdata/NON_EXISTSNT")
	assert.Equal(t, ret.Type(), oruby.MrbTTString)
	assert.Equal(t, mrb.String(ret), filepath.Join(abs, "NON_EXISTSNT"))

	ret = file.Call("realpath", "NON_EXISTSNT")
	assert.Equal(t, ret.Type(), oruby.MrbTTException)
}

func Test_fileRealpath(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	file := mrb.ClassGet("File")

	f, err := os.Open("testdata/test.txt")
	assert.NilError(t, err)
	abs, _ := filepath.Abs(f.Name())

	ret := file.Call("realpath", "testdata/test.txt")
	assert.Equal(t, ret.Type(), oruby.MrbTTString)
	assert.Equal(t, mrb.String(ret), abs)

	ret = file.Call("realpath", "testdata/NON_EXISTSNT")
	assert.Equal(t, ret.Type(), oruby.MrbTTException)

	ret = file.Call("realpath", "NON_EXISTSNT")
	assert.Equal(t, ret.Type(), oruby.MrbTTException)
}

func Test_fileRename(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	file := mrb.ClassGet("File")

	tmp, err := ioutil.TempFile("", "ft*.tmp")
	assert.NilError(t, err)
	name := tmp.Name()
	_= tmp.Close()

	ret, err := mrb.FuncallWithBlock(file ,mrb.Intern("rename"), name, name+"2")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)
	assert.Equal(t, ret.Int(), 0)
	_= os.Remove(name+"2")
}

func Test_fileSplit(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	f := mrb.ClassGet("File")

	split := func(s string) []string {
		t.Helper()
		ret, err := mrb.FuncallWithBlock(f, mrb.Intern("split"), s)
		assert.NilError(t, err)
		assert.Equal(t, ret.Type(), oruby.MrbTTArray)
		return []string{mrb.AryEntry(ret, 0).String(), mrb.AryEntry(ret, 1).String()}
	}

	assert.Equal(t, split("/home/gumby/.profile"), []string{"/home/gumby", ".profile"})
}

func Test_fileStat(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	f := mrb.ClassGet("File")

	tmp, err := ioutil.TempFile("", "ft*.tmp")
	assert.NilError(t, err)
	name := tmp.Name()

	ret := f.Call("stat", name)
	assert.Equal(t, mrb.Data(ret).(os.FileInfo).Name(), filepath.Base(name))

	_= tmp.Close()

	err = os.Symlink(name, name+"2")
	assert.NilError(t, err)

	_= f.Call("lstat", name+"2")
	assert.Equal(t, mrb.Data(ret).(os.FileInfo).Name(), filepath.Base(name))

	_= os.Remove(name)
	_= os.Remove(name+"2")
}

func Test_fileSymlink(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	file := mrb.ClassGet("File")

	tmp, err := ioutil.TempFile("", "ft*.tmp")
	assert.NilError(t, err)
	name := tmp.Name()
	_= tmp.Close()

	_= os.Remove(name+"3")
	ret, err := mrb.FuncallWithBlock(file ,mrb.Intern("symlink"), name, name+"3")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)
	assert.Equal(t, ret.Int(), 0)

	_= os.Remove(name)
	_= os.Remove(name+"3")
}

func Test_fileTruncate(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	file := mrb.ClassGet("File")

	tmp, err := ioutil.TempFile("", "ft*.tmp")
	assert.NilError(t, err)
	defer os.Remove(tmp.Name())

	_, err = tmp.WriteString("TestTestTest")
	assert.NilError(t, err)
	name := tmp.Name()
	_= tmp.Close()

	_, err = mrb.FuncallWithBlock(file, mrb.Intern("truncate"), name, 4)
	assert.NilError(t, err)

	b, err := ioutil.ReadFile(name)
	assert.NilError(t, err)
	assert.Equal(t, string(b), "Test")
}

func Test_fileMkfifo(t *testing.T) {
	if runtime.GOOS == "windows" {
		return
	}

	mrb := oruby.MrbOpen()
	defer mrb.Close()
	file := mrb.ClassGet("File")
	fifo := filepath.Join(os.TempDir(), "fifo_test_" + strconv.Itoa(time.Now().Nanosecond()))

	ret := file.Call("mkfifo",  fifo)
	assert.NilError(t, mrb.Err())
	assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)
	assert.Equal(t, ret.Int(), 0)

	ret = file.Call("pipe?", fifo)
	assert.NilError(t, mrb.Err())
	assert.Equal(t, ret.Type(), oruby.MrbTTTrue)

	err := os.Remove(fifo)
	assert.NilError(t,err)
}

func Test_fileUmask(t *testing.T) {
	if runtime.GOOS == "windows" {
		return
	}

	mrb := oruby.MrbOpen()
	defer mrb.Close()
	file := mrb.ClassGet("File")

	ret, err := mrb.FuncallWithBlock(file, mrb.Intern("umask"))
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)
	oldUmask := ret.Int()

	ret, err = mrb.FuncallWithBlock(file, mrb.Intern("umask"), 0111)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)


	ret, err = mrb.FuncallWithBlock(file, mrb.Intern("umask"), oldUmask)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)
	assert.Equal(t, ret.Int(), 0111)
}

func Test_fileOpen(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.open("testdata/test.txt") {|f| f.readlines }`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTArray)
	assert.Equal(t, mrb.AryEntry(ret, 0).String(), "line 1\n")
	assert.Equal(t, mrb.AryEntry(ret, 1).String(), "line 2")
}

func Test_fileInit(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	fileNew := func(s, m string) *os.File {
		t.Helper()
		mrb.SetGV("$fname", s)
		mrb.SetGV("$mode", m)
		f, err := mrb.Eval(`$f = File.new($fname, $mode)`)
		assert.NilError(t, err)
		file, ok := mrb.Data(f).(*os.File)
		assert.Equal(t, ok, true)
		assert.Equal(t, file.Name(), s)
		return file
	}

	file := fileNew("testdata/test.txt", "")
	_, err := mrb.Eval(`$f.close`)
	assert.NilError(t, err)
	assert.Error(t, file.Close(), "should be already closed")

	_= fileNew("testdata/test.txt", "r+")
	assert.NilError(t, err)

	_, err = mrb.Eval(`File.new($fname, $mode, 0777)`)
	assert.NilError(t, err)
}

func Test_fileFChmod(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	tmp, err := ioutil.TempFile("", "ft*.tmp")
	assert.NilError(t, err)
	defer os.Remove(tmp.Name())

	f := mrb.Value(tmp)
	ret, err := mrb.FuncallWithBlock(f, mrb.Intern("chmod"), 0700)
	assert.NilError(t, err)

	assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)
	assert.Equal(t, ret.Int(), 0)
	stat, _ := tmp.Stat()
	assert.Equal(t, stat.Mode().Perm(), os.FileMode(0700))
}

func Test_fileFChown(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	tmp, err := ioutil.TempFile("", "ft*.tmp")
	assert.NilError(t, err)
	defer os.Remove(tmp.Name())
	f := mrb.Value(tmp)

	ret := mrb.Call(f, "chown", -1,-1, tmp.Name())
	assert.NilError(t, mrb.Err())
	assert.Equal(t, ret.Int(), 0)

	ret = mrb.Call(f, "chown", 0,0, tmp.Name())
	assert.Equal(t, ret.Type(), oruby.MrbTTException)
	assert.Expect(t, mrb.Err() != nil, "not permited")
}

func Test_fileFlock(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	tmp, err := ioutil.TempFile("", "ft*.tmp")
	assert.NilError(t, err)
	defer os.Remove(tmp.Name())
	f := mrb.Value(tmp)

	if runtime.GOOS == "windows" {
		_, err := mrb.FuncallWithBlock(f, mrb.Intern("flock"), 0)
		assert.Error(t, err, "should be not implemented on Windows")
		return
	}

	_, err = mrb.FuncallWithBlock(f, mrb.Intern("flock"), mrb.Class("File").ConstGet("LOCK_EX"))
	assert.NilError(t, err)
}

func Test_fileToPath(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	tmp, err := ioutil.TempFile("", "ft*.tmp")
	assert.NilError(t, err)
	defer os.Remove(tmp.Name())

	f := mrb.Value(tmp)
	ret, err := mrb.FuncallWithBlock(f, mrb.Intern("to_path"))
	assert.NilError(t, err)
	assert.Equal(t, ret.String(), tmp.Name())
	_= tmp.Close()
}

func Test_fileFTruncate(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	tmp, err := ioutil.TempFile("", "ft*.tmp")
	assert.NilError(t, err)
	defer os.Remove(tmp.Name())

	_, err = tmp.WriteString("TestTestTest")
	assert.NilError(t, err)
	name := tmp.Name()

	f := mrb.Value(tmp)
	_, err = mrb.FuncallWithBlock(f, mrb.Intern("truncate"), 4)
	assert.NilError(t, err)
	_= tmp.Close()

	b, err := ioutil.ReadFile(name)
	assert.NilError(t, err)
	assert.Equal(t, string(b), "Test")
}


