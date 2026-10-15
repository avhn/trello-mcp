//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd

package auth

import "golang.org/x/sys/unix"

func (t fileTerminal) disableEcho() error {
	state, err := unix.IoctlGetTermios(t.fd, getTermios)
	if err != nil {
		return err
	}
	// Keep canonical input and signal handling; raw mode would disable Ctrl-C.
	state.Lflag &^= unix.ECHO | unix.ECHONL
	return unix.IoctlSetTermios(t.fd, setTermios, state)
}
