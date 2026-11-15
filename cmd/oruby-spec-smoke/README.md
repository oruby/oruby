### Real ruby/spec smoke runner

Prerequisites: Go and a built mruby library (see the repository's build instructions), CRuby, and a suite directory containing `ruby/` and `mspec/` checkouts. Run from the repository root:

```sh
go run ./cmd/oruby-spec-smoke -suite ../oruby_test/spec
go run ./cmd/oruby-spec-smoke -suite ../oruby_test/spec library/base64/strict_encode64_spec.rb
```

Samples are relative to `ruby/`. Without arguments the runner selects base64 strict encoding, language while, and YAML dump when present. `-timeout` bounds each execution (default `60s`); `-build-timeout` bounds each build (default `3m`). On Unix timeouts kill the command's process group. Other platforms only terminate the immediate command.

The runner builds `cmd/oruby` and `cmd/oruby-compiler` once. For each sample it runs the real mspec launcher with CRuby, targeting ORuby and setting `RUBY_EXE`, then compiles the spec entry using `-build -bundle` and executes the binary directly. The latter is **not** an mspec-launched run: a successful binary exit alone does not establish that examples ran. Full command output, warnings, exit status, timeouts, and any complete mspec tally are printed. Missing tallies are `unknown`; skipped compiler dependencies prevent a pass. Exit 0 requires successful commands and nonempty, failure-free tallies for both modes; exit 1 denotes failures or unknown results, and exit 2 denotes invalid setup or arguments.

The report includes the Go and CRuby versions, the SHA-256 of each selected spec and mspec launcher script, and a deterministic manifest of all regular `.rb` files in each suite tree. The manifest consists of `hex-sha256`, two spaces, slash-separated relative path, and newline per file, in lexical walk order; `.git` directories and symlinked directories are not traversed, and non-regular `.rb` files cause an error. Revision and dirty status are reported for the ORuby, ruby/spec, and mspec Git checkout roots when available. After building, the runner also reports the SHA-256 and byte size of the exact `oruby` and `oruby-compiler` executables used by the run. The report still does not capture non-Ruby fixtures, other installed tools, or the full environment; retain those separately when comparing runs. Temporary files are created under this directory and removed after normal completion.