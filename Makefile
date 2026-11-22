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

# bin/ next to lib/oruby is the install layout, so bin/oruby runs in-tree.
# scripts/gen_prism.rb writes Ruby::Prism and Ripper into lib/oruby/4.1.0
# (generated from mruby's vendored Prism, ignored by git).
bin : libmruby.a
	ruby scripts/gen_prism.rb
	go build -o bin/oruby ./cmd/oruby
	go build -o bin/orbi ./cmd/orbi
	go build -o bin/orubyc ./cmd/orubyc

PREFIX ?= /usr/local

install : bin
	install -d $(PREFIX)/bin $(PREFIX)/lib
	install bin/oruby bin/orbi bin/orubyc $(PREFIX)/bin
	cp -R lib/oruby $(PREFIX)/lib/

test:
	go test
	(cd compiler && go test)
	(cd gem/assert && go test)
	(cd gem/base64 && go test)
	(cd gem/bcrypt && go test)
	(cd gem/bcrypt_pbkdf && go test)
	(cd gem/bigint && go test)
	(cd gem/core_ext && go test)
	(cd gem/coverage && go test)
	(cd gem/database && go test)
	(cd gem/ed25519 && go test)
	(cd gem/env && go test)
	(cd gem/ffi && go test)
	(cd gem/fiddle && go test)
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
	(cd gem/objspace && go test)
	(cd gem/process && go test)
	(cd gem/pty && go test)
	(cd gem/regexp && go test)
	(cd gem/regexp/full && go test)
#	(cd gem/signal && go test)
	(cd gem/sqlite3 && go test)
	(cd gem/thread && go test)
	(cd gem/time && go test)
	(cd gem/websocket && go test)
	(cd gem/yaml && go test)
	(cd gem/transcode && go test)
	(cd gem/zlib && go test)
	(cd gem/zlibgo && go test)

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
