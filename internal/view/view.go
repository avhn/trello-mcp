// Package view defines compact projections, independent of MCP and HTTP transport.
package view

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Page keeps collection envelopes stable, including an empty items array.
type Page[T any] struct {
	Items []T    `json:"items"`
	Next  string `json:"next,omitempty"`
}

// Collection normalizes nil slices and attaches an optional continuation cursor.
func Collection[T any](items []T, next string) Page[T] {
	if items == nil {
		items = []T{}
	}
	return Page[T]{items, next}
}

// Encode returns one compact JSON document without HTML expansion or a trailing newline.
func Encode(value any) (string, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}

// Workspace retains canonical and human-readable workspace names.
type Workspace struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
}

// Board excludes preferences, badges and organization objects.
type Board struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url,omitempty"`
	WorkspaceID string `json:"idOrganization,omitempty"`
	Closed      bool   `json:"closed,omitempty"`
	Desc        string `json:"desc,omitempty"`
}

// List identifies its board so collection results remain useful across boards.
type List struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	BoardID string  `json:"idBoard,omitempty"`
	Closed  bool    `json:"closed,omitempty"`
	Pos     float64 `json:"pos,omitempty"`
}

// Label keeps label identifiers for later write operations.
type Label struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

// Member retains only identifiers and names, never profile or contact data.
type Member struct {
	ID       string `json:"id"`
	Username string `json:"username,omitempty"`
	FullName string `json:"fullName,omitempty"`
}

// Sticker is an opt-in curated sticker view.
type Sticker struct {
	ID    string  `json:"id"`
	Image string  `json:"image,omitempty"`
	Top   float64 `json:"top,omitempty"`
	Left  float64 `json:"left,omitempty"`
}

// PluginData exposes opt-in plugin values without the surrounding Trello metadata.
type PluginData struct {
	ID       string `json:"id"`
	PluginID string `json:"idPlugin,omitempty"`
	Value    string `json:"value,omitempty"`
}

// FieldValue retains the Trello typed custom-field value, or a dropdown option id.
type FieldValue struct {
	IDCustomField string `json:"idCustomField"`
	IDValue       string `json:"idValue,omitempty"`
	Value         *Value `json:"value,omitempty"`
}

// Value models all supported custom-field scalar types without lossy coercion.
type Value struct {
	Text    string `json:"text,omitempty"`
	Number  string `json:"number,omitempty"`
	Date    string `json:"date,omitempty"`
	Checked string `json:"checked,omitempty"`
}

// Card is the curated full view; summary projection removes optional expansions.
type Card struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	URL           string       `json:"url,omitempty"`
	BoardID       string       `json:"idBoard,omitempty"`
	ListID        string       `json:"idList,omitempty"`
	Desc          string       `json:"desc,omitempty"`
	DescTruncated bool         `json:"descTruncated,omitempty"`
	Due           string       `json:"due,omitempty"`
	DueComplete   bool         `json:"dueComplete,omitempty"`
	Closed        bool         `json:"closed,omitempty"`
	Labels        []Label      `json:"labels,omitempty"`
	MemberIDs     []string     `json:"idMembers,omitempty"`
	Start         string       `json:"start,omitempty"`
	LastActivity  string       `json:"dateLastActivity,omitempty"`
	Checklists    []Checklist  `json:"checklists,omitempty"`
	Members       []Member     `json:"members,omitempty"`
	Stickers      []Sticker    `json:"stickers,omitempty"`
	PluginData    []PluginData `json:"pluginData,omitempty"`
	Votes         []string     `json:"idMembersVoted,omitempty"`
	CustomFields  []FieldValue `json:"customFieldItems,omitempty"`
}

// ProjectCard limits collection descriptions by Unicode characters, not UTF-8 bytes.
func ProjectCard(c Card, full, collection bool) Card {
	if !full {
		c.Start = ""
		c.LastActivity = ""
		c.Checklists = nil
		c.Members = nil
	}
	if collection && utf8.RuneCountInString(c.Desc) > 200 {
		c.Desc = string([]rune(c.Desc)[:200])
		c.DescTruncated = true
	}
	return c
}

// Markdown renders the same curated card view without introducing extra network calls.
func Markdown(c Card) string {
	text := fmt.Sprintf("# %s\n\n%s", c.Name, c.Desc)
	if c.URL != "" {
		text += "\n\n" + c.URL
	}
	if c.Due != "" {
		text += "\nDue: " + c.Due
	}
	for _, list := range c.Checklists {
		text += "\n\n## " + list.Name
		for _, item := range list.Items {
			mark := " "
			if item.State == "complete" {
				mark = "x"
			}
			text += "\n- [" + mark + "] " + item.Name
		}
	}
	return text
}

// Item retains checklist progress, ordering and optional assignee/due date.
type Item struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	State    string  `json:"state"`
	Pos      float64 `json:"pos,omitempty"`
	Due      string  `json:"due,omitempty"`
	MemberID string  `json:"idMember,omitempty"`
}

// Checklist keeps the board/card references needed to verify object ownership.
type Checklist struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BoardID string `json:"idBoard,omitempty"`
	CardID  string `json:"idCard,omitempty"`
	Items   []Item `json:"checkItems"`
}

// Attachment retains metadata only; uploaded-file URLs are removed before returning results.
type Attachment struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	FileName string `json:"fileName"`
	MimeType string `json:"mimeType"`
	Bytes    int64  `json:"bytes"`
	Date     string `json:"date"`
	IsUpload bool   `json:"isUpload"`
	URL      string `json:"url,omitempty"`
}

// ActionData projects common activity entities instead of returning arbitrary raw data.
type ActionData struct {
	Text       string   `json:"text,omitempty"`
	Card       *Named   `json:"card,omitempty"`
	List       *Named   `json:"list,omitempty"`
	Board      *Named   `json:"board,omitempty"`
	ListBefore *Named   `json:"listBefore,omitempty"`
	ListAfter  *Named   `json:"listAfter,omitempty"`
	Old        *Changes `json:"old,omitempty"`
}

// Named is a compact entity reference within an action.
type Named struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// Changes is a bounded set of previous card values for update actions.
type Changes struct {
	Name   string `json:"name,omitempty"`
	Desc   string `json:"desc,omitempty"`
	Closed *bool  `json:"closed,omitempty"`
	Due    string `json:"due,omitempty"`
	ListID string `json:"idList,omitempty"`
}

// Action preserves type, timestamp, actor and relevant changes for history and comments.
type Action struct {
	ID      string     `json:"id"`
	Type    string     `json:"type"`
	Date    string     `json:"date"`
	Data    ActionData `json:"data"`
	Creator *Member    `json:"memberCreator,omitempty"`
}

// Option includes dropdown ids, which callers need for customfield_set in phase 2.
type Option struct {
	ID    string  `json:"id"`
	Value Value   `json:"value"`
	Color string  `json:"color,omitempty"`
	Pos   float64 `json:"pos,omitempty"`
}

// CustomField preserves scalar type and dropdown option identity.
type CustomField struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Options []Option `json:"options,omitempty"`
}
