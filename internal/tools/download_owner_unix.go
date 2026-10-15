//go:build unix

package tools

import (
	"os"
	"syscall"

	"github.com/avhn/trello-mcp/internal/trello"
)

const fileTransfersSupported = true

func checkDownloadOwner(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return trello.PolicyDenied("download directory safety", "download directory must be owned by the current user; choose a private TRELLO_DOWNLOAD_DIR")
	}
	return nil
}
