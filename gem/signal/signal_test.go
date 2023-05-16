package signal

import (
	"syscall"
	"testing"
	"time"

	"github.com/oruby/oruby"
)

func TestTrap(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`
		$testv = 0
		Signal.trap("USR1") { $testv = 1; p("got USR1",$testv) }
		Signal.trap(0) { p "Exited." }
	`)
	if err != nil {
		t.Error(err)
		return
	}

	v := mrb.GetGV("$testv")

	if !v.IsInteger() || v.Int() != 0 {
		t.Errorf("expected 0 got %v", mrb.Inspect(v))
		return
	}

	// Give some time for signal to be processed
	<-time.After(10 * time.Millisecond)

	err = syscall.Kill(syscall.Getpid(), syscall.SIGUSR1)
	if err != nil {
		t.Error(err)
		return
	}

	// Give some time for signal to be processed
	<-time.After(10 * time.Millisecond)

	v2 := mrb.GetGV("$testv")

	if v2.Int() != 1 {
		t.Errorf("expected 1 got %v", mrb.String(v2))
	}
}

func TestInfinite(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	go func() {
		<-time.After(100 * time.Millisecond)
		err := syscall.Kill(syscall.Getpid(), syscall.SIGUSR1)
		if err != nil {
			t.Error(err)
			return
		}
	}()

	_, err := mrb.Eval(`
		$testv = 0
		Signal.trap("USR1") { p "Received USR1"; raise StopIteration }
		loop do
			$testv = 1
		end		
	`)
	if err != nil {
		t.Error(err)
		return
	}

	v2 := mrb.GetGV("$testv").Int()
	if v2 != 1 {
		t.Errorf("expected 1 got %v", v2)
	}
}

func TestList(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	signal := mrb.ModuleGet("Signal")
	list := signal.Call("list")
	if mrb.Err() != nil {
		t.Error(mrb.Err())
		return
	}
	sigs, ok := mrb.Intf(list).(map[string]interface{})
	if !ok {
		t.Error("Signal list is not map[string]interface{}")
		return
	}

	if len(sigs) != len(signals) {
		t.Errorf("expected %v items, got %v", len(signals), len(sigs))
	}

	if sigs["INT"] != int(syscall.SIGINT) {
		t.Errorf("expected 'INT', got '%v'", sigs["INT"])
	}

	if sigs["KILL"] != int(syscall.SIGKILL) {
		t.Errorf("expected 'KILL', got '%v'", sigs["KILL"])
	}
}

func TestSigname(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	signal := mrb.ModuleGet("Signal")
	sig := signal.Call("signame", int(syscall.SIGINT)).String()
	if sig != "INT" {
		t.Errorf("expected 'INT', got '%v'", sig)
	}

	sig = signal.Call("signame", int(syscall.SIGKILL)).String()
	if sig != "KILL" {
		t.Errorf("expected 'KILL', got '%v'", sig)
	}

	//mSignal.DefineModuleFunction("signame", sigName, mrb.ArgsReq(1))
}

func TestSignalExceptions(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	eSignal := mrb.ClassGet("SignalException")
	e, err := eSignal.New("INT", "Message")
	if err != nil {
		t.Error(err)
		return
	}
	if e.GetIV("@signo").Int() != int(syscall.SIGINT) {
		t.Error("Signo: ", e.GetIV("@signo"))
	}

	if e.GetIV("mesg").String() != "Message" {
		t.Error("Message: ", e.GetIV("mesg"))
	}

	eInterrupt := mrb.ClassGet("Interrupt")
	e, err = eInterrupt.New("Message2")
	if err != nil {
		t.Error(err)
		return
	}

	if e.GetIV("@signo").Int() != int(syscall.SIGINT) {
		t.Error("Signo: ", e.GetIV("@signo"))
	}

	if e.GetIV("mesg").String() != "Message2" {
		t.Error("Message2: ", e.GetIV("mesg"))
	}
}
