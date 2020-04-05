package file

import (
	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/assert"
	"os"
	"testing"
)

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

