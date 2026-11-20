# Zlib in ORuby

ORuby's standard-library `require "zlib"` loads this gem. The native cgo
package compiles zlib 1.4 from source with zlib's `Z_PREFIX` option to keep
its symbols separate from other zlib copies in the process. It builds the
stream and checksum sources. Ruby handles the GzipFile IO API, so the
upstream file IO wrappers are unnecessary. No system libz is linked.

The upstream sources in `internal/native` are unmodified and come from the
[official zlib 1.4 archive](https://www.zlib.net/zlib-1.4.tar.gz). The archive's
SHA-256 is
`02dd159783677a749c04adbdd02b55b8a2bcc7e054f51b76b549199a916ad584`.
The upstream license is in `internal/native/LICENSE`.

The Go compression implementation is available separately at
`github.com/oruby/oruby/gem/zlibgo`. Import that package in the embedding
program to register the optional `zlibgo` gem, which exposes `ZlibGo`.
