# Isolated worker gem for ORuby (experimental)

An isolated worker runs a block in a separate mruby state and heap. It is not a
Ruby shared-heap thread: Ruby objects are copied across the boundary rather than
shared, and an `MrbState` must not be accessed concurrently from multiple
goroutines.

## Opt in

Blank-import the gem in Go and explicitly resolve it for each state:

```go
package main

import (
	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/isolated_worker"
)

func main() {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	if _, err := mrb.Resolve("isolated_worker"); err != nil {
		panic(err)
	}
	// Use mrb.Eval to run Ruby code in this state.
}
```

Alternatively, after blank-importing the gem, Ruby can use
`require "isolated_worker"` when `github.com/oruby/oruby/gem/load` is also
blank-imported. The gem is not loaded automatically by `fullcore`, and loading
it does not install Ruby `Thread` or `Process.fork`.

## API

```ruby
worker = ORuby::IsolatedWorker.new(21) { |n| n * 2 }
worker.join        # waits for completion and returns worker
worker.value       # waits if necessary and returns 42
```

`ORuby::IsolatedWorker.new(*args, &block)` copies the arguments and supported
block into the worker state. `join(timeout = nil)` waits for completion: it
returns the worker when completed, or `nil` if the timeout expires. A nil
timeout waits without a deadline. `value` waits for completion and returns the
copied result or propagates the worker failure in the caller's state.

## Transfer restrictions

Transfers are conservative for arguments, captured closure values, and results.
Supported values are `nil`, booleans, integers, floats, symbols, and plain
strings and arrays containing supported values. Strings and arrays must be plain
instances, not subclasses. Only restricted Ruby closures whose captured
environments can be copied safely are supported; native, strict, orphaned,
recursive, and aliased closures are rejected.

Aliases and cycles in transferred object graphs are rejected, as are hashes,
Go-backed data, and other unsupported objects. Rejection avoids sharing foreign
heap pointers or silently changing object identity. These restrictions also
apply when returning a result to the caller.

## Limits

Workers do not create an OS process or provide `fork`, a PID, process `wait`,
signals, or OS-level isolation. `IO.popen('-')` remains unsupported. Existing
clone machinery is retained only for future fork-like research, not as a
process or fork implementation.

Cancellation and kill are not supported. Uninterruptible native or CPU-bound
code can delay worker completion and parent shutdown; closing a state does not
guarantee immediate interruption of such work.