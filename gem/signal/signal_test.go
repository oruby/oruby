package signal

import (
	"github.com/oruby/oruby"
	"syscall"
	"testing"
)

func TestTrap(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	// mSignal.DefineModuleFunction("trap", sigTrap, mrb.ArgsArg(1, 1)+mrb.ArgsBlock())
	// syscall.Kill(syscall.Getpid(), syscall.SIGINT)
	t.Fatal("NOT READY YET")
}

func TestList(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	signal := mrb.ModuleGet("Signal")
	list := signal.Call("list")
	if mrb.Err() != nil {
		t.Fatal(mrb.Err())
	}
	sigs, ok := mrb.Intf(list).(map[string]interface{})
	if !ok {
		t.Fatal("Signal list is not map[string]interface{}")
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
		t.Fatal(err)
	}
	if e.GetIV("@signo").Int() != int(syscall.SIGINT) {
		t.Error("Signo: ", e.GetIV("@signo"))
	}

	if e.GetIV("mesg").String() != "Message" {
		t.Error("Message: ", e.GetIV("mesg"))
	}

	eInterrupt := mrb.ClassGet("Interrupt")
	e, err = eInterrupt.New( "Message2")
	if err != nil {
		t.Fatal(err)
	}

	if e.GetIV("@signo").Int() != int(syscall.SIGINT) {
		t.Error("Signo: ", e.GetIV("@signo"))
	}

	if e.GetIV("mesg").String() != "Message2" {
		t.Error("Message2: ", e.GetIV("mesg"))
	}
}