package localfs_test

import "syscall"

var (
	errNoSpace error = syscall.ENOSPC
	errIO      error = syscall.EIO
)
