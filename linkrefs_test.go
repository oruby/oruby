package oruby

import (
	"fmt"
	"sync"
	"testing"
)

func linkrefsIndex(mrb *MrbState) int {
	mu.Lock()
	defer mu.Unlock()
	for i, state := range states {
		if state == mrb {
			return i
		}
	}
	return -1
}

func linkrefsOpen(t *testing.T) *MrbState {
	t.Helper()
	mrb, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(mrb.Close)
	return mrb
}

func TestLinkrefsReuse(t *testing.T) {
	first := linkrefsOpen(t)
	firstIndex := linkrefsIndex(first)
	if firstIndex <= 0 {
		t.Fatalf("first state index = %d", firstIndex)
	}
	first.Close()
	if got := lookupMrbStateIndex(firstIndex); got != nil {
		t.Fatalf("closed state lookup = %p, want nil", got)
	}

	second := linkrefsOpen(t)
	if index := linkrefsIndex(second); index != firstIndex {
		t.Fatalf("reused state index = %d, want %d", index, firstIndex)
	}
	if got := getMrbStateIndex(firstIndex); got != second {
		t.Fatalf("index lookup = %p, want %p", got, second)
	}
	if got := lookupMrbStateIndex(firstIndex); got != second {
		t.Fatalf("nullable index lookup = %p, want %p", got, second)
	}
	if got := GetMrbState(second.State()); got != second {
		t.Fatalf("pointer lookup = %p, want %p", got, second)
	}
	if got := getMrbState(second.p); got != second {
		t.Fatalf("C state lookup = %p, want %p", got, second)
	}

	third := linkrefsOpen(t)
	if index := linkrefsIndex(third); index <= 0 || index == firstIndex {
		t.Fatalf("active state index = %d, conflicts with %d", index, firstIndex)
	}
}

func TestLinkrefsReuseLargeRegistry(t *testing.T) {
	mu.Lock()
	originalLen := len(states)
	for len(states) <= 500 {
		states = append(states, nil)
	}
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		states = states[:originalLen]
		mu.Unlock()
	})

	mrb := linkrefsOpen(t)
	if index := linkrefsIndex(mrb); index <= 0 {
		t.Fatalf("state not registered in large registry: index = %d", index)
	}
}

func TestLinkrefsInvalidIndex(t *testing.T) {
	mu.Lock()
	outOfRange := len(states)
	mu.Unlock()
	for _, tc := range []struct {
		index int
		want  string
	}{
		{-1, "State index is out of range"},
		{outOfRange, "State index is out of range"},
		{0, "State does not exists"},
	} {
		t.Run(fmt.Sprint(tc.index), func(t *testing.T) {
			if got := lookupMrbStateIndex(tc.index); got != nil {
				t.Errorf("nullable lookup = %p, want nil", got)
			}
			defer func() {
				if got := recover(); fmt.Sprint(got) != tc.want {
					t.Errorf("panic = %v, want %s", got, tc.want)
				}
			}()
			getMrbStateIndex(tc.index)
		})
	}
}

func TestLinkrefsConcurrentIndependentStates(t *testing.T) {
	const workers, iterations = 16, 20
	var wg sync.WaitGroup
	start := make(chan struct{})
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < iterations; i++ {
				mrb, err := New()
				if err != nil {
					t.Errorf("New: %v", err)
					return
				}
				index := linkrefsIndex(mrb)
				if index <= 0 {
					t.Errorf("state not registered: index = %d", index)
				} else if got := getMrbStateIndex(index); got != mrb {
					t.Errorf("index lookup = %p, want %p", got, mrb)
				} else if got := GetMrbState(mrb.State()); got != mrb {
					t.Errorf("pointer lookup = %p, want %p", got, mrb)
				} else if got := getMrbState(mrb.p); got != mrb {
					t.Errorf("C state lookup = %p, want %p", got, mrb)
				}
				if got := lookupMrbStateIndex(index); got != mrb {
					t.Errorf("nullable lookup = %p, want %p", got, mrb)
				}
				mrb.Close()
			}
		}()
	}
	close(start)
	wg.Wait()
}

func TestLinkrefsConcurrentCallbacks(t *testing.T) {
	const workers, iterations = 8, 20
	var wg sync.WaitGroup
	start := make(chan struct{})
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < iterations; i++ {
				mrb, err := New()
				if err != nil {
					t.Errorf("New: %v", err)
					return
				}
				mrb.DefineModuleFunction(mrb.KernelModule(), "linkrefs_callback", func(mrb *MrbState, self Value) MrbValue {
					return mrb.Value(42)
				}, ArgsNone())
				value, err := mrb.Eval("linkrefs_callback")
				if err != nil {
					t.Errorf("Eval: %v", err)
				} else if !value.Value().IsInteger() || MrbFixnum(value) != 42 {
					t.Errorf("callback result = %v, want 42", value)
				}
				mrb.Close()
			}
		}()
	}
	close(start)
	wg.Wait()
}

func TestLinkrefsCallbacksDuringRegistryGrowth(t *testing.T) {
	ready := make(chan struct{})
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		mrb, err := New()
		if err != nil {
			t.Errorf("New: %v", err)
			close(ready)
			return
		}
		defer mrb.Close()
		mrb.DefineModuleFunction(mrb.KernelModule(), "linkrefs_callback", func(mrb *MrbState, self Value) MrbValue {
			return mrb.Value(42)
		}, ArgsNone())
		if _, err := mrb.Eval("linkrefs_callback"); err != nil {
			t.Errorf("Eval: %v", err)
			close(ready)
			return
		}
		close(ready)
		for {
			select {
			case <-done:
				return
			default:
				if _, err := mrb.Eval("linkrefs_callback"); err != nil {
					t.Errorf("Eval: %v", err)
					return
				}
			}
		}
	}()
	<-ready
	mu.Lock()
	count := len(states) + 16
	mu.Unlock()
	var opened []*MrbState
	for i := 0; i < count; i++ {
		mrb, err := New()
		if err != nil {
			t.Errorf("New: %v", err)
			break
		}
		opened = append(opened, mrb)
	}
	close(done)
	wg.Wait()
	for _, mrb := range opened {
		mrb.Close()
	}
}
