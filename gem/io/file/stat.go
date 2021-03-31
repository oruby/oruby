package file

import (
	"fmt"
	"github.com/oruby/oruby"
	"os"
	"time"
)

func initFileStat(mrb *oruby.MrbState, fileClass oruby.RClass) {
	fileStat := mrb.DefineClassUnder(fileClass, "Stat", mrb.ObjectClass())
	fileStat.AttachType(os.Stat)
	fileStat.Include(mrb.ModuleGet("Comparable"))

	fileStat.DefineMethod("initialize", statInit, mrb.ArgsReq(1))
	fileStat.DefineMethod("<=>", statComp, mrb.ArgsNone())
	fileStat.DefineMethod("atime", statAtime, mrb.ArgsNone())
	fileStat.DefineMethod("birthtime", statBirthtime, mrb.ArgsNone())
	fileStat.DefineMethod("blksize", statBlksize, mrb.ArgsNone())
	fileStat.DefineMethod("blockdev?", statIsBlockdev, mrb.ArgsNone())
	fileStat.DefineMethod("blocks", statBlocks, mrb.ArgsNone())
	fileStat.DefineMethod("chardev?", statIsChardev, mrb.ArgsNone())
	fileStat.DefineMethod("ctime", statCtime, mrb.ArgsNone())
	fileStat.DefineMethod("dev", statDev, mrb.ArgsNone())
	fileStat.DefineMethod("dev_major", statDevMajor, mrb.ArgsNone())
	fileStat.DefineMethod("dev_minor", statDevMinor, mrb.ArgsNone())
	fileStat.DefineMethod("directory?", statIsDirectory, mrb.ArgsNone())
	fileStat.DefineMethod("empty?", statIsZero, mrb.ArgsNone())
	fileStat.DefineMethod("executable?", statIsExecutable, mrb.ArgsNone())
	fileStat.DefineMethod("executable_real?", statIsExecutableReal, mrb.ArgsNone())
	fileStat.DefineMethod("file?", statIsFile, mrb.ArgsNone())
	fileStat.DefineMethod("ftype", statFtype, mrb.ArgsNone())
	fileStat.DefineMethod("gid", statGid, mrb.ArgsNone())
	fileStat.DefineMethod("grpowned?", statIsGrpowned, mrb.ArgsNone())
	fileStat.DefineMethod("ino", statIno, mrb.ArgsNone())
	fileStat.DefineMethod("inspect", statInspect, mrb.ArgsNone())
	fileStat.DefineMethod("mode", statMode, mrb.ArgsNone())
	fileStat.DefineMethod("mtime", statMtime, mrb.ArgsNone())
	fileStat.DefineMethod("nlink", statNlink, mrb.ArgsNone())
	fileStat.DefineMethod("owned?", statIsOwned, mrb.ArgsNone())
	fileStat.DefineMethod("pipe?", statIsPipe, mrb.ArgsNone())
	fileStat.DefineMethod("rdev", statRdev, mrb.ArgsNone())
	fileStat.DefineMethod("rdev_major", statIsRdevMajor, mrb.ArgsNone())
	fileStat.DefineMethod("rdev_minor", statIsRdevMinor, mrb.ArgsNone())
	fileStat.DefineMethod("readable?", statIsReadable, mrb.ArgsNone())
	fileStat.DefineMethod("readable_real?", statIsReadableReal, mrb.ArgsNone())
	fileStat.DefineMethod("setgid?", statIsSetgid, mrb.ArgsNone())
	fileStat.DefineMethod("setuid?", statIsSetuid, mrb.ArgsNone())
	fileStat.DefineMethod("size", statSize, mrb.ArgsNone())
	fileStat.DefineMethod("size?", statSize, mrb.ArgsNone())
	fileStat.DefineMethod("socket?", statIsSocket, mrb.ArgsNone())
	fileStat.DefineMethod("sticky?", statIsSticky, mrb.ArgsNone())
	fileStat.DefineMethod("symlink?", statIsSymlink, mrb.ArgsNone())
	fileStat.DefineMethod("uid", statUid, mrb.ArgsNone())
	fileStat.DefineMethod("world_readable?", statIsWorldReadable, mrb.ArgsNone())
	fileStat.DefineMethod("world_writable?", statIsWorldWritable, mrb.ArgsNone())
	fileStat.DefineMethod("writable?", statIsWritable, mrb.ArgsNone())
	fileStat.DefineMethod("writable_real?", statIsWritableReal, mrb.ArgsNone())
	fileStat.DefineMethod("zero?", statIsZero, mrb.ArgsNone())
}

// extendedStat is for platform support to fill this struct using syscall
type extendedStat struct {
	ino uint64
	dev uint64
	nlink uint64
	uid int
	gid int
	rdev uint64
	blksize int64
	blocks int64
	atime time.Time
	ctime time.Time
	birthtime time.Time
}

func statFirst(mrb *oruby.MrbState) (os.FileInfo, error) {
	return getStat(mrb, mrb.GetArgsFirst())
}

func statInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	info, err := statFirst(mrb)
	if err != nil {
		return mrb.SysFail(err)
	}
	mrb.DataSetInterface(self, info)
	return self
}

func statComp(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	other, err := statFirst(mrb)
	if err != nil {
		return mrb.NilValue()
	}
	stat := mrb.Data(self).(os.FileInfo)

	if stat.ModTime().Before(other.ModTime()) {
		return oruby.Integer(1)
	}

	if stat.ModTime().After(other.ModTime()) {
		return oruby.Integer(-1)
	}

	return oruby.Integer(0)
}

func statAtime(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	ext := getExtendedStat(stat)
	if ext.atime.IsZero() {
		return mrb.NilValue()
	}
	return mrb.Value(ext.atime)
}

func statBirthtime(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	ext := getExtendedStat(stat)
	if ext.birthtime.IsZero() {
		return mrb.NilValue()
	}
	return mrb.Value(ext.birthtime)
}

func statBlksize(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	ext := getExtendedStat(stat)
	if ext.blksize == 0 {
		return mrb.NilValue()
	}
	return oruby.Int64(ext.blksize)
}

func statIsBlockdev(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	return oruby.Bool(stat.Mode()&os.ModeDevice != 0)
}

func statBlocks(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	ext := getExtendedStat(stat)
	if ext.blocks == 0 {
		return mrb.NilValue()
	}

	return oruby.Int64(ext.blocks)
}

func statIsChardev(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	return oruby.Bool(stat.Mode()&os.ModeCharDevice != 0)
}

func statCtime(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	ext := getExtendedStat(stat)
	if ext.ctime.IsZero() {
		return mrb.NilValue()
	}

	return mrb.Value(ext.ctime)
}

func statDev(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	ext := getExtendedStat(stat)

	return oruby.Int64(int64(ext.dev))
}

func statDevMajor(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	ext := getExtendedStat(stat)
	if ext.dev == 0 {
		return mrb.NilValue()
	}
	return mrb.Value(major(ext.dev))
}

func statDevMinor(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	ext := getExtendedStat(stat)
	if ext.dev == 0 {
		return mrb.NilValue()
	}
	return mrb.Value(minor(ext.dev))
}

func statIsDirectory(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	return oruby.Bool(stat.IsDir())
}

func statIsFile(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	return oruby.Bool(stat.Mode().IsRegular())
}

func statFtype(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	if stat.IsDir() {
		return mrb.StrNew("directory")
	}
	mode := stat.Mode()
	if mode&os.ModeCharDevice != 0 {
		return mrb.StrNew("characterSpecial")
	}
	if mode&os.ModeSymlink != 0 {
		return mrb.StrNew("link")
	}
	if mode&os.ModeSocket != 0 {
		return mrb.StrNew("socket")
	}
	if mode&os.ModeDevice != 0 {
		return mrb.StrNew("blockSpecial")
	}
	if mode&os.ModeNamedPipe != 0 {
		return mrb.StrNew("fifo")
	}

	if stat.Mode().IsRegular() {
		return mrb.StrNew("file")
	}
	return mrb.StrNew("unknown")
}

func statGid(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	ext := getExtendedStat(stat)
	return mrb.FixnumValue(ext.gid)
}

func statIno(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	ext := getExtendedStat(stat)
	return mrb.Value(ext.ino)
}

func statInspect(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	ext := getExtendedStat(stat)
	birthtime := ""
	if !ext.birthtime.IsZero() {
		birthtime = ", birthtime=" + ext.birthtime.Format(time.RubyDate)
	}

	return mrb.StrNew(fmt.Sprintf(
		"#<File::Stat dev=%x, ino=%d, mode=%o, nlink=%d, uid=%d, gid=%d, rdev=%x, " +
        "size=%d, blksize=%d, blocks=%d, atime=%v, mtime=%v, ctime=%v%v>",
		ext.dev,
		ext.ino,
		stat.Mode(),
		ext.nlink,
		ext.uid,
		ext.gid,
		ext.rdev,
		stat.Size(),
		ext.blksize,
		ext.blocks,
		ext.atime.Format(time.RubyDate),
		stat.ModTime().Format(time.RubyDate),
		ext.ctime.Format(time.RubyDate),
		birthtime,
	))
}

func statMode(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	return oruby.Integer(int(stat.Mode().Perm()))
}

func statMtime(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	return mrb.Value(stat.ModTime())
}

func statNlink(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	ext := getExtendedStat(stat)
	return mrb.Value(ext.nlink)
}

func statIsOwned(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	ext := getExtendedStat(stat)
	return oruby.Bool(os.Geteuid() == ext.uid)
}

func statIsPipe(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	return oruby.Bool(stat.Mode()&os.ModeNamedPipe != 0)
}

func statRdev(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	ext := getExtendedStat(stat)
	return mrb.Value(ext.rdev)
}

func statIsRdevMajor(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	ext := getExtendedStat(stat)
	if ext.rdev == 0 {
		return mrb.NilValue()
	}
	return mrb.Value(major(ext.rdev))
}

func statIsRdevMinor(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	ext := getExtendedStat(stat)
	if ext.rdev == 0 {
		return mrb.NilValue()
	}
	return mrb.Value(minor(ext.rdev))
}

func statIsReadable(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	ext := getExtendedStat(stat)
	euid := os.Geteuid()

	if euid == 0 {
		return oruby.True
	}

	perm := stat.Mode().Perm()

	if euid == ext.uid  {
		return oruby.Bool((perm&0400 != 0))
	}
	if os.Getegid() == ext.gid {
		return oruby.Bool((perm&0040 != 0))
	}

	return oruby.Bool(perm&0004 != 0)
}

func statIsReadableReal(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	ext := getExtendedStat(stat)
	uid := os.Getuid()

	if uid == 0 {
		return oruby.True
	}

	perm := stat.Mode().Perm()

	if uid == ext.uid  {
		return oruby.Bool((perm&0400 != 0))
	}
	if os.Getgid() == ext.gid {
		return oruby.Bool((perm&0040 != 0))
	}

	return oruby.Bool(perm&0004 != 0)
}

func statIsSetgid(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	return oruby.Bool(stat.Mode()&os.ModeSetgid != 0)
}

func statIsSetuid(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	return oruby.Bool(stat.Mode()&os.ModeSetuid != 0)
}

func statSize(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	return oruby.Int64(stat.Size())
}

func statIsSocket(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	return oruby.Bool(stat.Mode()&os.ModeSocket != 0)
}

func statIsSticky(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	return oruby.Bool(stat.Mode()&os.ModeSticky != 0)
}

func statIsSymlink(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	return oruby.Bool(stat.Mode()&os.ModeSymlink != 0)
}

func statUid(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	ext := getExtendedStat(stat)
	return mrb.Value(ext.uid)
}

func statIsWorldReadable(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	if stat.Mode().Perm()&0004 == 0 {
		return mrb.NilValue()
	}
	return oruby.Integer(int(stat.Mode().Perm()))
}

func statIsWorldWritable(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	if stat.Mode().Perm()&0002 == 0 {
		return mrb.NilValue()
	}
	return oruby.Integer(int(stat.Mode().Perm()))
}

func statIsWritable(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	ext := getExtendedStat(stat)
	euid := os.Geteuid()

	if euid == 0 {
		return oruby.True
	}

	perm := stat.Mode().Perm()

	if euid == ext.uid  {
		return oruby.Bool((perm&0200 != 0))
	}
	if os.Getegid() == ext.gid {
		return oruby.Bool((perm&0020 != 0))
	}

	return oruby.Bool(perm&0002 != 0)
}

func statIsWritableReal(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	ext := getExtendedStat(stat)
	uid := os.Getuid()

	if uid == 0 {
		return oruby.True
	}

	perm := stat.Mode().Perm()

	if uid == ext.uid  {
		return oruby.Bool((perm&0200 != 0))
	}
	if os.Getegid() == ext.gid {
		return oruby.Bool((perm&0020 != 0))
	}

	return oruby.Bool(perm&0002 != 0)
}

func statIsZero(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	stat := mrb.Data(self).(os.FileInfo)
	return oruby.Bool(stat.Size() == 0)
}
