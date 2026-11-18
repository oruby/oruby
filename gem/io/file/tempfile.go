package file

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oruby/oruby"
)

// Tempfile as MRI 4.1's tempfile.rb (with tmpdir.rb's Dir::Tmpname.create),
// in Go. A Tempfile is a File subclass here (MRI's delegates to one): its
// stream is the *os.File of the file it created.
//
//   - the name: tmpdir/prefix + YYYYMMDD-pid-random + suffix (basename is a
//     prefix or [prefix, suffix]; characters other than ,-.0-9A-Za-z_~
//     dropped), a new one while the name exists
//   - opened mode | RDWR|CREAT|EXCL, perm 0600, the other options passed to
//     File#initialize (encodings ...)
//   - #open reopens without truncating, #path stays after #close (nil once
//     unlinked), #size works closed, #_close is protected
//   - Tempfile.create gives a plain File (removed after its block;
//     anonymous: unlinked at once)
//   - files not unlinked are removed when the program ends

const tmpUsable = ",-.0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ_abcdefghijklmnopqrstuvwxyz~"

func initTmpFile(mrb *oruby.MrbState, fileClass oruby.RClass) {
	tmp := mrb.DefineClass("Tempfile", fileClass)
	tmp.DefineClassMethod("create", tmpCreate, mrb.ArgsAny()|mrb.ArgsBlock())
	tmp.DefineClassMethod("open", tmpOpen, mrb.ArgsAny()|mrb.ArgsBlock())
	tmp.DefineMethod("initialize", tmpInit, mrb.ArgsAny()|mrb.ArgsBlock())
	tmp.DefineMethod("_close", tmpPrivateClose, mrb.ArgsNone())
	tmp.DefineMethod("close", tmpClose, mrb.ArgsOpt(1))
	tmp.DefineMethod("close!", tmpCloseBang, mrb.ArgsNone())
	tmp.DefineMethod("unlink", tmpUnlink, mrb.ArgsNone())
	tmp.DefineMethod("delete", tmpUnlink, mrb.ArgsNone())
	tmp.DefineMethod("open", tmpReopen, mrb.ArgsNone())
	tmp.DefineMethod("path", tmpPath, mrb.ArgsNone())
	tmp.DefineMethod("size", tmpSize, mrb.ArgsNone())
	tmp.DefineMethod("length", tmpSize, mrb.ArgsNone())
	tmp.DefineMethod("inspect", tmpInspect, mrb.ArgsNone())
	tmp.DefineMethod("to_s", tmpInspect, mrb.ArgsNone())
	_, _ = mrb.Funcall(tmp.Value(), mrb.Intern("protected"), mrb.SymbolValue(mrb.Intern("_close")))
}

// the tempfiles of each state not unlinked yet, removed at exit
var (
	tmpLiveMu sync.Mutex
	tmpLive   = map[*oruby.MrbState]map[string]bool{}
)

func tmpTrack(mrb *oruby.MrbState, path string) {
	tmpLiveMu.Lock()
	live, ok := tmpLive[mrb]
	if !ok {
		live = map[string]bool{}
		tmpLive[mrb] = live
	}
	live[path] = true
	tmpLiveMu.Unlock()
	if ok {
		return
	}
	// one at_exit handler per state (Kernel#at_exit's list)
	cleanup := mrb.ProcNewCFunc(func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		tmpLiveMu.Lock()
		paths := tmpLive[mrb]
		delete(tmpLive, mrb)
		tmpLiveMu.Unlock()
		for p := range paths {
			_ = os.Remove(p)
		}
		return mrb.NilValue()
	})
	list := mrb.GetGV("$__at_exit_list__")
	if list.IsNil() {
		l := mrb.AryNew()
		mrb.SetGV("$__at_exit_list__", l.Value())
		list = l.Value()
	}
	_, _ = mrb.Funcall(list, mrb.Intern("push"), cleanup.Value())
}

func tmpUntrack(mrb *oruby.MrbState, path string) {
	tmpLiveMu.Lock()
	defer tmpLiveMu.Unlock()
	delete(tmpLive[mrb], path)
}

// tmpAffix is String.try_convert of a prefix or suffix, its unusable
// characters dropped
func tmpAffix(mrb *oruby.MrbState, v oruby.Value, what string) (string, error) {
	if !v.IsString() {
		if v.IsNil() || !mrb.RespondTo(v, oruby.Sym_to_str) {
			return "", oruby.EArgumentError("unexpected %s: %s", what, mrb.Inspect(v))
		}
		s, err := mrb.Funcall(v, oruby.Sym_to_str)
		if err != nil {
			return "", err
		}
		v = s.Value()
	}
	b := []byte(mrb.String(v))
	out := make([]byte, 0, len(b))
	for _, c := range b {
		for i := 0; i < len(tmpUsable); i++ {
			if c == tmpUsable[i] {
				out = append(out, c)
				break
			}
		}
	}
	return string(out), nil
}

// tmpDir: the directory given (a path), or Dir.tmpdir
func tmpDir(mrb *oruby.MrbState, v oruby.Value) (string, error) {
	if !v.IsNil() {
		if !v.IsString() && !mrb.RespondTo(v, oruby.Sym_to_path) && !mrb.RespondTo(v, oruby.Sym_to_str) {
			return "", oruby.ETypeError("no implicit conversion of %s into String", mrb.ClassName(mrb.ObjClass(v)))
		}
		return pathString(mrb, v)
	}
	dir := mrb.ClassGet("Dir").Value()
	if !mrb.RespondTo(dir, mrb.Intern("tmpdir")) {
		_, _ = mrb.Resolve("tmpdir")
	}
	if mrb.RespondTo(dir, mrb.Intern("tmpdir")) {
		if d, err := mrb.Funcall(dir, mrb.Intern("tmpdir")); err == nil && d.IsString() {
			return mrb.String(d), nil
		}
		mrb.ExcClear()
	}
	return filepath.Clean(os.TempDir()), nil
}

// errExists: the path is there (a Go error or Errno::EEXIST from Ruby)
func errExists(err error) bool {
	if errors.Is(err, os.ErrExist) {
		return true
	}
	var re *oruby.RubyError
	if errors.As(err, &re) {
		return re.Class == "Errno::EEXIST"
	}
	return false
}

// tmpCreateFile is Dir::Tmpname.create with the open: open gets candidate
// paths until one is not there yet
func tmpCreateFile(mrb *oruby.MrbState, basename, tmpdirV oruby.Value, open func(path string) error) (string, error) {
	prefixV, suffixV := basename, mrb.NilValue()
	if basename.IsArray() {
		prefixV, suffixV = mrb.AryEntry(basename, 0), mrb.AryEntry(basename, 1)
	}
	prefix, err := tmpAffix(mrb, prefixV, "prefix")
	if err != nil {
		return "", err
	}
	suffix := ""
	if !suffixV.IsNil() {
		if suffix, err = tmpAffix(mrb, suffixV, "suffix"); err != nil {
			return "", err
		}
	}
	dir, err := tmpDir(mrb, tmpdirV)
	if err != nil {
		return "", err
	}
	t := time.Now().Format("20060102")
	for n := 0; n < 1000; n++ {
		name := prefix + t + "-" + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatUint(uint64(rand.Uint32()), 36)
		if n > 0 {
			name += "-" + strconv.Itoa(n)
		}
		// File.join: "." stays ("./name"), as MRI
		path := dir + "/" + name + suffix
		if strings.HasSuffix(dir, "/") {
			path = dir + name + suffix
		}
		err := open(path)
		if err == nil {
			return path, nil
		}
		if !errExists(err) {
			return "", err
		}
		mrb.ExcClear()
	}
	return "", fmt.Errorf("cannot generate temporary name using '%s' under '%s'", mrb.Inspect(basename), dir)
}

// tmpModeOpts: the Integer mode | RDWR|CREAT|EXCL and the other keyword
// options with perm: 0600 (MRI: a String mode fails on |)
func tmpModeOpts(mrb *oruby.MrbState, opts oruby.Value) (int, oruby.Value, error) {
	mode := 0
	o := mrb.HashNew().Value()
	if opts.IsHash() {
		o = mrb.HashDup(opts)
		m := mrb.HashDeleteKey(o, mrb.SymbolValue(oruby.Sym_mode))
		if !m.IsNil() {
			if !m.IsInteger() {
				return 0, o, oruby.ENoMethodError("undefined method '|' for an instance of %s", mrb.ClassName(mrb.ObjClass(m)))
			}
			mode = m.Int()
		}
	}
	mode |= os.O_RDWR | os.O_CREATE | os.O_EXCL
	mrb.HashSet(o, mrb.SymbolValue(oruby.Sym_perm), oruby.Int(0600))
	return mode, o, nil
}

// fileInitialize runs File#initialize on obj (the encodings and options of
// the Ruby side included)
func fileInitialize(mrb *oruby.MrbState, obj oruby.Value, args ...interface{}) error {
	init, err := mrb.Funcall(mrb.ClassGet("File").Value(), oruby.Sym_instance_method, mrb.SymbolValue(oruby.Sym_initialize))
	if err != nil {
		return err
	}
	_, err = mrb.Funcall(init, mrb.Intern("bind_call"), append([]interface{}{obj}, args...)...)
	return err
}

// tmpArgs: the positional arguments and the keyword Hash (a trailing Hash)
func tmpArgs(mrb *oruby.MrbState) (oruby.RArgs, oruby.Value, oruby.RProc) {
	args, block := mrb.GetArgsWithBlock()
	kw := mrb.KeywordArgs().Value()
	return args, kw, block
}

// Tempfile.new(basename = "", tmpdir = nil, mode: 0, **options)
func tmpInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args, kw, block := tmpArgs(mrb)
	if !block.IsNil() && mrb.RespondTo(mrb.KernelModule().Value(), oruby.Sym_warn) {
		_, _ = mrb.Funcall(mrb.KernelModule().Value(), oruby.Sym_warn, "warning: Tempfile.new doesn't call the given block.")
	}
	if err := tmpInitialize(mrb, self, args.Slice(), kw); err != nil {
		return mrb.RaiseError(err)
	}
	return self
}

func tmpInitialize(mrb *oruby.MrbState, self oruby.Value, args []oruby.Value, kw oruby.Value) error {
	if len(args) > 2 {
		return oruby.EArgumentError("wrong number of arguments (given %d, expected 0..2)", len(args))
	}
	basename := mrb.StrNew("").Value()
	tmpdirV := mrb.NilValue()
	if len(args) > 0 {
		basename = args[0]
	}
	if len(args) > 1 {
		tmpdirV = args[1]
	}
	mode, opts, err := tmpModeOpts(mrb, kw)
	if err != nil {
		return err
	}
	path, err := tmpCreateFile(mrb, basename, tmpdirV, func(p string) error {
		return fileInitialize(mrb, self, p, mode, opts)
	})
	if err != nil {
		return err
	}
	mrb.HashDeleteKey(opts, mrb.SymbolValue(oruby.Sym_perm))
	mrb.SetIV(self, "@tmp_mode", mode)
	mrb.SetIV(self, "@tmp_opts", opts)
	mrb.SetIV(self, "@tmp_path", path)
	mrb.SetIV(self, "@unlinked", false)
	tmpTrack(mrb, path)
	return nil
}

// Tempfile.open(*args, **kw): new; with a block, it is closed (not
// unlinked) after the block, whose value is returned
func tmpOpen(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args, kw, block := tmpArgs(mrb)
	// allocated and initialized here: the keywords stay keywords
	tv, err := mrb.Funcall(self, mrb.Intern("allocate"))
	if err != nil {
		return mrb.RaiseError(err)
	}
	t := tv.Value()
	if err := tmpInitialize(mrb, t, args.Slice(), kw); err != nil {
		return mrb.RaiseError(err)
	}
	if block.IsNil() {
		return t
	}
	result, err := mrb.YieldE(block, t)
	_, _ = mrb.Funcall(t.Value(), oruby.Sym_close)
	if err != nil {
		return mrb.RaiseError(err)
	}
	return result
}

// Tempfile.create(basename = "", tmpdir = nil, mode: 0, anonymous: false,
// **options): a File; with a block it is closed and removed after it
func tmpCreate(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args, kw, block := tmpArgs(mrb)
	if args.Len() > 2 {
		return mrb.ArgnumError(args.Len(), 0, 2)
	}
	basename := args.Item(0)
	if args.Len() == 0 {
		basename = mrb.StrNew("").Value()
	}
	anonymous := false
	if kw.IsHash() {
		kw = mrb.HashDup(kw)
		anonymous = mrb.HashDeleteKey(kw, mrb.SymbolValue(mrb.Intern("anonymous"))).Bool()
	}
	mode, opts, err := tmpModeOpts(mrb, kw)
	if err != nil {
		return mrb.RaiseError(err)
	}
	var file oruby.Value
	fileClass := mrb.ClassGet("File").Value()
	path, err := tmpCreateFile(mrb, basename, args.Item(1), func(p string) error {
		f, err := mrb.Funcall(fileClass, oruby.Sym_new, p, mode, opts)
		if err == nil {
			file = f.Value()
		}
		return err
	})
	if err != nil {
		return mrb.RaiseError(err)
	}
	if anonymous {
		// no name: unlinked now, its path the directory's
		_ = os.Remove(path)
		mrb.SetIV(file, "@__oruby_path", mrb.StrNew(filepath.Dir(path)+string(filepath.Separator)))
	}
	if block.IsNil() {
		return file
	}
	result, yerr := mrb.YieldE(block, file)
	if closed, err := mrb.Funcall(file, oruby.SymQ_closed); err == nil && !closed.Bool() {
		_, _ = mrb.Funcall(file, oruby.Sym_close)
	}
	if !anonymous {
		_ = os.Remove(path)
	}
	if yerr != nil {
		return mrb.RaiseError(yerr)
	}
	return result
}

func tmpFile(mrb *oruby.MrbState, self oruby.Value) (*os.File, bool) {
	f, ok := mrb.Data(self).(*os.File)
	return f, ok && f != nil
}

func tmpClosed(mrb *oruby.MrbState, self oruby.Value) bool {
	c, err := mrb.Funcall(self, oruby.SymQ_closed)
	return err == nil && c.Bool()
}

// _close (protected): the stream closes, the file stays
func tmpPrivateClose(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if !tmpClosed(mrb, self) {
		closeM, err := mrb.Funcall(mrb.ClassGet("IO").Value(), oruby.Sym_instance_method, mrb.SymbolValue(oruby.Sym_close))
		if err == nil {
			_, err = mrb.Funcall(closeM, mrb.Intern("bind_call"), self)
		}
		if err != nil {
			return mrb.RaiseError(err)
		}
	}
	return mrb.NilValue()
}

// close(unlink_now = false)
func tmpClose(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	unlink := mrb.GetArgsFirst().Bool()
	tmpPrivateClose(mrb, self)
	if unlink {
		tmpUnlink(mrb, self)
	}
	return mrb.NilValue()
}

func tmpCloseBang(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	tmpPrivateClose(mrb, self)
	tmpUnlink(mrb, self)
	return mrb.NilValue()
}

// open: reopened read-write, not truncated
func tmpReopen(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	tmpPrivateClose(mrb, self)
	mode := mrb.GetIV(self, "@tmp_mode").Int() &^ (os.O_CREATE | os.O_EXCL | os.O_TRUNC)
	opts := mrb.GetIV(self, "@tmp_opts")
	mrb.SetIV(self, "@closed", false)
	if err := fileInitialize(mrb, self, mrb.GetIV(self, "@tmp_path"), mode, opts); err != nil {
		return mrb.RaiseError(err)
	}
	return self
}

func tmpPath(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if mrb.GetIV(self, "@unlinked").Bool() {
		return mrb.NilValue()
	}
	p := mrb.GetIV(self, "@tmp_path")
	if p.IsNil() {
		if f, ok := tmpFile(mrb, self); ok {
			return mrb.StrNew(f.Name())
		}
	}
	return p
}

// size: of the open stream, or of the file once closed
func tmpSize(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if !tmpClosed(mrb, self) {
		if f, ok := tmpFile(mrb, self); ok {
			if st, err := f.Stat(); err == nil {
				return oruby.Int64(st.Size())
			}
		}
	}
	st, err := os.Stat(mrb.String(mrb.GetIV(self, "@tmp_path")))
	if err != nil {
		return mrb.SysFail(err)
	}
	return oruby.Int64(st.Size())
}

// unlink: the file goes (a missing one is fine); path is nil after
func tmpUnlink(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if mrb.GetIV(self, "@unlinked").Bool() {
		return mrb.NilValue()
	}
	p := mrb.GetIV(self, "@tmp_path")
	if !p.IsString() {
		return mrb.NilValue()
	}
	path := mrb.String(p)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		if errors.Is(err, os.ErrPermission) {
			return mrb.NilValue()
		}
		return mrb.SysFail(err)
	}
	tmpUntrack(mrb, path)
	mrb.SetIV(self, "@unlinked", true)
	return mrb.TrueValue()
}

func tmpInspect(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	name := mrb.ClassName(mrb.ObjClass(self))
	path := mrb.String(tmpPath(mrb, self).Value())
	if tmpClosed(mrb, self) {
		return mrb.StrNew(fmt.Sprintf("#<%s:%s (closed)>", name, path))
	}
	return mrb.StrNew(fmt.Sprintf("#<%s:%s>", name, path))
}
