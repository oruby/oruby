package io

import (
	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/assert"
	"io/ioutil"
	"os"
	"path/filepath"
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

}


func Test_fileExist(t *testing.T) {

}

func Test_fileExpandPath(t *testing.T) {

}

func Test_fileExtname(t *testing.T) {

}


func Test_fileMatch(t *testing.T) {

}

//func Test_fileForeach(t *testing.T) {


func Test_fileIdentical(t *testing.T) {

}

func Test_fileJoin(t *testing.T) {

}

//lchmod
//lchown
func Test_fileLink(t *testing.T) {

}

func Test_fileLStat(t *testing.T) {

}

//lutime
//mkfifo
func Test_filePath(t *testing.T) {

}

func Test_fileReadlink(t *testing.T) {

}

func Test_fileRealdirpath(t *testing.T) {

}

func Test_fileRealpath(t *testing.T) {

}

func Test_fileRename(t *testing.T) {

}

func Test_fileSplit(t *testing.T) {

}

func Test_fileStat(t *testing.T) {

}

func Test_fileSymlink(t *testing.T) {

}

func Test_fileTruncate(t *testing.T) {

}

func Test_fileUmask(t *testing.T) {

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

}

func Test_fileToPath(t *testing.T) {

}

func Test_fileFTruncate(t *testing.T) {

}


