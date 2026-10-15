//go:build !linux && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd && !windows

package auth

import "fmt"

func (t fileTerminal) disableEcho() error {
	return fmt.Errorf("hidden terminal input unavailable on this platform")
}
