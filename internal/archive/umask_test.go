package archive_test

import "syscall"

func syscallUmask(mask int) int { return syscall.Umask(mask) }
