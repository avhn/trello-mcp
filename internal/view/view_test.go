package view

import (
	"encoding/json"
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
	want := `{"id":"a1","type":"updateCard","date":"today","by":"synthetic","cardId":"c1","card":"Card","list":"List","listBefore":"Before","listAfter":"After","old":{"closed":false,"due":null,"name":"","pos":0},"changed":["cover","prefs"],"checklist":"Checklist","item":"Item","state":"complete","attachment":{"id":"at1","name":"File"},"label":{"id":"lb1","name":"Ready","color":"green"},"member":"teammate"}`
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
