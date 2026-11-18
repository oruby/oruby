package file

// fileID: no inode numbers here; "***/" follows symlinks without the cycle
// check
type fileID struct{ dev, ino uint64 }

func statID(string) (fileID, bool) { return fileID{}, false }
