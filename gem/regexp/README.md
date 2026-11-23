# regexp gem

Ruby `Regexp`, `MatchData`, and the regexp-aware `String` methods (`=~`,
`match`, `match?`, `[]`, `slice`, `slice!`, `[]=`, `index`, `rindex`,
`byteindex`, `byterindex`, `sub`, `gsub`, `scan`, `split`, `partition`,
`rpartition`, `start_with?`).

## Two engines

| Package | Engine | Imported by | Use |
|---|---|---|---|
| `gem/regexp` | Go `regexp` (RE2) | everything that needs regexps | embedded oruby in Go apps |
| `gem/regexp/full` | regexp2 and mruby's native regexp engine | `gem/stdlib` (standalone ORuby, compiler) | additional Ruby syntax |

Every pattern is first translated from Ruby (Onigmo) syntax. If RE2 can express
it exactly, RE2 runs it: linear time, safe for untrusted input. Only patterns
that need Ruby-only syntax go to the backtracking engine, and only when
`gem/regexp/full` is imported. Without it they raise `RegexpError` naming the
construct, for example `lookahead (?=...) requires the full regexp engine`.

Embedded users who import `gem/regexp` alone do not link regexp2.

### Ruby semantics that differ from RE2 / .NET, handled by the translator

- `^` and `$` are always line anchors; `/m` (and `(?m)`) means "dot matches newline".
- `\w`, `\d` and `\s` are ASCII, and `\s` includes `\v`; `\h` is a hex digit.
- POSIX classes like `[[:alpha:]]` and `\p{Alpha}` are Unicode.
- With named groups, plain `(...)` does not capture.
- `/x` comments and whitespace; `{,n}`; `a{2}+` is a repeat of a repeat.
- Inline option groups `(?imx-imx)` and `(?imx-imx:...)`.

### Needs the full engine

Lookahead and lookbehind, atomic groups `(?>...)`, possessive quantifiers,
backreferences `\1` and `\k<name>`, `\K`, `\G`, `\Z`, `\R`, `\X`, conditionals,
character class intersection `[a-z&&[^aeiou]]`, and repeat counts over 1000.
RE2 cannot start a search part-way through a string, so `match(re, pos)` with
`pos > 0` and a pattern using `^`, `\A` or `\b` also uses the full engine when
it is loaded.

Recursive subexpression calls (`\g<name>`) and the absent operator
(`(?~...)`) use the native engine in `gem/regexp/full`. It compiles from
the pinned mruby source and uses the same ORuby Regexp and MatchData classes.

Binary, Windows-31J and EUC-JP input is matched by encoded character, and
MatchData keeps the original bytes and offsets.

### Regexp.timeout

`Regexp.timeout=` sets the process-wide limit for matching, including existing
regexps. `Regexp.new(source, timeout: seconds)` sets an instance limit. Timed
RE2 and native searches check the deadline during matching. The regexp2
fallback also checks elapsed time, with its internal interruption resolution
limited by regexp2's clock. Exceeding a deadline or the backtracking limit
raises `Regexp::TimeoutError`.

## Tests

`go test ./gem/regexp/...`. The Ruby-level checks were compared with MRI 4.0.9.
