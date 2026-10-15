package auth

import "golang.org/x/sys/windows"

func (t fileTerminal) disableEcho() error {
	handle := windows.Handle(t.fd)
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return err
	}
	return windows.SetConsoleMode(handle, mode&^windows.ENABLE_ECHO_INPUT)
}
