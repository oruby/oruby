package popen

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/process"
)

type superPipe struct {
	io.ReadCloser
	io.WriteCloser
	pid    int
	mode   int
	cmd    *exec.Cmd
	mrb    *oruby.MrbState
	waited bool
}

// wait reaps the child once and sets $? (IO#close on a popen stream)
func (p *superPipe) wait() {
	if p.waited || p.cmd == nil {
		return
	}
	p.waited = true
	_ = p.cmd.Wait()
	if ps := p.cmd.ProcessState; ps != nil && p.mrb != nil {
		process.SetLastStatus(p.mrb, p.pid, ps.ExitCode(), ps.Exited(), ps.Success())
	}
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
	p.wait()
	if errW != nil || errR != nil {
		return &pipeError{errW, errR}
	}

	return nil
}

// Read and Write honor the popen mode: "w" streams can't be read, "r"
// streams can't be written (IOError in Ruby)
func (p *superPipe) Read(b []byte) (int, error) {
	if p.mode&(os.O_WRONLY|os.O_RDWR) == os.O_WRONLY {
		return 0, &os.PathError{Op: "read", Path: "pipe", Err: syscall.EBADF}
	}
	return p.ReadCloser.Read(b)
}

func (p *superPipe) Write(b []byte) (int, error) {
	if p.mode&(os.O_WRONLY|os.O_RDWR) == os.O_RDONLY {
		return 0, &os.PathError{Op: "write", Path: "pipe", Err: syscall.EBADF}
	}
	return p.WriteCloser.Write(b)
}
