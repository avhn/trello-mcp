//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package auth

import "golang.org/x/sys/unix"

const getTermios = unix.TIOCGETA
const setTermios = unix.TIOCSETA
