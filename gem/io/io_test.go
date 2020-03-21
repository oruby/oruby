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

	v := raiseEOF(mrb)
	assert.Expect(t, mrb.ObjIsKindOf(v, mrb.EExceptionClass()), "exception excepted")
	assert.Expect(t, mrb.Exc() != nil, "Should be raised")
	assert.Expect(t, *mrb.Exc() == mrb.RObject(v), "Should be raised")
	assert.Expect(t, *mrb.Exc() == mrb.RObject(v), "Should be raised")
}

func Test_raiseIOError(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	v := raiseIOError(mrb, "IO error")
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
	f2, err := getStream(mrb, os.O_WRONLY | os.O_CREATE | os.O_TRUNC, mrb.StrNew(tmpName))
	assert.NilError(t, err)
	assert.Equal(t, f2.(*os.File).Name(), tmpName)

	_, err = getStream(mrb, os.O_WRONLY, oruby.Int(12345))
	assert.Error(t, err, "should be non-stream error")
}

func Test_ioCopyStream(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	tmpName := filepath.Join(os.TempDir(), "test.tmp")
	mrb.SetGV("$dest", tmpName)

	v, err := mrb.Eval(`IO.copy_stream "testdata/test.txt", $dest`)
	assert.NilError(t, err)
	assert.Expect(t, v.Value().IsFixnum(), "shoud return length")

	tmp2Name := filepath.Join(os.TempDir(), "test2.tmp")
	f,_ := os.OpenFile(tmp2Name, os.O_CREATE|os.O_WRONLY, os.ModePerm)
	mrb.SetGV("$dest2", f)

	v, err = mrb.Eval(`IO.copy_stream "testdata/test.txt", $dest2`)
	assert.NilError(t, err)
	assert.Expect(t, v.Value().IsFixnum(), "shoud return length")

	stat, err := f.Stat()
	assert.NilError(t, err)
	assert.Expect(t, stat.Size() > 0, "shoud have length")
	assert.Equal(t, stat.Size(), v.Value().Int64())
}

func Test_rwNameLenOffset(t *testing.T) {
	offset := int64(1)
	length := int64(-1)
	f, err := rwNameLenOffset("testdata/test.txt", offset, &length)
	assert.NilError(t, err)
	stat, err := f.Stat()
	assert.NilError(t, err)
	assert.Equal(t, stat.Size() - offset, length)
}

func Test_ioBinread(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	v, err := mrb.Eval(`IO.binread("testdata/test.txt", 3, 1)`)
	assert.NilError(t, err)
	assert.Equal(t, v.Type(), oruby.MrbTTString)
	assert.Equal(t, v.String(),"ine")
}

func Test_ioBinwrite(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	tmpName := filepath.Join(os.TempDir(), "test.tmp")
	mrb.SetGV("$tmp", mrb.StrNew(tmpName))
	v, err := mrb.Eval(`IO.binwrite($tmp, "test")`)
	assert.NilError(t, err)
	assert.Equal(t, v.Type(), oruby.MrbTTFixnum)
	assert.Equal(t, v.Int(),  len("test"))

	ret, err := mrb.Eval(`IO.binread($tmp)`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Type(), oruby.MrbTTString)
	assert.Equal(t, ret.String(),  "test")
}

func Test_modestrToFlags(t *testing.T) {
	m,err := modestrToFlags("r")
	assert.NilError(t, err)
	assert.Equal(t,m, os.O_RDONLY)
	m,_ = modestrToFlags("w")
	assert.Equal(t, m, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	m,_ = modestrToFlags("a")
	assert.Equal(t, m, os.O_WRONLY|os.O_CREATE|os.O_APPEND)
	//m,_ = modestrToFlags("rb")
	//assert.Equal(t, m, os.O_RDONLY|os.O_BINARY)
	//m,_ = modestrToFlags("rt")
	//assert.Equal(t, m, os.O_RDONLY|os.O_TEXT)
	m,_ = modestrToFlags("r+")
	assert.Equal(t, m, os.O_RDWR)
	m,_ = modestrToFlags("wx")
	assert.Equal(t, m, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_EXCL)
	m,err = modestrToFlags("rx")
	assert.Error(t, err, "read exclusive should not be supported")
	m,_ = modestrToFlags("r:")
	assert.Equal(t,m, os.O_RDONLY)

	m,err = modestrToFlags("invalid")
	assert.Error(t, err, "invlid flags should raise error")
}

func Test_modeToFlags(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	flags, err := modeToFlags(mrb, mrb.NilValue())
	assert.NilError(t, err)
	assert.Equal(t, flags, 0)

	flags, _ = modeToFlags(mrb, mrb.StrNew("a"))
	assert.Equal(t, flags, os.O_WRONLY | os.O_CREATE | os.O_APPEND)

	flags, _ = modeToFlags(mrb, mrb.FixnumValue(os.O_WRONLY | os.O_CREATE))
	assert.Equal(t, flags, os.O_WRONLY | os.O_CREATE)

	opt := mrb.HashNew()
	opt.SetI(mrb.Intern("mode"), "w")
	flags, _ = modeToFlags(mrb, opt.Value())
	assert.Equal(t, flags, os.O_WRONLY | os.O_CREATE | os.O_TRUNC)

	opt = mrb.HashNew()
	opt.SetI(mrb.Intern("flags"), os.O_WRONLY)
	flags, _ = modeToFlags(mrb, opt.Value())
	assert.Equal(t, flags, os.O_WRONLY)

	opt = mrb.HashNew()
	opt.SetI(mrb.Intern("flags"), os.O_TRUNC)
	opt.SetI(mrb.Intern("mode"), "a")
	flags, _ = modeToFlags(mrb, opt.Value())
	assert.Equal(t, flags, os.O_WRONLY | os.O_CREATE | os.O_APPEND | os.O_TRUNC)
}

func Test_parseFlags(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	opt := mrb.HashNew()
	opt.SetI(mrb.Intern("flags"), os.O_RDONLY|os.O_EXCL)

	f, err := parseFlags(mrb, mrb.Value("r"), opt.Value())
	assert.NilError(t, err)
	assert.Equal(t, f, os.O_RDONLY|os.O_EXCL)

	opt.SetI(mrb.Intern("flags"), os.O_RDONLY)

	f, err = parseFlags(mrb, mrb.NilValue(), opt.Value())
	assert.NilError(t, err)
	assert.Equal(t, f, os.O_RDONLY)

	opt.Clear()
	opt.SetI(mrb.Intern("mode"), "r+")

	f, err = parseFlags(mrb, mrb.NilValue(), opt.Value())
	assert.NilError(t, err)
	assert.Equal(t, f, os.O_RDWR)
}

// TODO: try not to leak fd
func Test_ioSSysopen(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	v, err := mrb.Eval(`IO.sysopen "testdata/test.txt"`)
	assert.NilError(t, err)
	assert.Equal(t, v.Type(), oruby.MrbTTCptr)
}

func Test_openIO(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	pid, err := mrb.Eval(`$pid = IO.sysopen "testdata/test.txt"`)
	assert.NilError(t, err)

	f, err := openIO(mrb, pid.Value(), mrb.NilValue(), mrb.NilValue())
	assert.NilError(t, err)
	if _, ok := f.(*os.File); !ok {
		t.Error("File expected")
	}

	f, err = openIO(mrb, mrb.StrNew("testdata/test.txt"), mrb.NilValue(), mrb.NilValue())
	assert.NilError(t, err)
	if _, ok := f.(*os.File); !ok {
		t.Error("File expected")
	}

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

	_, err := mrb.Eval(`$pid = IO.sysopen "testdata/test.txt"`)
	assert.NilError(t, err)

	v, err := mrb.Eval(`$obj = IO.new($pid)`)
	assert.NilError(t, err)
	assert.Expect(t, v.Value().IsData(), "IO object expected")
}

func Test_ioOpen(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	v, err := mrb.Eval(`$pid = IO.open("testdata/test.txt") {|o| o.readlines }`)
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

	mrb.SetGV("$/", "\n")

	ret, err := mrb.Eval(`IO.readlines("testdata/test.txt")`)
	assert.NilError(t, err)
	assert.Equal(t, ret.Interface(), []interface{}{"line 1\n","line 2"})

	ret, err = mrb.Eval(`IO.readlines("testdata/test.txt", chomp: true)`)
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

	mrb.SetGV("$/", "\n")
	a := mrb.AryNew()
	mrb.SetGV("$a", a)

	ret, err := mrb.Eval(`IO.foreach("testdata/test.txt") {|x| $a << x }`)
	assert.NilError(t, err)
	assert.Expect(t, ret.IsNil(), "nil expected")
	assert.Equal(t, a.Interface(), []interface{}{"line 1\n","line 2"})

	a.Clear()
	ret, err = mrb.Eval(`IO.foreach("testdata/test.txt", chomp: true) {|x| $a << x }`)
	assert.NilError(t, err)
	assert.Expect(t, ret.IsNil(), "nil expected")
	assert.Equal(t, a.Interface(), []interface{}{"line 1","line 2"})

	a.Clear()
	ret, err = mrb.Eval(`IO.foreach("testdata/test.txt", "ne", 2, chomp: true) {|x| $a << x }`)
	assert.NilError(t, err)
	assert.Expect(t, ret.IsNil(), "nil expected")
	assert.Equal(t, a.Interface(), []interface{}{"li",""," 1","\nl","in","e ","2"})

	ret, err = mrb.Eval(`IO.foreach("testdata/test.txt", chomp: true).to_a`)
	assert.NilError(t, err)
	assert.Expect(t, ret.Value().IsArray(), "array expected")
	assert.Equal(t, ret.Interface(), []interface{}{"line 1","line 2"})
}

func Test_ioTryConvert(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`$pid = IO.sysopen "testdata/test.txt"`)
	assert.NilError(t, err)

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

func Test_ioPopen(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	v, err := mrb.Eval(`$p = IO.popen("/bin/echo test")`)
	assert.NilError(t, err)

	ret := mrb.Call(v, "readlines")
	assert.NilError(t, mrb.Err())
	assert.Expect(t, ret.IsArray(), "array expected, got %v", mrb.String(ret))
	assert.Expect(t, ret.Len() > 0,  "array len expected")
	println(ret.Len())

	r := mrb.AryPop(ret)
	assert.Expect(t, r.IsString(), "string item expected")
	assert.Expect(t, r.String() == "test\n", "array expected")

	_,err = mrb.Eval("Process.wait $pid")
	assert.NilError(t, mrb.Err())
}

