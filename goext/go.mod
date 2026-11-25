module github.com/oruby/oruby/goext

go 1.27.0

require (
	github.com/andybalholm/brotli v1.3.0
	github.com/andybalholm/cascadia v1.3.5
	github.com/antchfx/xpath v1.3.9
	github.com/fxamacker/cbor/v2 v2.10.0
	github.com/go-sql-driver/mysql v1.10.2
	github.com/jackc/pgx/v5 v5.13.0
	github.com/jacoelho/xsd v0.6.14
	github.com/klauspost/compress v1.20.5
	github.com/mattn/go-sqlite3 v1.14.16
	github.com/oruby/oruby v0.0.0
	github.com/pierrec/lz4/v4 v4.1.34
	github.com/tim-riep/go-xslt v0.4.3
	github.com/yuin/goldmark v1.8.7
	golang.org/x/crypto v0.59.0
	golang.org/x/image v0.48.0
	golang.org/x/net v0.63.0
	golang.org/x/sys v0.50.0
	golang.org/x/text v0.44.0
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/creack/pty v1.1.24 // indirect
	github.com/dlclark/regexp2/v2 v2.13.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	go.yaml.in/yaml/v3 v3.0.6 // indirect
	software.sslmate.com/src/go-pkcs12 v0.7.4 // indirect
)

replace github.com/oruby/oruby => ..
