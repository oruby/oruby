package io

import "errors"

func platformDup(f *os.File) (*os.File, error) {
	return nil, errors.New("dup() not supported on Windows")
}

