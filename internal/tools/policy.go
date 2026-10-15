package tools

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/avhn/trello-mcp/internal/credentials"
	"github.com/avhn/trello-mcp/internal/view"
)

// selected applies the same profile union to registration and policy diagnostics.
func (s *Service) selected(d definition) bool {
	selectors := s.config.Tools
	return len(selectors) == 0 || slices.Contains(selectors, "all") || slices.Contains(selectors, d.name) || slices.Contains(selectors, strings.SplitN(d.name, "_", 2)[0])
}

func (s *Service) enabled(name string) bool {
	if !s.config.HasCredentials() {
		return name == "server_health"
	}
	for _, d := range s.definitions() {
		if d.name == name {
			return s.selected(d) && (!s.config.ReadOnly || !d.write)
		}
	}
	return false
}

func (s *Service) downloadDir() string {
	if s.config.DownloadDir != "" {
		return s.config.DownloadDir
	}
	return filepath.Join(os.TempDir(), "trello-mcp")
}

// Instructions describes startup policy without fetching or exposing saved selections.
func (s *Service) Instructions() string {
	if !s.config.HasCredentials() {
		return credentials.Hint
	}
	workspaces := "unrestricted"
	if len(s.config.AllowedWorkspaces) > 0 {
		workspaces = "restricted"
	}
	uploads := fileTransfersSupported && s.config.UploadDir != "" && s.enabled("attachment_upload")
	downloads := fileTransfersSupported && s.enabled("attachment_download")
	return fmt.Sprintf("Workspaces: %s; read-only: %t; path uploads: %t; downloads: %t. See server_health detail \"policy\" for access restrictions.", workspaces, s.config.ReadOnly, uploads, downloads)
}

type allowedWorkspace struct {
	Selector string `json:"selector"`
	ID       string `json:"id,omitempty"`
	Name     string `json:"name,omitempty"`
}

type transferPolicy struct {
	Enabled bool   `json:"enabled"`
	Dir     string `json:"dir"`
}

// policy resolves configured selectors once, best effort. This cache never grants access.
func (s *Service) policy(ctx context.Context) any {
	s.policyOnce.Do(func() {
		if len(s.config.AllowedWorkspaces) == 0 {
			return
		}
		var all []view.Workspace
		if err := s.access.Client.Get(ctx, "/members/me/organizations", url.Values{"fields": {"id,name"}}, &all); err != nil {
			return
		}
		for _, w := range all {
			if s.access.WorkspaceAllowed(w) {
				s.policyWorkspaces = append(s.policyWorkspaces, w)
			}
		}
	})
	allowed := make([]allowedWorkspace, 0, len(s.config.AllowedWorkspaces))
	for _, selector := range s.config.AllowedWorkspaces {
		entry := allowedWorkspace{Selector: s.access.Client.Scrub(selector)}
		for _, w := range s.policyWorkspaces {
			if selector == w.ID || selector == w.Name {
				entry.ID, entry.Name = s.access.Client.Scrub(w.ID), s.access.Client.Scrub(w.Name)
				break
			}
		}
		allowed = append(allowed, entry)
	}
	enabled, hidden := 0, 0
	for _, d := range s.definitions() {
		if !s.selected(d) {
			continue
		}
		if s.config.ReadOnly && d.write {
			hidden++
		} else {
			enabled++
		}
	}
	profile := make([]string, 0, len(s.config.Tools))
	for _, selector := range s.config.Tools {
		profile = append(profile, s.access.Client.Scrub(selector))
	}
	if len(profile) == 0 {
		profile = append(profile, "all")
	}
	return struct {
		Restricted        bool               `json:"restricted"`
		AllowedWorkspaces []allowedWorkspace `json:"allowedWorkspaces"`
		ReadOnly          bool               `json:"readOnly"`
		CredentialScope   string             `json:"credentialScope,omitempty"`
		ToolProfile       []string           `json:"toolProfile"`
		EnabledToolCount  int                `json:"enabledToolCount"`
		HiddenWriteTools  int                `json:"hiddenWriteTools"`
		Uploads           transferPolicy     `json:"uploads"`
		Downloads         transferPolicy     `json:"downloads"`
	}{len(allowed) > 0, allowed, s.config.ReadOnly, s.config.CredentialScope, profile, enabled, hidden,
		transferPolicy{fileTransfersSupported && s.config.UploadDir != "" && s.enabled("attachment_upload"), s.access.Client.Scrub(s.config.UploadDir)},
		transferPolicy{fileTransfersSupported && s.enabled("attachment_download"), s.access.Client.Scrub(s.downloadDir())}}
}
