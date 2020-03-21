package io

import (
	"fmt"
	"io"
)

type superPipe struct {
	io.ReadCloser
	io.WriteCloser
	pid int
	mode int
}
type pipeError struct {
	errW error
	errR error
}

func (e pipeError) Error() string {
	if e.errW != nil && e.errR == nil {
		return e.errW.Error()
	} else if e.errW == nil && e.errR != nil {
		return e.errR.Error()
	}
	return fmt.Sprintf("%v %v", e.errW, e.errR)
}

func (p *superPipe) Close() error {
	errW := p.WriteCloser.Close()
	errR := p.ReadCloser.Close()
	if errW != nil || errR != nil {
		return &pipeError{errW, errR}
	}

	return nil
}
