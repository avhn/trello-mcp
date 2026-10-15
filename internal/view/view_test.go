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
	raw := `{"id":"a1","type":"updateCard","date":"today","memberCreator":{"id":"m1","username":"synthetic","fullName":"drop","avatarUrl":"drop"},"data":{"card":{"id":"c1","name":"Card","desc":"drop"},"list":{"id":"l1","name":"List","pos":1},"board":{"id":"b1","name":"drop","prefs":{}},"listBefore":{"id":"l1","name":"Before","pos":1},"listAfter":{"id":"l2","name":"After","pos":2},"checklist":{"id":"cl1","name":"Checklist","checkItems":[]},"checkItem":{"id":"ci1","name":"Item","state":"complete","pos":1},"attachment":{"id":"at1","name":"File","isUpload":true,"url":"drop","previews":[]},"label":{"id":"lb1","name":"Ready","color":"green","uses":42},"member":{"id":"m2","username":"teammate","fullName":"drop"},"old":{"closed":false,"due":null,"name":"","pos":0,"prefs":{"drop":true},"cover":"drop"},"unknown":"drop"}}`
	var a Action
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		t.Fatal(err)
	}
	got, err := Encode(ProjectAction(a, "b1"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"a1","type":"updateCard","date":"today","data":{"card":{"id":"c1","name":"Card"},"list":{"id":"l1","name":"List"},"listBefore":{"id":"l1","name":"Before"},"listAfter":{"id":"l2","name":"After"},"old":{"closed":false,"due":null,"name":"","pos":0},"checklist":{"id":"cl1","name":"Checklist"},"checkItem":{"id":"ci1","name":"Item","state":"complete"},"attachment":{"id":"at1","name":"File"},"label":{"id":"lb1","name":"Ready","color":"green"},"member":{"id":"m2","username":"teammate"}},"memberCreator":{"id":"m1","username":"synthetic"}}`
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	other, err := Encode(ProjectAction(a, "b2"))
	if err != nil || !strings.Contains(other, `"board":{"id":"b1"}`) {
		t.Errorf("nonredundant board lost: %s %v", other, err)
	}
}

func TestActionTextAndOldValues(t *testing.T) {
	text := strings.Repeat("界", 2500)
	a := Action{Data: ActionData{Text: text, Old: map[string]any{"desc": text, "name": "before", "closed": false, "idList": "l1", "unknown": text, "state": map[string]any{"drop": true}}}}
	projected := ProjectAction(a, "b1")
	if projected.Data.Text != text {
		t.Error("comment text changed")
	}
	old := projected.Data.Old
	if len(old) != 4 || old["closed"] != false || old["idList"] != "l1" || old["name"] != "before" || utf8.RuneCountInString(old["desc"].(string)) != 200 || !strings.HasSuffix(old["desc"].(string), "...") {
		t.Error("old changed values were not bounded and filtered")
	}
	encoded, err := Encode(projected)
	if err != nil || !utf8.ValidString(encoded) {
		t.Errorf("invalid Unicode projection: %v", err)
	}
}
