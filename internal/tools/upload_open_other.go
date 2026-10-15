//go:build !unix

package tools

import (
	"fmt"
	"os"
)

func openUpload(_, _ string, _ os.FileInfo) (*os.File, error) {
	return nil, fmt.Errorf("upload file path verification is unavailable on this platform")
}
