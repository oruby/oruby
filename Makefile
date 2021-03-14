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

all : libmruby.a $(DYN_LIB)
	ruby scripts/check_api.rb
	go build .

test:
	go test
	(cd gem/assert && go test)
	(cd gem/base64 && go test)
	(cd gem/complex && go test)
	(cd gem/database && go test)
	(cd gem/env && go test)
	(cd gem/fullcore && go test)
	(cd gem/io && go test)
	(cd gem/io/file && go test)
	(cd gem/io/popen && go test)
	(cd gem/json && go test)
	(cd gem/load && go test)
	(cd gem/process && go test)
	(cd gem/regexp && go test)
#	(cd gem/signal && go test)
	(cd gem/thread && go test)
	(cd gem/time && go test)
	(cd gem/zlib && go test)

libmruby.a :
	(cd mruby && $(MAKE))

pull :
	git submodule init
	git submodule update --remote

check :
	ruby scripts/check_api.rb

update : clean pull libmruby.a $(DYN_LIB)

mruby.dll : libmruby.a # mruby.def
	mkdir -p build
	windres scripts\mrubyver.rc build\mrubyver.o
	dlltool -z scripts\mruby.def libmruby.a
	gcc -shared -static -dll -Wl,--export-all-symbols -o mruby.dll build\mruby.def libmruby.a build/mrubyver.o
	dlltool -D mruby.dll -d build\mruby.def -l libmruby.dll.a

install: mruby.dll
	$(COPY) $(DYN_LIB) $(GOBIN)

clean :
	(cd mruby && $(MAKE) clean)
#	$(RM) mrubyver.o mruby.def mruby.dll
	go clean .
	go clean --cache
