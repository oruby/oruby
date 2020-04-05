package file

import (
	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/assert"
	"os"
	"path/filepath"
	"testing"
)

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

// TODO: try not to leak fd
func Test_ioSSysopen(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	v, err := mrb.Eval(`IO.sysopen "testdata/test.txt"`)
	assert.NilError(t, err)
	assert.Equal(t, v.Type(), oruby.MrbTTCptr)
}

func Test_fileOpenIO(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	pid, err := mrb.Eval(`$pid = IO.sysopen "testdata/test.txt"`)
	assert.NilError(t, err)

	f, err := fileOpenIO(mrb, pid.Value(), mrb.NilValue(), mrb.NilValue())
	assert.NilError(t, err)
	if _, ok := f.(*os.File); !ok {
		t.Error("File expected")
	}

	f, err = fileOpenIO(mrb, mrb.StrNew("testdata/test.txt"), mrb.NilValue(), mrb.NilValue())
	assert.NilError(t, err)
	if _, ok := f.(*os.File); !ok {
		t.Error("File expected")
	}

	file, err := os.Open("testdata/test.txt")
	assert.NilError(t, err)

	f2, err := fileOpenIO(mrb, mrb.DataValue(file), mrb.NilValue(), mrb.NilValue())
	assert.NilError(t, err)
	if _, ok := f2.(*os.File); !ok {
		t.Error("File expected")
	}
}

func Test_fileGetStream(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	f, err := fileGetStream(mrb, os.O_RDONLY, mrb.Value(os.Stdout))
	assert.NilError(t, err)
	assert.Equal(t, f, os.Stdout)

	tmpName := filepath.Join(os.TempDir(), "test.tmp")
	f2, err := fileGetStream(mrb, os.O_WRONLY | os.O_CREATE | os.O_TRUNC, mrb.StrNew(tmpName))
	assert.NilError(t, err)
	assert.Equal(t, f2.(*os.File).Name(), tmpName)

	_, err = fileGetStream(mrb, os.O_WRONLY, oruby.Int(12345))
	assert.Error(t, err, "should be non-stream error")
}

