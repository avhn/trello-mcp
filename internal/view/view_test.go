package view

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestProjection checks Unicode-safe collection truncation and full single-card descriptions.
func TestProjection(t *testing.T) {
	c := Card{ID: "c1", Name: "A < B & C", Desc: strings.Repeat("界", 201), Start: "today", Members: []Member{{ID: "m1"}}}
	summary := ProjectCard(c, false, true)
	if utf8.RuneCountInString(summary.Desc) != 200 || !summary.DescTruncated || summary.Start != "" || summary.Members != nil {
		t.Fatalf("incorrect summary: %+v", summary)
	}
	full := ProjectCard(c, true, false)
	if full.Desc != c.Desc || full.DescTruncated || full.Start == "" {
		t.Error("full description changed")
	}
	encoded, err := Encode(Collection([]Card{summary}, "next"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encoded, "\n") || !strings.Contains(encoded, "A < B & C") {
		t.Errorf("not compact JSON: %s", encoded)
	}
	empty, err := Encode(Collection[Card](nil, ""))
	if err != nil || empty != `{"items":[]}` {
		t.Errorf("empty page: %s %v", empty, err)
	}
	if _, err = Encode(make(chan int)); err == nil {
		t.Error("unsupported value encoded")
	}
}

// TestCardSerializationOmission scopes zero-value omission to the requested card projection.
func TestCardSerializationOmission(t *testing.T) {
	for _, raw := range []string{
		`{"id":"c1","name":"Example"}`,
		`{"id":"c1","name":"Example","url":"","idBoard":"","idList":"","desc":"","descTruncated":false,"due":"","dueComplete":false,"closed":false,"labels":[],"idMembers":[],"start":"","dateLastActivity":"","checklists":[],"members":[],"stickers":[],"pluginData":[],"idMembersVoted":[],"customFieldItems":[]}`,
		`{"id":"c1","name":"Example","desc":null,"due":null,"dueComplete":null,"closed":null,"labels":null,"idMembers":null,"start":null,"checklists":null,"members":null,"stickers":null,"pluginData":null,"idMembersVoted":null,"customFieldItems":null}`,
	} {
		var card Card
		if err := json.Unmarshal([]byte(raw), &card); err != nil {
			t.Fatal(err)
		}
		for _, full := range []bool{false, true} {
			got, err := Encode(ProjectCard(card, full, false))
			if err != nil || got != `{"id":"c1","name":"Example"}` {
				t.Errorf("empty requested card fields (full=%v): %s, %v", full, got, err)
			}
		}
	}

	const populated = `{"id":"c1","name":"Example","url":"https://example.invalid/card","idBoard":"b1","idList":"l1","desc":"Description","descTruncated":true,"due":"2027-02-03T12:00:00Z","dueComplete":true,"closed":true,"labels":[{"id":"lb1","name":"Ready","color":"green"}],"idMembers":["m1"],"start":"2027-02-02T12:00:00Z","dateLastActivity":"2027-02-01T12:00:00Z","checklists":[{"id":"cl1","name":"Checks","checkItems":null}],"members":[{"id":"m1","username":"synthetic"}],"stickers":[{"id":"s1","image":"star"}],"pluginData":[{"id":"p1","idPlugin":"plugin1","value":"synthetic"}],"idMembersVoted":["m1"],"customFieldItems":[{"idCustomField":"f1","value":{"text":"Ready"}}]}`
	var card Card
	if err := json.Unmarshal([]byte(populated), &card); err != nil {
		t.Fatal(err)
	}
	for _, full := range []bool{false, true} {
		got, err := Encode(ProjectCard(card, full, false))
		if err != nil {
			t.Fatal(err)
		}
		var actual, want map[string]any
		if err = json.Unmarshal([]byte(got), &actual); err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal([]byte(populated), &want); err != nil {
			t.Fatal(err)
		}
		if !full {
			for _, key := range []string{"start", "dateLastActivity", "checklists", "members"} {
				delete(want, key)
			}
		}
		if !reflect.DeepEqual(actual, want) {
			t.Errorf("populated requested card fields (full=%v): got %s, want %v", full, got, want)
		}
	}
}

// TestActionProjection proves nested metadata cannot leak through activity entities.
func TestActionProjection(t *testing.T) {
	raw := `{"id":"a1","type":"updateCard","date":"today","memberCreator":{"id":"m1","username":"synthetic","fullName":"drop","avatarUrl":"drop"},"data":{"card":{"id":"c1","name":"Card","desc":"drop"},"list":{"id":"l1","name":"List","pos":1},"board":{"id":"b1","name":"drop","prefs":{}},"listBefore":{"id":"l1","name":"Before","pos":1},"listAfter":{"id":"l2","name":"After","pos":2},"checklist":{"id":"cl1","name":"Checklist","checkItems":[]},"checkItem":{"id":"ci1","name":"Item","state":"complete","pos":1},"attachment":{"id":"at1","name":"File","isUpload":true,"url":"drop","previews":[]},"label":{"id":"lb1","name":"Ready","color":"green","uses":42},"member":{"id":"m2","username":"teammate","fullName":"drop"},"old":{"closed":false,"due":null,"name":"","pos":0,"prefs":{"drop":true},"cover":{"color":"blue"}},"unknown":"drop"}}`
	var a Action
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		t.Fatal(err)
	}
	got, err := Encode(ProjectAction(a, "b1"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"a1","type":"updateCard","date":"today","by":"synthetic","cardId":"c1","card":"Card","listId":"l1","list":"List","listBeforeId":"l1","listBefore":"Before","listAfterId":"l2","listAfter":"After","old":{"closed":false,"due":null,"name":"","pos":0},"changed":["cover","prefs"],"checklist":"Checklist","item":"Item","state":"complete","attachment":{"id":"at1","name":"File"},"label":{"id":"lb1","name":"Ready","color":"green"},"member":"teammate"}`
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	other, err := Encode(ProjectAction(a, "b2"))
	if err != nil || !strings.Contains(other, `"boardId":"b1"`) || a.Data.Board == nil {
		t.Errorf("nonredundant board lost: %s %v", other, err)
	}
}

func TestActionTextAndOldValues(t *testing.T) {
	text := strings.Repeat("界", 2500)
	a := Action{Data: ActionData{Text: text, Old: map[string]any{"desc": text, "name": "before", "closed": false, "idList": "l1", "custom": "previous", "state": map[string]any{"drop": true}}}}
	projected := ProjectAction(a, "b1")
	if projected.Text != text {
		t.Error("comment text changed")
	}
	old := projected.Old
	if len(old) != 5 || old["closed"] != false || old["idList"] != "l1" || old["name"] != "before" || old["custom"] != "previous" || utf8.RuneCountInString(old["desc"].(string)) != 80 {
		t.Error("old changed values were not bounded")
	}
	if strings.Join(projected.OldTruncated, ",") != "desc" || strings.Join(projected.Changed, ",") != "state" || a.Data.Old["desc"] != text {
		t.Error("truncation or non-scalar change markers incorrect, or source changed")
	}
	encoded, err := Encode(projected)
	if err != nil || !utf8.ValidString(encoded) {
		t.Errorf("invalid Unicode projection: %v", err)
	}
}

func TestActionOldBoundaries(t *testing.T) {
	a := Action{Data: ActionData{Old: map[string]any{
		"empty": "", "limit": strings.Repeat("界", 80), "over": strings.Repeat("界", 81),
		"desc": strings.Repeat("é", 100), "null": nil, "false": false, "zero": float64(0),
		"array": []any{"not exposed"}, "object": map[string]any{"not": "exposed"},
	}}}
	for range 10 {
		got := ProjectAction(a, "")
		if got.Old["empty"] != "" || got.Old["limit"] != a.Data.Old["limit"] || got.Old["null"] != nil || got.Old["false"] != false || got.Old["zero"] != float64(0) {
			t.Fatal("empty, boundary, or default old values lost")
		}
		if len(got.Old) != 7 || strings.Join(got.OldTruncated, ",") != "desc,over" || strings.Join(got.Changed, ",") != "array,object" {
			t.Fatalf("incorrect or nondeterministic markers: %+v", got)
		}
	}
}

func TestActionPageNames(t *testing.T) {
	names := []string{"First", "First", "Renamed", "First"}
	actions := make([]Action, len(names))
	for i, name := range names {
		actions[i] = Action{Data: ActionData{Card: &Named{ID: "c1", Name: name}}}
	}
	want := []string{"First", "", "Renamed", "First"}
	for range 2 {
		got := ProjectActions(actions, "b1")
		for i, item := range got {
			if item.CardID != "c1" || item.Card != want[i] || actions[i].Data.Card.Name != names[i] {
				t.Errorf("action %d: %+v, want name %q with original id", i, item, want[i])
			}
		}
	}
	if got := ProjectActions(actions[1:], "b1"); got[0].Card != "First" {
		t.Error("name cache leaked across page boundaries")
	}
	actions = []Action{
		{Data: ActionData{Card: &Named{ID: "c1", Name: "Same"}}},
		{Data: ActionData{Card: &Named{ID: "c2", Name: "Same"}}},
		{Data: ActionData{Card: &Named{Name: "No id"}}},
		{Data: ActionData{Card: &Named{Name: "No id"}}},
	}
	for _, item := range ProjectActions(actions, "") {
		if item.Card == "" {
			t.Error("different or missing ids incorrectly deduplicated")
		}
	}
	empty, err := Encode(Collection(ProjectActions(nil, ""), ""))
	if err != nil || empty != `{"items":[]}` {
		t.Errorf("empty actions: %s %v", empty, err)
	}
}

func TestActionActorFallback(t *testing.T) {
	for _, tc := range []struct {
		creator *Actor
		id      string
		want    string
	}{
		{&Actor{ID: "m1", Username: "synthetic"}, "m1", "synthetic"},
		{&Actor{ID: "m1"}, "m1", "m1"},
		{&Actor{}, "m1", "m1"},
		{nil, "m1", "m1"},
		{nil, "", ""},
	} {
		got := ProjectAction(Action{Creator: tc.creator, CreatorID: tc.id, Data: ActionData{Member: tc.creator}}, "")
		if got.By != tc.want || got.Member != actorName(tc.creator) {
			t.Errorf("actor fallback: %+v, want %q", got, tc.want)
		}
	}
}

func TestActionPageListNames(t *testing.T) {
	actions := []Action{
		{Data: ActionData{Card: &Named{ID: "l1", Name: "Same"}, List: &Named{ID: "l1", Name: "Same"}, ListBefore: &Named{ID: "l1", Name: "Same"}, ListAfter: &Named{ID: "l2", Name: "Same"}}},
		{Data: ActionData{List: &Named{ID: "l2", Name: "Same"}, ListBefore: &Named{ID: "l1", Name: "Renamed"}, ListAfter: &Named{ID: "l1", Name: "Renamed"}}},
		{Data: ActionData{List: &Named{ID: "l1", Name: "Same"}, ListBefore: &Named{ID: "archived"}, ListAfter: &Named{Name: "No id"}}},
		{Data: ActionData{List: &Named{Name: "No id"}}},
	}
	for range 2 {
		got := ProjectActions(actions, "")
		if got[0].Card != "Same" || got[0].List != "Same" || got[0].ListBefore != "" || got[0].ListAfter != "Same" || got[1].List != "" || got[1].ListBefore != "Renamed" || got[1].ListAfter != "" || got[2].List != "Same" || got[2].ListAfter != "No id" || got[3].List != "No id" {
			t.Fatalf("incorrect list name deduplication: %+v", got)
		}
		for i, item := range got {
			data := actions[i].Data
			for _, ref := range []struct {
				id     string
				source *Named
			}{{item.ListID, data.List}, {item.ListBeforeID, data.ListBefore}, {item.ListAfterID, data.ListAfter}} {
				if ref.source != nil && ref.id != ref.source.ID {
					t.Errorf("action %d lost list id: %+v", i, item)
				}
			}
		}
	}
	if ProjectActions(actions[1:], "")[0].List != "Same" || actions[0].Data.ListBefore.Name != "Same" {
		t.Error("list name cache crossed pages or mutated source")
	}
}

func TestActionPageUnknownNames(t *testing.T) {
	actions := []Action{
		{Data: ActionData{Card: &Named{ID: "shared"}, List: &Named{ID: "shared"}, ListBefore: &Named{ID: "l1"}, ListAfter: &Named{ID: "l1"}}},
		{Data: ActionData{Card: &Named{ID: "shared", Name: "Same"}, List: &Named{ID: "shared", Name: "Same"}, ListBefore: &Named{ID: "l1", Name: "Before"}, ListAfter: &Named{ID: "l1", Name: "Before"}}},
		{Data: ActionData{Card: &Named{ID: "shared", Name: "Same"}, List: &Named{ID: "shared"}, ListBefore: &Named{ID: "l1"}, ListAfter: &Named{ID: "l1"}}},
		{Data: ActionData{Card: &Named{ID: "shared"}, List: &Named{ID: "shared"}, ListBefore: &Named{ID: "l1", Name: "Before"}, ListAfter: &Named{ID: "l1", Name: "Renamed"}}},
		{Data: ActionData{Card: &Named{ID: "shared", Name: "Same"}, List: &Named{ID: "shared", Name: "Same"}, ListBefore: &Named{ID: "l1", Name: "Renamed"}, ListAfter: &Named{Name: "No id"}}},
		{Data: ActionData{Card: &Named{ID: "shared", Name: "Renamed"}, List: &Named{ID: "shared", Name: "Renamed"}, ListBefore: &Named{ID: "shared", Name: "Second"}, ListAfter: &Named{ID: "shared", Name: "Third"}}},
		{Data: ActionData{Card: &Named{ID: "shared", Name: "Renamed"}, List: &Named{ID: "shared", Name: "Third"}, ListAfter: &Named{Name: "No id"}}},
	}
	wantNames := [][4]string{
		{"", "", "", ""},
		{"Same", "Same", "Before", ""},
		{"", "", "", ""},
		{"", "", "", "Renamed"},
		{"", "", "", "No id"},
		{"Renamed", "Renamed", "Second", "Third"},
		{"", "", "", "No id"},
	}
	original, err := json.Marshal(actions)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		got := ProjectActions(actions, "b1")
		for i, item := range got {
			want := ProjectAction(actions[i], "b1")
			want.Card, want.List, want.ListBefore, want.ListAfter = wantNames[i][0], wantNames[i][1], wantNames[i][2], wantNames[i][3]
			if !reflect.DeepEqual(item, want) {
				t.Errorf("action %d: got %+v, want %+v", i, item, want)
			}
			encoded, err := json.Marshal(item)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			for j, key := range []string{"card", "list", "listBefore", "listAfter"} {
				_, present := fields[key]
				if present != (wantNames[i][j] != "") {
					t.Errorf("action %d: incorrect omission of %s: %s", i, key, encoded)
				}
			}
		}
	}
	for i, action := range actions {
		// A fresh page still deduplicates shared list slots within its first action.
		want := ProjectActions([]Action{action}, "b1")[0]
		if got := ProjectActions(actions[i:], "b1")[0]; !reflect.DeepEqual(got, want) {
			t.Errorf("page starting at %d reused names: got %+v, want %+v", i, got, want)
		}
	}
	after, err := json.Marshal(actions)
	if err != nil || string(after) != string(original) {
		t.Fatalf("projection changed input: %s, err=%v", after, err)
	}
}
