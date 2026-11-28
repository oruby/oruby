//go:build !darwin && !linux

package oruby

import "os"

func mapOrbFile(_ *os.File, _ int) ([]byte, func(), error) { return nil, nil, nil }
