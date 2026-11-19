//go:build !unix && !windows

package tools

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestUnsupportedTransferPolicy(t *testing.T) {
	if fileTransfersSupported {
		t.Fatal("file transfers reported supported on an unverifiable platform")
	}
	_, err := (&Service{}).attachmentDownload(context.Background(), arguments{})
	if err == nil || !strings.Contains(err.Error(), "downloads unavailable on this platform") || !strings.Contains(err.Error(), "not a token/permission error") {
		t.Fatalf("wrong download refusal: %v", err)
	}
	if err := checkDownloadPrivate(nil, nil); err == nil || !strings.Contains(err.Error(), "access denied by download platform safety") {
		t.Fatalf("wrong owner refusal: %v", err)
	}
	_, err = openUpload("", "", nil)
	if err == nil || !strings.Contains(err.Error(), "access denied by upload platform safety") || !strings.Contains(err.Error(), "supply base64") {
		t.Fatalf("wrong upload refusal: %v", err)
	}
}

// Shared attachment tests still compile here; transfers themselves are refused.
func isPrivatePath(t *testing.T, path string, mode os.FileMode) bool {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm() == mode
}

func makePublicDir(dir string) error { return os.Mkdir(dir, 0755) }
