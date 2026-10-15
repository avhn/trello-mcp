//go:build !unix

package tools

import (
	"fmt"
	"os"
)

func checkDownloadOwner(_ os.FileInfo) error {
	return fmt.Errorf("download directory ownership verification is unavailable on this platform")
}
