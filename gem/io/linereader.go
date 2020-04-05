package io

import (
	"bufio"
	"bytes"
	"github.com/oruby/oruby"
	"io"
	"runtime"
)

func openLineReader(mrb *oruby.MrbState, fd oruby.Value, args oruby.RArgs, index int) (*bufio.Scanner, io.Closer, error) {
	var sep *string
	limit := 0

	globalSeparator := mrb.GetGV("$/")
	if globalSeparator.IsString() {
		tmp := globalSeparator.String()
		sep = &tmp
	}

	arg1 := args.ItemDef(index, globalSeparator)
	arg2 := args.Item(index + 1)
	opt := args.GetLastHash()
	chomp := opt.IsHash() && mrb.HashGet(opt, mrb.Intern("chomp")).Bool()

	switch arg1.Type() {
	case oruby.MrbTTString:
		tmp := arg1.String()
		sep = &tmp
	case oruby.MrbTTFalse:
		if arg1.IsNil() {
			sep = nil
		} else {
			return nil, nil, oruby.ETypeError("no implicit conversion of false into Integer")
		}
	case oruby.MrbTTFixnum:
		limit = arg1.Int()
	}

	if arg2.IsFixnum() {
		limit = arg2.Int()
	}

	f, err := openIO(mrb, fd, mrb.NilValue(), opt)
	if err != nil {
		return nil, nil, err
	}

	reader, ok := f.(io.Reader)
	if !ok {
		return nil, nil, oruby.EError("IOError", "IO object does not support reading")
	}

	lineReader := bufio.NewScanner(reader)
	lineReader.Split(getSpliter(sep, chomp, limit))

	if closer, ok := reader.(io.Closer); ok && !fd.IsData() {
		return lineReader, closer, nil
	}
	return lineReader, nil, nil
}

func getSpliter(sep *string, chomp bool, limit int) bufio.SplitFunc {
	if sep == nil {
		return func(data []byte, atEOF bool) (advance int, token []byte, err error) {
			// Return nothing if at end of file and no data passed
			if atEOF && len(data) == 0 {
				return 0, nil, io.EOF
			}

			// If at end of file with data return the data
			if atEOF {
				return len(data), data, nil
			}

			return
		}
	}

	// paragraphs
	separator := []byte(*sep)
	if separator == nil {
		if runtime.GOOS == "windows" {
			separator = []byte("\n\r\n\r")
		} else {
			separator = []byte("\n\n")
		}
	}

	sepLen := len(separator)

	// Custom separator
	return func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		// Return nothing if at end of file and no data passed
		if atEOF && len(data) == 0 {
			return 0, nil, io.EOF
		}

		// Find the index of the input of a separator
		if i := bytes.Index(data, separator); i >= 0 {
			// Return up to limit, if limit set and hit
			if limit > 0 && i+sepLen > limit {
				return limit, data[0:limit], nil
			}

			// Return line without spearator, if chomp flag set
			if chomp && bytes.Equal(data[i:i+sepLen], separator) {
				return i + sepLen, data[:i], nil
			}

			// Return line with separator, as Ruby does
			return i + 1, data[0:i+sepLen], nil
		}

		if limit > 0 && len(data) > limit {
			return limit, data[0:limit], nil
		}

		// If at end of file with data return the data
		if atEOF {
			return len(data), data, nil
		}

		return
	}
}

