package io

import (
	"bufio"
	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/assert"
	"io"
	"os"
	"strings"
	"testing"
)

type h = map[oruby.MrbSym]interface{}

func Test_openLineReader(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	olr := func(fd, arg1, arg2 interface{}, opt h) (*bufio.Scanner, io.Closer, error) {
		args := oruby.RArgsNew(mrb.Value(arg1), mrb.Value(arg2), mrb.Value(opt))
		return openLineReader(mrb, mrb.Value(fd), args, 0)
	}

	r, c, err := olr("testdata/test.txt", nil, nil, nil)
	assert.NilError(t, err)
	assert.Expect(t, c != nil, "file should implement closer")
	assert.Equal(t, c.(*os.File).Name(), "testdata/test.txt")

	r, _, err = olr(strings.NewReader("test$test$test$"), "$", nil, nil)
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

func Test_openLineReaderSepLimitChomp(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	chompTrue := h{mrb.Intern("chomp"): true}

	olr := func(fd, arg1, arg2 interface{}, opt h) (*bufio.Scanner, io.Closer, error) {
		args := oruby.RArgsNew(mrb.Value(arg1), mrb.Value(arg2), mrb.Value(opt))
		return openLineReader(mrb, mrb.Value(fd), args, 0)
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

