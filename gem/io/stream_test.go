package io

import (
	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/assert"
	"testing"
)

func Test_ioInitCopy(t *testing.T) {
}

func Test_valueToS(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	v2s := func(v oruby.Value) string  {
		t.Helper()
		ret, err := valueToS(mrb, v)
		if err != nil {
			t.Error(err)
			return ""
		}
		return ret.String()
	}

  	assert.Equal(t, v2s(mrb.Value("test")), "test")
	assert.Equal(t, v2s(mrb.Value(23)), "23")
	assert.Equal(t, v2s(mrb.TrueValue()), "true")
	assert.Equal(t, v2s(mrb.NilValue()), "")
	assert.Equal(t, v2s(mrb.Value(22.2)), "22.2")
}

func Test_ioWriteString(t *testing.T) {
}

func  Test_ioSetAutosclose(t *testing.T) {
}

func  Test_ioIsAutoclose(t *testing.T) {
}

func Test_ioBinmode(t *testing.T) {
}

func Test_ioIsBinbode(t *testing.T) {
}

func Test_ioClose(t *testing.T) {
}

func  Test_ioIsCloseOnExec(t *testing.T) {
}

func Test_ioCloseRead(t *testing.T) {
}

func Test_ioCloseWrite(t *testing.T) {
}

func Test_ioIsClosed(t *testing.T) {
}

func Test_ioEach(t *testing.T) {
}

func Test_ioEachByte(t *testing.T) {
}

func Test_ioEachChar(t *testing.T) {
}

func Test_ioEachCodepoint(t *testing.T) {
}

func Test_ioIsEof(t *testing.T) {
}

func Test_ioFileno(t *testing.T) {
}

func Test_ioFlush(t *testing.T) {
}

func Test_getFile(t *testing.T) {
}

func Test_ioFsync(t *testing.T) {
}

func Test_ioGetbyte(t *testing.T) {
}

func Test_ioGetc(t *testing.T) {
}

func Test_setLastLine(t *testing.T) {
}

func Test_ioGets(t *testing.T) {
}

func Test_ioInspect(t *testing.T) {
}

func Test_ioLineno(t *testing.T) {
}

func Test_ioSetLineno(t *testing.T) {
}

func Test_ioPid(t *testing.T) {
}

func Test_ioPos(t *testing.T) {
}

func Test_ioSetPos(t *testing.T) {
}

func Test_ioPread(t *testing.T) {
}

func Test_ioPrint(t *testing.T) {
}

func Test_ioPrintf(t *testing.T) {
}

func Test_ioPutc(t *testing.T) {
}

func Test_pArray(t *testing.T) {

}

func Test_ioPuts(t *testing.T) {
}

func Test_ioPwrite(t *testing.T) {
}

func Test_ioRead(t *testing.T) {
}

func Test_ioReadbyte(t *testing.T) {
}

func Test_ioReadchar(t *testing.T) {
}

func Test_ioReadline(t *testing.T) {
}

func Test_ioReadlines(t *testing.T) {
}

func Test_ioReadpartial(t *testing.T) {
}

func Test_ioReopen(t *testing.T) {
}

func Test_ioRewind(t *testing.T) {
}

func Test_ioSeek(t *testing.T) {
}

func Test_ioStat(t *testing.T) {
}

func Test_ioSync(t *testing.T) {
}

func Test_ioSetSync(t *testing.T) {
}

func Test_ioTell(t *testing.T) {
}

func Test_ioToIo(t *testing.T) {
}

// TODO: not same as MRI, which modifies internal buffer with byte
func Test_ioUngetbyte(t *testing.T) {
}

// TODO: not same as MRI, which modifies internal buffer with chr
func Test_ioUngetc(t *testing.T) {
}

func Test_ioWrite(t *testing.T) {
}


