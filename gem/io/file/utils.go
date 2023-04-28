package file

import (
	"os"

	"github.com/oruby/oruby"
)

func modestrToFlags(mode string) (int, error) {
	if mode == "" {
		return 0, nil
	}

	flags := 0
	switch mode[0] {
	case 'r':
		flags = os.O_RDONLY
	case 'w':
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	case 'a':
		flags = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	default:
		return 0, oruby.EArgumentError("illegal access mode %v", mode)
	}

	if len(mode) == 1 {
		return flags, nil
	}

	for _, m := range mode[1:] {
		switch m {
		case 'b':
			//flags |= os.O_BINARY
		case 't':
			//flags |= os.O_TEXT
		case '+':
			flags = (flags & ^(os.O_RDONLY | os.O_WRONLY | os.O_RDWR)) | os.O_RDWR
		case 'x':
			if mode[0] != 'w' {
				return 0, oruby.EArgumentError("illegal access mode %v", mode)
			}
			flags |= os.O_EXCL
		case ':':
			// ignore BOM
			goto end
		default:
			return 0, oruby.EArgumentError("illegal access mode %v", mode)
		}
	}

end:
	return flags, nil
}

func modeToFlags(mrb *oruby.MrbState, mode oruby.Value) (int, error) {
	if mode.IsNil() {
		// Default is RDONLY
		return 0, nil

	} else if mode.IsString() {
		// mode string: 'rw+'
		return modestrToFlags(mode.String())

	} else if mode.IsInteger() {
		// mode integer: File::RDONLY|File::EXCL
		return mode.Int(), nil

	} else if mode.IsHash() {
		// mode: 'rw+', flags: File::EXCL
		optMode := mrb.HashFetch(mode, mrb.Intern("mode"), mrb.NilValue())
		mFlags, err := modestrToFlags(optMode.String())
		if err != nil {
			return 0, err
		}

		fFlags := mrb.HashFetch(mode, mrb.Intern("flags"), oruby.Int(0)).Int()
		return mFlags | fFlags, nil

	} else {
		return 0, oruby.EArgumentError("illegal access mode %v", mrb.Inspect(mode))
	}
}

func parseFlags(mrb *oruby.MrbState, mode, optHash oruby.Value) (int, error) {
	flags, err := modeToFlags(mrb, mode)
	if err != nil {
		return 0, err
	}

	flagsOpt, err := modeToFlags(mrb, optHash)
	if err != nil {
		return 0, err
	}

	return flags | flagsOpt, nil
}
