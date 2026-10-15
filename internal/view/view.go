// Package view defines compact projections, independent of MCP and HTTP transport.
package view

import (
	"encoding/json"
	"fmt"
	"slices"
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
	Text       string         `json:"text,omitempty"`
	Card       *Named         `json:"card,omitempty"`
	List       *Named         `json:"list,omitempty"`
	Board      *Reference     `json:"board,omitempty"`
	ListBefore *Named         `json:"listBefore,omitempty"`
	ListAfter  *Named         `json:"listAfter,omitempty"`
	Old        map[string]any `json:"old,omitempty"`
	Checklist  *Named         `json:"checklist,omitempty"`
	CheckItem  *ActionItem    `json:"checkItem,omitempty"`
	Attachment *Named         `json:"attachment,omitempty"`
	Label      *Label         `json:"label,omitempty"`
	Member     *Actor         `json:"member,omitempty"`
}

// Reference retains only the identity of a related board.
type Reference struct {
	ID string `json:"id"`
}

// Actor excludes full names and all profile metadata from activity results.
type Actor struct {
	ID       string `json:"id"`
	Username string `json:"username,omitempty"`
}

// ActionItem retains checklist-item identity, name and completion state only.
type ActionItem struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	State string `json:"state,omitempty"`
}

// Named is a compact entity reference within an action.
type Named struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// Activity is a flat, type-specific action summary. Related lists, checklist
// items and actors need names, not repeated object ids or profile metadata.
// Card ids remain available on every action for follow-up calls.
type Activity struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Date         string         `json:"date"`
	By           string         `json:"by,omitempty"`
	CardID       string         `json:"cardId,omitempty"`
	Card         string         `json:"card,omitempty"`
	Text         string         `json:"text,omitempty"`
	List         string         `json:"list,omitempty"`
	ListBefore   string         `json:"listBefore,omitempty"`
	ListAfter    string         `json:"listAfter,omitempty"`
	BoardID      string         `json:"boardId,omitempty"`
	Old          map[string]any `json:"old,omitempty"`
	OldTruncated []string       `json:"oldTruncated,omitempty"`
	Changed      []string       `json:"changed,omitempty"`
	Checklist    string         `json:"checklist,omitempty"`
	Item         string         `json:"item,omitempty"`
	State        string         `json:"state,omitempty"`
	Attachment   *Named         `json:"attachment,omitempty"`
	Label        *Label         `json:"label,omitempty"`
	Member       string         `json:"member,omitempty"`
}

// actorName uses the id only when Trello has no username (for example a deleted member).
func actorName(actor *Actor) string {
	if actor == nil {
		return ""
	}
	if actor.Username != "" {
		return actor.Username
	}
	return actor.ID
}

func entityName(entity *Named) string {
	if entity == nil {
		return ""
	}
	return entity.Name
}

// ProjectAction removes redundant references and bounds old strings to 80 runes.
// Non-scalar previous values retain their field names in changed, never raw data.
// Comment text and entity names are preserved without additional truncation.
func ProjectAction(a Action, boardID string) Activity {
	d := a.Data
	out := Activity{
		ID: a.ID, Type: a.Type, Date: a.Date, By: actorName(a.Creator),
		Text: d.Text, Card: entityName(d.Card), List: entityName(d.List),
		ListBefore: entityName(d.ListBefore), ListAfter: entityName(d.ListAfter),
		Checklist: entityName(d.Checklist), Attachment: d.Attachment,
		Label: d.Label, Member: actorName(d.Member),
	}
	if out.By == "" {
		out.By = a.CreatorID
	}
	if d.Card != nil {
		out.CardID = d.Card.ID
	}
	if d.Board != nil && d.Board.ID != boardID {
		out.BoardID = d.Board.ID
	}
	if d.CheckItem != nil {
		out.Item, out.State = d.CheckItem.Name, d.CheckItem.State
	}
	if len(d.Old) > 0 {
		out.Old = make(map[string]any, len(d.Old))
	}
	for key, value := range d.Old {
		switch v := value.(type) {
		case string:
			if utf8.RuneCountInString(v) > 80 {
				v = string([]rune(v)[:80])
				out.OldTruncated = append(out.OldTruncated, key)
			}
			out.Old[key] = v
		case nil, bool, float64:
			out.Old[key] = v
		default:
			out.Changed = append(out.Changed, key)
		}
	}
	slices.Sort(out.OldTruncated)
	slices.Sort(out.Changed)
	return out
}

// ProjectActions emits a card's name on its first occurrence and whenever it
// changes within this page. Every page is independently readable, with no cache
// across requests; an omitted name means the last name for that card in this page.
func ProjectActions(actions []Action, boardID string) []Activity {
	out := make([]Activity, len(actions))
	names := make(map[string]string)
	for i, action := range actions {
		item := ProjectAction(action, boardID)
		if item.CardID != "" && item.Card != "" {
			if names[item.CardID] == item.Card {
				item.Card = ""
			} else {
				names[item.CardID] = item.Card
			}
		}
		out[i] = item
	}
	return out
}

// Action preserves type, timestamp, actor and relevant changes for history and comments.
type Action struct {
	ID        string     `json:"id"`
	Type      string     `json:"type"`
	Date      string     `json:"date"`
	Data      ActionData `json:"data"`
	Creator   *Actor     `json:"memberCreator,omitempty"`
	CreatorID string     `json:"idMemberCreator,omitempty"`
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
