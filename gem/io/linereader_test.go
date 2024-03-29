package io

import (
	"bufio"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/assert"
)

type h = map[oruby.MrbSym]interface{}

func Test_openLineReader(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	olr := func(fd, arg1, arg2 interface{}, opt h) (*bufio.Scanner, io.Closer, error) {
		var err error
		if name, ok := fd.(string); ok {
			fd, err = os.Open(name)
			assert.NilError(t, err)
		}
		args := oruby.RArgsNew(mrb.Value(arg1), mrb.Value(arg2))
		options := mrb.EnsureHashType(mrb.Value(opt))
		return openLineReader(mrb, mrb.Value(fd), args, options, 0)
	}

	r, _, err := olr(strings.NewReader("test$test$test$"), "$", nil, nil)
	assert.NilError(t, err)
	r.Scan()
	assert.NilError(t, r.Err())
	assert.Equal(t, r.Text(), "test$")

	r, _, err = olr(strings.NewReader("test$test$test$"), "$", nil, h{mrb.Intern("chomp"): true})
	assert.NilError(t, err)
	r.Scan()
	assert.NilError(t, r.Err())
	assert.Equal(t, r.Text(), "test")

	r, _, err = olr(strings.NewReader("testlongSeptestlongSep$"), "longSep", nil, nil)
	assert.NilError(t, err)
	r.Scan()
	assert.NilError(t, r.Err())
	assert.Equal(t, r.Text(), "testlongSep")

	mrb.SetGV("$/", "$$$")
	r, _, err = olr(strings.NewReader("test$test$$$test$"), mrb.GetGV("$/"), nil, nil)
	assert.NilError(t, err)
	r.Scan()
	assert.NilError(t, r.Err())
	assert.Equal(t, r.Text(), "test$test$$$")

	r, _, err = olr(strings.NewReader("test$qqst$test$"), "$", 2, nil)
	assert.NilError(t, err)
	r.Scan()
	assert.NilError(t, r.Err())
	assert.Equal(t, r.Text(), "te")
	r.Scan()
	assert.NilError(t, r.Err())
	assert.Equal(t, r.Text(), "st")
	r.Scan()
	assert.NilError(t, r.Err())
	assert.Equal(t, r.Text(), "$")
	r.Scan()
	assert.NilError(t, r.Err())
	assert.Equal(t, r.Text(), "qq")
}

func Test_openLineReader_reg1(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	olr := func(fd, arg1, arg2 interface{}, opt h) (*bufio.Scanner, io.Closer, error) {
		var err error
		if name, ok := fd.(string); ok {
			fd, err = os.Open(name)
			assert.NilError(t, err)
		}
		args := oruby.RArgsNew(mrb.Value(arg1), mrb.Value(arg2))
		options := mrb.EnsureHashType(mrb.Value(opt))
		return openLineReader(mrb, mrb.Value(fd), args, options, 0)
	}

	r, _, err := olr(strings.NewReader("test$test$test$"), "$", nil, h{mrb.Sym("chomp"): true})
	assert.NilError(t, err)
	r.Scan()
	assert.NilError(t, r.Err())
	assert.Equal(t, r.Text(), "test")
}

func Test_openLineReaderSepLimitChomp(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	chompTrue := h{mrb.Intern("chomp"): true}

	olr := func(fd, arg1, arg2 interface{}, opt h) (*bufio.Scanner, io.Closer, error) {
		args := oruby.RArgsNew(mrb.Value(arg1), mrb.Value(arg2))
		options := mrb.EnsureHashType(mrb.Value(opt))
		return openLineReader(mrb, mrb.Value(fd), args, options, 0)
	}

	r, _, err := olr(strings.NewReader("test$qqst$test$"), "$", 2, chompTrue)
	assert.NilError(t, err)
	r.Scan()
	assert.NilError(t, r.Err())
	assert.Equal(t, r.Text(), "te")
	r.Scan()
	assert.NilError(t, r.Err())
	assert.Equal(t, r.Text(), "st")
	r.Scan()
	assert.NilError(t, r.Err())
	assert.Equal(t, r.Text(), "")
	r.Scan()
	assert.NilError(t, r.Err())
	assert.Equal(t, r.Text(), "qq")

	r, _, err = olr(strings.NewReader("line 1\nline 2"), "ne", 2, chompTrue)
	assert.NilError(t, err)
	assert.Equal(t, r.Scan(), true)
	assert.Equal(t, r.Text(), "li")
	assert.Equal(t, r.Scan(), true)
	assert.Equal(t, r.Text(), "")
	assert.Equal(t, r.Scan(), true)
	assert.Equal(t, r.Text(), " 1")
	assert.Equal(t, r.Scan(), true)
	assert.Equal(t, r.Text(), "\nl")
	assert.Equal(t, r.Scan(), true)
	assert.Equal(t, r.Text(), "in")
	assert.Equal(t, r.Scan(), true)
	assert.Equal(t, r.Text(), "e ")
	assert.Equal(t, r.Scan(), true)
	assert.Equal(t, r.Text(), "2")
}
