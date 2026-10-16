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

// checkDirectory is the Unix directory rule for every read and write. Only
// the default ~/.secrets directory requires 0700; auth repairs it after checking
// ownership. Custom parents and the shared home store permit read/search bits,
// but never group/world write bits. Files always forbid all group/world bits.
func checkDirectory(file *os.File, info os.FileInfo, rule directoryRule, repair bool) error {
	if err := checkOwner(info); err != nil {
		return err
	}
	if rule == privateDirectory && repair {
		if err := file.Chmod(0700); err != nil {
			return fmt.Errorf("make credentials directory private; chmod 700 ~/.secrets or rerun the installer's absolute-path auth command")
		}
		return nil
	}
	mask := os.FileMode(0022)
	if rule == privateDirectory {
		mask = 0077
	}
	return checkPermissions(info, mask, true)
}

func checkOwner(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("credentials must be owned by the current user; rerun the installer's absolute-path auth command as that user")
	}
	return nil
}

func checkPrivate(info os.FileInfo, directory bool) error {
	if err := checkOwner(info); err != nil {
		return err
	}
	return checkPermissions(info, 0077, directory)
}

func checkPermissions(info os.FileInfo, mask os.FileMode, directory bool) error {
	if info.Mode().Perm()&mask != 0 {
		if directory {
			if mask == 0077 {
				return fmt.Errorf("credentials directory is group/world accessible; chmod 700 ~/.secrets or rerun the installer's absolute-path auth command")
			}
			return fmt.Errorf("credentials directory is group/world writable; remove group/world write permissions (chmod go-w) or choose a private directory with TRELLO_MCP_CREDENTIALS")
		}
		return fmt.Errorf("credentials file is group/world accessible; chmod 600 the credentials file or rerun the installer's absolute-path auth command")
	}
	return nil
}
