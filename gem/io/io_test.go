package io

import (
	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/assert"
	_ "github.com/oruby/oruby/gem/process"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func Test_raiseEOF(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	v := RaiseEOF(mrb)
	assert.Expect(t, mrb.ObjIsKindOf(v, mrb.EExceptionClass()), "exception excepted")
	assert.Expect(t, mrb.Exc() != nil, "Should be raised")
	assert.Expect(t, *mrb.Exc() == mrb.RObject(v), "Should be raised")
	assert.Expect(t, *mrb.Exc() == mrb.RObject(v), "Should be raised")
}

func Test_raiseIOError(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	v := RaiseIOError(mrb, "IO error")
	assert.Expect(t, mrb.ObjIsKindOf(v, mrb.EExceptionClass()), "Should be exception")
	assert.Expect(t, mrb.Exc() != nil, "Should be raised")
	assert.Expect(t, *mrb.Exc() == mrb.RObject(v), "Should be raised")
}

func Test_getStream(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	f, err := getStream(mrb, os.O_RDONLY, mrb.Value(os.Stdout))
	assert.NilError(t, err)
	assert.Equal(t, f, os.Stdout)

	tmpName := filepath.Join(os.TempDir(), "test.tmp")
	_, err = getStream(mrb, os.O_WRONLY | os.O_CREATE | os.O_TRUNC, mrb.StrNew(tmpName))
	assert.Error(t, err, "string file name is not supported on IO stream without 'file' gem")

	_, err = getStream(mrb, os.O_WRONLY, oruby.Int(12345))
	assert.Error(t, err, "should be non-stream error")
}

func Test_ioCopyStream(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	f, err := os.Open("testdata/test.txt")
	defer f.Close()
	assert.NilError(t, err)
	mrb.SetGV("$source", mrb.Value(f))

	tmp2Name := filepath.Join(os.TempDir(), "test2.tmp")
	f2,_ := os.OpenFile(tmp2Name, os.O_CREATE|os.O_WRONLY, os.ModePerm)
	mrb.SetGV("$dest2", f2)

	_,err= f.Seek(0, io.SeekStart)
	assert.NilError(t, err)

	v, err := mrb.Eval(`IO.copy_stream $source, $dest2`)
	assert.NilError(t, err)
	assert.Expect(t, v.Value().IsFixnum(), "shoud return length")

	stat, err := f2.Stat()
	assert.NilError(t, err)
	assert.Expect(t, stat.Size() > 0, "shoud have length")
	assert.Equal(t, stat.Size(), v.Value().Int64())
}

func Test_openIO(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	file, err := os.Open("testdata/test.txt")
	assert.NilError(t, err)

	f2, err := openIO(mrb, mrb.DataValue(file), mrb.NilValue(), mrb.NilValue())
	assert.NilError(t, err)
	if _, ok := f2.(*os.File); !ok {
		t.Error("File expected")
	}
}

func Test_ioInit(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	f, err := os.Open("testdata/test.txt")
	defer f.Close()
	assert.NilError(t, err)
	mrb.SetGV("$pid", mrb.Value(f))

	v, err := mrb.Eval(`$obj = IO.new($pid)`)
	assert.NilError(t, err)
	assert.Expect(t, v.Value().IsData(), "IO object expected")
}

func Test_ioOpen(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	f, err := os.Open("testdata/test.txt")
	defer f.Close()
	assert.NilError(t, err)
	mrb.SetGV("$pid", mrb.Value(f))

	v, err := mrb.Eval(`$pid = IO.open($pid) {|o| o.readlines }`)
	assert.NilError(t, err)
	println(v.String())
	assert.Expect(t, v.Value().IsArray(), "array of strings expected, got %v", mrb.TypeName(v))
}

func Test_ioPipe(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_,err := mrb.Eval(`$r, $w = IO.pipe`)
	assert.NilError(t, err)

	w, ok := mrb.Data(mrb.GetGV("$w")).(io.WriteCloser)
	assert.Expect(t, ok, "io.WriteCloser expected")
	go func(){
		_, err := io.WriteString(w, "test")
		assert.NilError(t, err)
		_=w.Close()
	}()

	ret, err := mrb.Eval(`$r.read`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTString)
	assert.Equal(t, ret.String(), "test")
}

func Test_ioSReadlines(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	f, err := os.Open("testdata/test.txt")
	defer f.Close()
	assert.NilError(t, err)
	mrb.SetGV("$pid", mrb.Value(f))

	mrb.SetGV("$/", "\n")

	ret, err := mrb.Eval(`IO.readlines($pid)`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Interface(), []interface{}{"line 1\n","line 2"})

	_,err= f.Seek(0, io.SeekStart)
	assert.NilError(t, err)

	ret, err = mrb.Eval(`IO.readlines($pid, chomp: true)`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Interface(), []interface{}{"line 1","line 2"})

	mrb.SetGV("$s", strings.NewReader("line 1\nline 2"))
	ret, err = mrb.Eval(`IO.readlines($s, "ne", 2, chomp: true)`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Interface(), []interface{}{"li",""," 1","\nl","in","e ","2"})
}

func Test_ioForeach(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	f, err := os.Open("testdata/test.txt")
	defer f.Close()
	assert.NilError(t, err)
	mrb.SetGV("$pid", mrb.Value(f))

	mrb.SetGV("$/", "\n")
	a := mrb.AryNew()
	mrb.SetGV("$a", a)

	ret, err := mrb.Eval(`IO.foreach($pid) {|x| $a << x }`)
	assert.NilError(t, err)
	assert.Expect(t, ret.IsNil(), "nil expected")
	assert.Equal(t, a.Interface(), []interface{}{"line 1\n","line 2"})

	_,err= f.Seek(0, io.SeekStart)
	assert.NilError(t, err)
	a.Clear()

	ret, err = mrb.Eval(`IO.foreach($pid, chomp: true) {|x| $a << x }`)
	assert.NilError(t, err)
	assert.Expect(t, ret.IsNil(), "nil expected")
	assert.Equal(t, a.Interface(), []interface{}{"line 1","line 2"})

	_,err= f.Seek(0, io.SeekStart)
	assert.NilError(t, err)
	a.Clear()

	ret, err = mrb.Eval(`IO.foreach($pid, "ne", 2, chomp: true) {|x| $a << x }`)
	assert.NilError(t, err)
	assert.Expect(t, ret.IsNil(), "nil expected")
	assert.Equal(t, a.Interface(), []interface{}{"li",""," 1","\nl","in","e ","2"})

	_,err= f.Seek(0, io.SeekStart)
	assert.NilError(t, err)

	ret, err = mrb.Eval(`IO.foreach($pid, chomp: true).to_a`)
	assert.NilError(t, err)
	assert.Expect(t, ret.Value().IsArray(), "array expected")
	assert.Equal(t, ret.Interface(), []interface{}{"line 1","line 2"})
}

func Test_ioTryConvert(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	f, err := os.Open("testdata/test.txt")
	defer f.Close()
	assert.NilError(t, err)
	mrb.SetGV("$pid", mrb.Value(f))

	ret, err := mrb.Eval(`IO::try_convert($pid)`)
	assert.NilError(t, err)
	assert.Expect(t, ret.Value().IsData(), "data expected")
	v, ok := mrb.Data(ret).(*os.File)
	assert.Expect(t, ok, "*os.File expected")

	mrb.SetGV("$obj", mrb.Value(v))
	assert.NilError(t, err)

	ret, err = mrb.Eval(`IO::try_convert($obj)`)
	assert.NilError(t, err)
	assert.Expect(t, ret.Value().IsData(), "data expected")
	_, ok = mrb.Data(ret).(*os.File)
	assert.Expect(t, ok, "*os.File expected")

	ret, err = mrb.Eval(`IO::try_convert("string_is_not_io")`)
	assert.NilError(t, err)
	assert.Expect(t, ret.IsNil(), "nil expected")
}

