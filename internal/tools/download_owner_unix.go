//go:build unix

package tools

import (
	"fmt"
	"os"
	"syscall"
)

func checkDownloadOwner(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("download directory must be owned by the current user")
	}
	return nil
}
