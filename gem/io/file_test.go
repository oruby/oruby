package io

import (
	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/assert"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func Test_fileAbsolutePath(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	file := mrb.ClassGet("File")

	absPath := func(of string) string {
		return file.Call("absolute_path", of).String()
	}

	_=os.Chdir(os.TempDir())
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

	_=os.Chdir(os.TempDir())
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
	ret := file.Call("chown", 0,0, tmp.Name())
	assert.NilError(t, mrb.Err())
	assert.Equal(t, ret.Int(), 1)
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
	assert.Equal(t, expandPath("ruby", "/usr/bin"),  "/usr/bin/ruby")
	//assert.Equal(t, expandPath("../../lib/mygem.rb", "__FILE__"),  "/usr/bin/ruby")
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
	fnmatch := func(s, p string) bool {
		t.Helper()
		ret, err := mrb.FuncallWithBlock(f, mrb.Intern("fnmatch"), s, p)
		assert.NilError(t, err)
		assert.Expect(t, ret.Type() == oruby.MrbTTTrue || ret.Type() == oruby.MrbTTFalse, "")
		return ret.Bool()
	}
	assert.Equal(t, fnmatch("cat","cat")  ,true)
	assert.Equal(t, fnmatch("cat","category"),false)
	assert.Equal(t, fnmatch("c{at,ub}s", "cats")  ,false)
	//assert.Equal(t, fnmatch("c{at,ub}s","cats", File::FNM_EXTGLOB) ,true)
	assert.Equal(t, fnmatch("c?t","cat") ,true)
	assert.Equal(t, fnmatch("c??t","cat") ,false)
	assert.Equal(t, fnmatch("c*", "cats"),true)
	assert.Equal(t, fnmatch("c*t","c/a/b/t"),true)
	assert.Equal(t, fnmatch("ca[a-z]","cat") ,true)
	assert.Equal(t, fnmatch("ca[^t]","cat") ,false)
	assert.Equal(t, fnmatch("cat", "CAT"),false)
	//assert.Equal(t, fnmatch("cat", "CAT", File::FNM_CASEFOLD) ,true)
	//assert.Equal(t, fnmatch("cat", "CAT", File::FNM_SYSCASE)  ,true or false)
	//assert.Equal(t, fnmatch("?","/", File::FNM_PATHNAME)  ,false)
	//assert.Equal(t, fnmatch("*", "/", File::FNM_PATHNAME)  ,false)
	//assert.Equal(t, fnmatch("[/]","/", File::FNM_PATHNAME)  ,false)
	assert.Equal(t, fnmatch("\\?", "?")  ,true)
	assert.Equal(t, fnmatch("\\a", "a")  ,true)
	//assert.Equal(t, fnmatch("\a", a", File::FNM_NOESCAPE)  ,true)
	assert.Equal(t, fnmatch("[\\?]","?")  ,true)
	assert.Equal(t, fnmatch("*", ".profile") ,false)
	//assert.Equal(t, fnmatch("*",".profile", File::FNM_DOTMATCH)  ,true)
	assert.Equal(t, fnmatch(".*",  ".profile") ,true)

	rbfiles := "**/*.rb"
	assert.Equal(t, fnmatch(rbfiles, "main.rb")  ,false)
	assert.Equal(t, fnmatch(rbfiles, "./main.rb"),false)
	assert.Equal(t, fnmatch(rbfiles, "lib/song.rb") ,true)
	assert.Equal(t, fnmatch("**.rb", "main.rb")  ,true)
	assert.Equal(t, fnmatch("**.rb", "./main.rb"),false)
	assert.Equal(t, fnmatch("**.rb", "lib/song.rb") ,true)
	assert.Equal(t, fnmatch("*",  "dave/.profile") ,true)

	//pattern := "*/*"
	//assert.Equal(t, fnmatch(pattern, "dave/.profile", File::FNM_PATHNAME)  ,false)
	//assert.Equal(t, fnmatch(pattern, "dave/.profile", File::FNM_PATHNAME | File::FNM_DOTMATCH) ,true)

	//pattern = "**/foo"
	//assert.Equal(t, fnmatch(pattern, "a/b/c/foo", File::FNM_PATHNAME)  ,true)
	//assert.Equal(t, fnmatch(pattern, "/a/b/c/foo", File::FNM_PATHNAME) ,true)
	//assert.Equal(t, fnmatch(pattern, "c:/a/b/c/foo", File::FNM_PATHNAME)  ,true)
	//assert.Equal(t, fnmatch(pattern, "a/.b/c/foo", File::FNM_PATHNAME) ,false)
	//assert.Equal(t, fnmatch(pattern, "a/.b/c/foo", File::FNM_PATHNAME | File::FNM_DOTMATCH) ,true)
}

//func Test_fileForeach(t *testing.T)


func Test_fileIdentical(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	f := mrb.ClassGet("File")
	identical := func(s, s2 string) bool {
		t.Helper()
		ret, err := mrb.FuncallWithBlock(f, mrb.Intern("identical"), s, s2)
		assert.NilError(t, err)
		assert.Expect(t, ret.Type() == oruby.MrbTTTrue || ret.Type() == oruby.MrbTTFalse, "")
		return ret.Bool()
	}

	assert.Equal(t, identical("a", "a"),true)
	assert.Equal(t, identical("a", "./a"), true)
	//File.link("a", "b")
	assert.Equal(t, identical("a", "b"), true)
	//File.symlink("a", "c")
	assert.Equal(t, identical("a", "c"), true)
	//open("d", "w") {}
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
	_= tmp.Close()
	ret, err := mrb.FuncallWithBlock(file ,mrb.Intern("link"), 0700, name, name+"2")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)
	assert.Equal(t, ret.Int(), 0)
	_= os.Remove(name)
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

}

func Test_fileRealpath(t *testing.T) {

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
	ret, err := mrb.FuncallWithBlock(file ,mrb.Intern("symlink"), 0700, name, name+"3")
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

}

func Test_fileInit(t *testing.T) {

}

func Test_fileFChmod(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	tmp, err := ioutil.TempFile("", "ft*.tmp")
	assert.NilError(t, err)
	f := mrb.Value(tmp)
	ret, err := mrb.FuncallWithBlock(f, mrb.Intern("chmod"), 0700)
	assert.NilError(t, err)

	assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)
	assert.Equal(t, ret.Int(), 0)
	stat, _ := tmp.Stat()
	assert.Equal(t, stat.Mode().Perm(), os.FileMode(0700))
}

func Test_fileFChown(t *testing.T) {
}

func Test_fileFlock(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	tmp, err := ioutil.TempFile("", "ft*.tmp")
	assert.NilError(t, err)
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


