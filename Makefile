ifeq ($(OS),Windows_NT)
	export MRUBY_CONFIG:=..\scripts\build_config_win.rb
	DYN_LIB=mruby.dll
	MAKE=mingw32-make
	COPY=xcopy /Y
	GOBIN=$(GOPATH)\bin
	RM=del -f -Q
else
	UNAME_S := $(shell uname -s)
	ifeq ($(UNAME_S),Darwin)
		export MRUBY_CONFIG:=../scripts/build_config_darwin.rb
	else
		export MRUBY_CONFIG:=../scripts/build_config_unix.rb
	endif
	MAKE=make
	DYN_LIB=
	COPY=\cp
	GOBIN=$(GOPATH)/bin
	RM=rm
endif

all : libmruby.a $(DYN_LIB) bin
	ruby scripts/check_api.rb
	go build .

# bin/ next to lib/oruby is the install layout, so bin/oruby runs in-tree;
# gem, bundle, irb, ... beside it are RubyGems stubs (scripts/gen_bin_stubs.rb).
# scripts/gen_prism.rb writes Ruby::Prism and Ripper into lib/oruby/4.1.0
# (generated from mruby's vendored Prism, ignored by git).
# lib/liboruby-capi.a and lib/libmruby.a: what mkmf's link tests link with
# (RbConfig LIBRUBYARG), the Ruby C API (gem/capi) and libmruby as static
# libraries; extensions themselves are compiled into an oruby binary by
# `oruby build` (cmd/orubyc).
CAPI_CFLAGS = -Igem/capi/include -I. -Imruby/include -Imruby/build/host/include -DMRB_INT64 -DMRB_HIGH_PROFILE -DMRB_NO_BOXING -DMRB_SYMBOL_MAX=0 -DMRB_USE_DEBUG_HOOK -DHAVE_MRUBY_ENCODING_GEM -DMRB_UTF8_STRING -DMRB_USE_RATIONAL -DMRB_USE_SET -DMRB_USE_COERCE -DMRB_USE_BIGINT -DMRB_USE_COMPLEX -DMRB_USE_REFINEMENTS -DMRB_USE_GC_WEAK

capi-lib : libmruby.a
	cc -O2 -c $(CAPI_CFLAGS) -o mruby/build/host/oruby-capi.o gem/capi/capi.c
	ar rcs lib/liboruby-capi.a mruby/build/host/oruby-capi.o
	$(COPY) mruby/build/host/lib/libmruby.a lib/libmruby.a

bin : libmruby.a capi-lib
	ruby scripts/gen_prism.rb
	go build -o bin/oruby ./cmd/oruby
	go build -o bin/orbi ./cmd/orbi
	go build -o bin/orubyc ./cmd/orubyc
	bin/oruby scripts/gen_bin_stubs.rb bin

PREFIX ?= /usr/local

install : bin
	install -d $(PREFIX)/bin $(PREFIX)/lib
	install bin/oruby bin/orbi bin/orubyc $(PREFIX)/bin
	cp -R lib/oruby $(PREFIX)/lib/
	$(PREFIX)/bin/oruby scripts/gen_bin_stubs.rb $(PREFIX)/bin
	install -m 644 lib/liboruby-capi.a lib/libmruby.a $(PREFIX)/lib
	install -d $(PREFIX)/include/oruby-4.1.0
	cp -R gem/capi/include/ $(PREFIX)/include/oruby-4.1.0/

test:
	go test
	(cd cmd/orubyc && go test)
	(cd compiler && go test)
	(cd gem/argon2 && go test)
	(cd gem/assert && go test)
	(cd gem/base64 && go test)
	(cd gem/bcrypt && go test)
	(cd gem/bcrypt_pbkdf && go test)
	(cd gem/bigdecimal && go test)
	(cd gem/bigint && go test)
	(cd gem/brotli && go test)
	(cd gem/capi/testext && go test)
	(cd gem/commonmarker && go test)
	(cd gem/core_ext && go test)
	(cd gem/coverage && go test)
	(cd gem/database && go test)
	(cd gem/date && go test)
	(cd gem/digest && go test)
	(cd gem/ed25519 && go test)
	(cd gem/env && go test)
	(cd gem/erubi && go test)
	(cd gem/etc && go test)
	(cd gem/ffi && go test)
	(cd gem/fiddle && go test)
	(cd gem/hiredis && go test)
	(cd gem/fullcore && go test)
	(cd gem/stdlib && go test)
	(cd gem/http && go test)
	(cd gem/io && go test)
	(cd gem/io/console && go test)
	(cd gem/io/file && go test)
	(cd gem/io/popen && go test)
	(cd gem/isolated_worker && go test)
	(cd gem/json && go test)
	(cd gem/load && go test)
	(cd gem/marshal && go test)
	(cd gem/msgpack && go test)
	(cd gem/mysql2 && go test)
	(cd gem/nethttp && go test)
	(cd gem/nio4r && go test)
	(cd gem/nokogiri && go test)
	(cd gem/objspace && go test)
	(cd gem/oj && go test)
	(cd gem/oruby-cbor && go test)
	(cd gem/oruby-digest-crc && go test)
	(cd gem/oruby-escape_utils && go test)
	(cd gem/oruby-fast_blank && go test)
	(cd gem/oruby-lz4 && go test)
	(cd gem/oruby-ox && go test)
	(cd gem/oruby-redcarpet && go test)
	(cd gem/oruby-snappy && go test)
	(cd gem/oruby-vips && go test)
	(cd gem/oruby-xxhash && go test)
	(cd gem/oruby-yajl && go test)
	(cd gem/pg && go test)
	(cd gem/process && go test)
	(cd gem/pty && go test)
	(cd gem/puma_http11 && go test)
	(cd gem/racc && go test)
	(cd gem/rack && go test)
	(cd gem/rbnacl && go test)
	(cd gem/rbs && go test)
	(cd gem/regexp && go test)
	(cd gem/regexp/full && go test)
#	(cd gem/signal && go test)
	(cd gem/scrypt && go test)
	(cd gem/socket && go test)
	(cd gem/sqlite3 && go test)
	(cd gem/strscan && go test)
	(cd gem/thread && go test)
	(cd gem/time && go test)
	(cd gem/uri && go test)
	(cd gem/websocket && go test)
	(cd gem/yaml && go test)
	(cd gem/transcode && go test)
	(cd gem/zlib && go test)
	(cd gem/zlibgo && go test)
	(cd gem/zstd && go test)

# tools/gensym: the names oruby looks up become presyms (the mruby-oruby-presym
# gem), then Go constants for them (presym_gen.go) from the built table
libmruby.a :
	go run ./tools/gensym -c
	(cd mruby && $(MAKE))
	go run ./tools/gensym -go

pull :
	git submodule init
	git submodule update --remote
	(cd mruby && git submodule update --init)

check :
	ruby scripts/check_api.rb

update : clean pull libmruby.a $(DYN_LIB)

mruby.dll : libmruby.a # mruby.def
	mkdir -p build
	windres scripts\mrubyver.rc build\mrubyver.o
	dlltool -z scripts\mruby.def libmruby.a
	gcc -shared -static -dll -Wl,--export-all-symbols -o mruby.dll build\mruby.def libmruby.a build/mrubyver.o
	dlltool -D mruby.dll -d build\mruby.def -l libmruby.dll.a

install-dll: mruby.dll
	$(COPY) $(DYN_LIB) $(GOBIN)

clean :
	(cd mruby && $(MAKE) clean)
#	$(RM) mrubyver.o mruby.def mruby.dll
	go clean .
	go clean --cache
