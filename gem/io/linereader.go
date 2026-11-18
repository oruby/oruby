package io

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"

	"github.com/oruby/oruby"
)

func openLineReader(mrb *oruby.MrbState, fd oruby.Value, args oruby.RArgs, kwargs oruby.RHash, index int) (*bufio.Scanner, io.Closer, error) {
	var sep *string
	limit := 0

	globalSeparator := mrb.GetGV("$/")
	if globalSeparator.IsString() {
		tmp := globalSeparator.String()
		sep = &tmp
	}

	arg1 := args.ItemDef(index, globalSeparator)
	arg2 := args.Item(index + 1)
	chomp := kwargs.Get(oruby.Sym_chomp).Bool()

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

	if arg2.IsInteger() {
		limit = arg2.Int()
	}

	f, err := openIO(mrb, fd, mrb.NilValue(), kwargs.Value())
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

func readLine(mrb *oruby.MrbState, fd oruby.Value, args oruby.RArgs, kwargs oruby.RHash, index int) ([]byte, error) {
	var sep *string
	limit := 0

	globalSeparator := mrb.GetGV("$/")
	if globalSeparator.IsString() {
		tmp := globalSeparator.String()
		sep = &tmp
	}

	arg1 := args.ItemDef(index, globalSeparator)
	arg2 := args.Item(index + 1)
	chomp := kwargs.Get(oruby.Sym_chomp).Bool()

	switch arg1.Type() {
	case oruby.MrbTTString:
		tmp := arg1.String()
		sep = &tmp
	case oruby.MrbTTFalse:
		if arg1.IsNil() {
			sep = nil
		} else {
			return nil, oruby.ETypeError("no implicit conversion of false into Integer")
		}
	case oruby.MrbTTFixnum:
		limit = arg1.Int()
	}

	if arg2.IsInteger() {
		limit = arg2.Int()
	}

	// an IO reads through its buffer (rbuf.go)
	if fd.IsData() {
		if _, isFile := mrb.Data(fd).(io.Reader); isFile {
			sr, err := ioReader(mrb, fd)
			if err != nil {
				return nil, err
			}
			return readLineFrom(sr, sep, limit, chomp)
		}
	}

	f, err := openIO(mrb, fd, mrb.NilValue(), kwargs.Value())
	if err != nil {
		return nil, err
	}
	reader, ok := f.(io.Reader)
	if !ok {
		return nil, oruby.EError("IOError", "IO object does not support reading")
	}
	return readLineFrom(&plainReader{reader}, sep, limit, chomp)
}

// lineSource is what readLineFrom reads: an IO's streamReader, or a plain
// reader for a stream that is no IO object
type lineSource interface {
	ReadUntil(sep []byte, limit int) ([]byte, error)
	ReadN(n int) ([]byte, error)
	ReadAll() ([]byte, error)
}

func readLineFrom(src lineSource, sep *string, limit int, chomp bool) ([]byte, error) {
	if sep == nil {
		if limit > 0 {
			return src.ReadN(limit)
		}
		buf, err := src.ReadAll()
		if err != nil {
			return nil, err
		}
		if len(buf) == 0 {
			return nil, io.EOF
		}
		return buf, nil
	}
	separator := []byte(*sep)
	if *sep == "" {
		separator = []byte("\n\n")
	}
	line, err := src.ReadUntil(separator, limit)
	if err != nil {
		return nil, err
	}
	if chomp && bytes.HasSuffix(line, separator) {
		line = line[:len(line)-len(separator)]
	}
	return line, nil
}

// plainReader is a lineSource over an io.Reader, a byte at a time (what
// it reads ahead would be lost)
type plainReader struct{ r io.Reader }

func (p *plainReader) ReadUntil(sep []byte, limit int) ([]byte, error) {
	var line []byte
	one := make([]byte, 1)
	for limit <= 0 || len(line) < limit {
		n, err := p.r.Read(one)
		if n > 0 {
			line = append(line, one[0])
			if len(sep) > 0 && bytes.HasSuffix(line, sep) {
				return line, nil
			}
		}
		if err != nil {
			if len(line) > 0 {
				return line, nil
			}
			return nil, err
		}
	}
	if len(line) == 0 {
		return nil, io.EOF
	}
	return line, nil
}

func (p *plainReader) ReadN(n int) ([]byte, error) {
	buf := make([]byte, n)
	m, err := io.ReadFull(p.r, buf)
	if m > 0 {
		return buf[:m], nil
	}
	if err == nil || errors.Is(err, io.ErrUnexpectedEOF) {
		err = io.EOF
	}
	return nil, err
}

func (p *plainReader) ReadAll() ([]byte, error) { return io.ReadAll(p.r) }

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
		if os.PathSeparator == '\\' {
			// Windows uses \ as path separator, and CRLF as line ending
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
			return i + 1, data[0 : i+sepLen], nil
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
