//go:build windows

package auth

import (
	"testing"

	"github.com/avhn/trello-mcp/internal/credentials"
)

func TestWindowsInputTerminators(t *testing.T) {
	for _, line := range []string{"synthetic-token\r\n", "synthetic-token\n", "synthetic-token"} {
		if inputLine(line) != "synthetic-token" {
			t.Fatal("input terminator became part of the credential")
		}
	}
	for _, line := range []string{" synthetic-token\r\n", "synthetic-token \r\n", "synthetic\rtoken\r\n", "synthetic-token\r"} {
		if credentials.ValidValue(inputLine(line)) {
			t.Fatal("input normalization hid invalid credential bytes")
		}
	}
}
