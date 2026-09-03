package tailer

import (
	"fmt"
	"os"
	"syscall"
)

// inodeOf returns the inode number for fi, used to detect log rotation
// (the kubelet rotates by renaming the old file away and creating a fresh
// one at the same path — same path, different inode).
func inodeOf(fi os.FileInfo) (uint64, error) {
	sys, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("inode detection unsupported on this platform")
	}
	return uint64(sys.Ino), nil
}
