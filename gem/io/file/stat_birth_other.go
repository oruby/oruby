//go:build !linux

package file

import (
	"os"
	"time"
)

func withBirth(fi os.FileInfo, err error, _ string, _ bool) (os.FileInfo, error) { return fi, err }

func birthtimeOf(_ os.FileInfo, ext extendedStat) time.Time { return ext.birthtime }

func bareInfo(fi os.FileInfo) os.FileInfo { return fi }
