//go:build !unix

package main

import "os/exec"

func configureProcess(cmd *exec.Cmd) {}
