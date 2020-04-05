package file

import (
	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/assert"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func statTest(t *testing.T, mrb *oruby.MrbState, f string) oruby.Value {
	t.Helper()
	stat, err := os.Stat(f)
	assert.NilError(t, err)

	ret := mrb.Value(stat)
	assert.Equal(t, mrb.ClassName(mrb.ClassOf(ret)), "File::Stat")

	return ret
}

func statTemp(t *testing.T) (*oruby.MrbState, os.FileInfo, func()) {
	t.Helper()
	mrb := oruby.MrbOpen()

	f, err := ioutil.TempFile("", "stt*.tmp")
	assert.NilError(t, err)

	stat, err := f.Stat()
	assert.NilError(t, err)

	ret := mrb.Value(stat)
	assert.Equal(t, mrb.ClassName(mrb.ClassOf(ret)), "File::Stat")

	mrb.SetGV("$tmpf", f)
	mrb.SetGV("$tmp_name", f.Name())

	return mrb, stat, func() {
		_= f.Close()
		_= os.Remove(f.Name())
		mrb.Close()
	}
}

func Test_statInit(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File::Stat.new("testdata/test.txt")`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTData)

	f, err := ioutil.TempFile("", "st*.tmp")
	assert.NilError(t, err)
	defer os.Remove(f.Name())
	mrb.SetGV("$f", f)

	ret, err = mrb.Eval("File::Stat.new($f)")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTData)
}

func Test_statComp(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	f1, err := ioutil.TempFile("", "stt*.tmp")
	assert.NilError(t, err)

	f2, err := ioutil.TempFile("", "stt*.tmp")
	assert.NilError(t, err)

	defer func() {
		_= f1.Close()
		_= f2.Close()
		_= os.Remove(f1.Name())
		_= os.Remove(f1.Name())
	}()

	mrb.SetGV("$f1", f1)
	mrb.SetGV("$f2", f2)

	ret, err := mrb.Eval(`$f1.stat<=>$f2.stat`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)
	assert.Equal(t, ret.Int(), 1)
}

func Test_statAtime(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").atime`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTData)
	_, ok := mrb.Data(ret).(time.Time)
	assert.Equal(t, ok, true)
}

func Test_statBirthtime(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").birthtime`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTData)
	_, ok := mrb.Data(ret).(time.Time)
	assert.Equal(t, ok, true)
}

func Test_statBlksize(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").blksize`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)
	if runtime.GOOS != "windows" {
		assert.Expect(t, ret.Int() > 0, "should have block size")
	}
}

func Test_statIsBlockdev(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").blockdev?`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFalse)

	if runtime.GOOS == "darwin" {
		ret, err := mrb.Eval(`File.stat("/dev/disk0").blockdev?`)
		assert.NilError(t, err)
		assert.Equal(t, ret.Type(), oruby.MrbTTTrue)
	}
}

func Test_statBlocks(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").blocks`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)
	if runtime.GOOS != "windows" {
		assert.Expect(t, ret.Int() > 0, "should occupy some blocks")
	}
}

func Test_statIsChardev(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").chardev?`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFalse)

	if runtime.GOOS != "windows" {
		ret, err := mrb.Eval(`File.stat("/dev/tty").chardev?`)
		assert.NilError(t, err)
		assert.Equal(t, ret.Type(), oruby.MrbTTTrue)
	}
}

func Test_statCtime(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").ctime`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTData)
	_, ok := mrb.Data(ret).(time.Time)
	assert.Equal(t, ok, true)
}

func Test_statDev(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").dev`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)
}

func Test_statDevMajor(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").dev_minor`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)
}

func Test_statDevMinor(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").dev_major`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)
}

func Test_statIsDirectory(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").directory?`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFalse)

	ret, err = mrb.Eval(`File.stat("testdata").directory?`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTTrue)
}

func Test_statIsZero(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").zero?`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFalse)

	ret, err = mrb.Eval(`File.stat("testdata/zero.txt").zero?`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTTrue)
}

func Test_statIsExecutable(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").executable?`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFalse)
}

func Test_statIsExecutableReal(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").executable_real?`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFalse)
}

func Test_statIsFile(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").file?`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTTrue)
}

func Test_statFtype(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ftype := func(path string) string {
		mrb.SetGV("$path", path)
		ret, err := mrb.Eval(`File.lstat($path).ftype`)
		assert.NilError(t, err)
		assert.Equal(t, ret.Type(), oruby.MrbTTString)
		return mrb.String(ret)
	}

	assert.Equal(t, ftype("testdata/test.txt"), "file")
	assert.Equal(t, ftype("testdata"), "directory")
	if runtime.GOOS != "windows" {
		assert.Equal(t, ftype("/dev/tty"), "characterSpecial")

		file := mrb.ClassGet("File")
		fifo := filepath.Join(os.TempDir(), "fifo_test_" + strconv.Itoa(time.Now().Nanosecond()))

		ret := file.Call("mkfifo",  fifo)
		assert.NilError(t, mrb.Err())
		assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)
		assert.Equal(t, ret.Int(), 0)
		assert.Equal(t, ftype(fifo), "fifo")

		_= os.Remove("fifo")

		if runtime.GOOS == "darwin" {
			assert.Equal(t, ftype("/dev/disk0"), "blockSpecial")
		}
		//assert.Equal(t,ftype(""),"socket")
	}

	tmp, err := ioutil.TempFile("", "ft*.tmp")
	assert.NilError(t, err)
	name := tmp.Name()
	_= tmp.Close()
	err = os.Symlink(name, name+"2")
	assert.NilError(t, err)
	assert.Equal(t,ftype(name+"2"),"link")

	_= os.Remove(name+"2")
	_= os.Remove(name)
}

func Test_statGid(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	f, err := ioutil.TempFile("", "st*.tmp")
	assert.NilError(t, err)
	defer os.Remove(f.Name())

	mrb.SetGV("$f", f)
	ret, err := mrb.Eval("$f.stat.gid")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)
	assert.Equal(t, ret.Int(), os.Getegid())
}

func Test_statIsGrpowned(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	f, err := ioutil.TempFile("", "st*.tmp")
	assert.NilError(t, err)
	defer os.Remove(f.Name())

	mrb.SetGV("$f", f)
	ret, err := mrb.Eval("$f.stat.grpowned?")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTTrue)
}

func Test_statIno(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").ino`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)
}

func Test_statInspect(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").inspect`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTString)
	s := ret.String()
	assert.Expect(t, strings.HasPrefix(s,"#<File::Stat dev="), "prefix")
	assert.Expect(t, strings.Contains(s,"ino="), "")
	assert.Expect(t, strings.Contains(s,"mode="), "")
	assert.Expect(t, strings.Contains(s,"nlink="), "")
	assert.Expect(t, strings.Contains(s,"uid="), "")
	assert.Expect(t, strings.Contains(s,"gid="), "")
	assert.Expect(t, strings.Contains(s,"rdev="), "")
	assert.Expect(t, strings.Contains(s,"size="), "")
	assert.Expect(t, strings.Contains(s,"blksize="), "")
	assert.Expect(t, strings.Contains(s,"blocks="), "")
	assert.Expect(t, strings.Contains(s,"atime="), "")
	assert.Expect(t, strings.Contains(s,"mtime="), "")
	assert.Expect(t, strings.Contains(s,"ctime="), "")
	assert.Expect(t, strings.Contains(s,"birthtime="), "")
	assert.Expect(t, strings.HasSuffix(s,">"), "")
}

func Test_statMode(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	f, err := ioutil.TempFile("", "st*.tmp")
	assert.NilError(t, err)
	defer os.Remove(f.Name())

	mrb.SetGV("$f", f)
	ret, err := mrb.Eval("$f.stat.mode")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)

	err = f.Chmod(0777)
	assert.NilError(t, err)

	ret, err = mrb.Eval("$f.stat.mode")
	assert.NilError(t, err)
	assert.Equal(t, ret.Int(), 0777)
}

func Test_statMtime(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").mtime`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTData)
	_, ok := mrb.Data(ret).(time.Time)
	assert.Equal(t, ok, true)

	f, err := ioutil.TempFile("", "st*.tmp")
	assert.NilError(t, err)
	defer os.Remove(f.Name())

	mrb.SetGV("$f", f)
	ret, err = mrb.Eval(`$f.stat.mtime`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTData)
	ct, ok := mrb.Data(ret).(time.Time)
	assert.Equal(t, ok, true)

	stat, _ := f.Stat()
	assert.Equal(t, stat.ModTime().Nanosecond(), ct.Nanosecond())
}

func Test_statNlink(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").nlink`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)
}

func Test_statIsOwned(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	f, err := ioutil.TempFile("", "st*.tmp")
	assert.NilError(t, err)
	defer os.Remove(f.Name())

	mrb.SetGV("$f", f)
	ret, err := mrb.Eval("$f.stat.owned?")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTTrue)
}

func Test_statIsPipe(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").pipe?`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFalse)
}

func Test_statRdev(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").rdev`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)
}

func Test_statIsRdevMajor(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").rdev_major`)
	assert.NilError(t, err)
	if runtime.GOOS == "linux" {
		assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)
	}
}

func Test_statIsRdevMinor(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").rdev_minor`)
	assert.NilError(t, err)
	if runtime.GOOS == "linux" {
		assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)
	}
}

func Test_statIsReadable(t *testing.T) {
	mrb, fstat, closer := statTemp(t)
	defer closer()

	ret := mrb.Call(mrb.Value(fstat), "readable?")
	assert.NilError(t, mrb.Err())
	assert.Equal(t, ret.Type(), oruby.MrbTTTrue)
}

func Test_statIsReadableReal(t *testing.T) {
	mrb, fstat, closer := statTemp(t)
	defer closer()

	ret := mrb.Call(mrb.Value(fstat), "readable_real?")
	assert.NilError(t, mrb.Err())
	assert.Equal(t, ret.Type(), oruby.MrbTTTrue)
}

func Test_statIsSetgid(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").setgid?`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFalse)
}

func Test_statIsSetuid(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").setuid?`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFalse)
}

func Test_statSize(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").size`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)

	s,err := ioutil.ReadFile("testdata/test.txt")
	assert.NilError(t, err)
	assert.Equal(t, ret.Int(), len(s))
}

func Test_statIsSocket(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").socket?`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFalse)
}

func Test_statIsSticky(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").sticky?`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFalse)
}

func Test_statIsSymlink(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	ret, err := mrb.Eval(`File.stat("testdata/test.txt").symlink?`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFalse)
}

func Test_statUid(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	f, err := ioutil.TempFile("", "st*.tmp")
	assert.NilError(t, err)
	defer os.Remove(f.Name())

	mrb.SetGV("$f", f)
	ret, err := mrb.Eval("$f.stat.uid")
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTFixnum)
	assert.Equal(t, ret.Int(), os.Geteuid())
}

func Test_statIsWorldReadable(t *testing.T) {
	mrb, _, closer := statTemp(t)
	defer closer()

	f := mrb.GetGV("$tmp_name").String()
	err := os.Chmod(f, 0700)
	assert.NilError(t, err)

	ret, err := mrb.Eval("$tmpf.stat.world_readable?")
	assert.NilError(t, err)
	assert.Expect(t, ret.IsNil(), "should not be world readable")

	err = os.Chmod(f, 0777)
	assert.NilError(t, err)

	ret, err = mrb.Eval("$tmpf.stat.world_readable?")
	assert.NilError(t, err)
	assert.Expect(t, !ret.IsNil(), "should be world readable")
	assert.Equal(t, ret.Int(), 0777)
}

func Test_statIsWorldWritable(t *testing.T) {
	mrb, _, closer := statTemp(t)
	defer closer()

	err := os.Chmod(mrb.GetGV("$tmp_name").String(), 0700)
	assert.NilError(t, err)

	ret, err := mrb.Eval("$tmpf.stat.world_writable?")
	assert.NilError(t, err)
	assert.Expect(t, ret.IsNil(), "should not be writable")

	err = os.Chmod(mrb.GetGV("$tmp_name").String(), 0777)
	assert.NilError(t, err)

	ret, err = mrb.Eval("$tmpf.stat.world_writable?")
	assert.NilError(t, err)
	assert.Expect(t, !ret.IsNil(), "should be writable")
	assert.Equal(t, ret.Int(), 0777)
}

func Test_statIsWritable(t *testing.T) {
	mrb, fstat, closer := statTemp(t)
	defer closer()

	ret := mrb.Call(mrb.Value(fstat), "writable?")
	assert.NilError(t, mrb.Err())
	assert.Equal(t, ret.Type(), oruby.MrbTTTrue)
}

func Test_statIsWritableReal(t *testing.T) {
	mrb, fstat, closer := statTemp(t)
	defer closer()

	ret := mrb.Call(mrb.Value(fstat), "writable_real?")
	assert.NilError(t, mrb.Err())
	assert.Equal(t, ret.Type(), oruby.MrbTTTrue)
}


