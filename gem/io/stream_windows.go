package io

import (
	"errors"
	"os"
)

func platformDup(f *os.File) (*os.File, error) {
	return nil, errors.New("dup() not supported on Windows")
}

func platformIsatty(f *os.File) bool {
	return false
}
