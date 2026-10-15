//go:build unix

package credentials

import (
	"fmt"
	"os"
	"syscall"
)

const readFlags = os.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_NONBLOCK

func makePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create credentials directory")
	}
	return nil
}

func createPrivateFile(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
}

func checkPrivateFile(_ *os.File, info os.FileInfo, directory bool) error {
	return checkPrivate(info, directory)
}

func checkPrivate(info os.FileInfo, directory bool) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("credentials must be owned by the current user; rerun trello-mcp auth as that user")
	}
	mask := os.FileMode(0077)
	if directory {
		mask = 0022
	}
	if info.Mode().Perm()&mask != 0 {
		if directory {
			return fmt.Errorf("credentials directory is group/world writable; remove group/world write permissions (chmod go-w) or choose a private directory with TRELLO_MCP_CREDENTIALS")
		}
		return fmt.Errorf("credentials file is group/world accessible; chmod 600 the credentials file or rerun trello-mcp auth")
	}
	return nil
}
