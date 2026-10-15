//go:build !unix

package tools

import (
	"os"

	"github.com/avhn/trello-mcp/internal/trello"
)

const fileTransfersSupported = false

func checkDownloadOwner(_ os.FileInfo) error {
	return trello.PolicyDenied("download platform safety", "downloads unavailable on this platform because directory ownership cannot be verified; use a Unix host")
}
