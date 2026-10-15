//go:build !unix && !windows

package credentials

import (
	"fmt"
	"os"
)

const readFlags = os.O_RDONLY

func privacyUnsupported() error {
	return fmt.Errorf("private credential storage is unsupported on this platform; use TRELLO_API_KEY and TRELLO_TOKEN instead")
}

func makePrivateDir(string) error { return privacyUnsupported() }

func createPrivateFile(*os.Root, string) (*os.File, error) {
	return nil, privacyUnsupported()
}

func checkPrivateFile(*os.File, os.FileInfo, bool) error { return privacyUnsupported() }

func checkDirectory(*os.File, os.FileInfo, directoryRule, bool) error { return privacyUnsupported() }
