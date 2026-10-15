package view

import (
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
