//go:build !unix

package tools

import (
	"os"

	"github.com/avhn/trello-mcp/internal/trello"
)

func openUpload(_, _ string, _ os.FileInfo) (*os.File, error) {
	return nil, trello.PolicyDenied("upload platform safety", "path uploads unavailable on this platform because file paths cannot be verified; supply base64 or use a Unix host")
}
