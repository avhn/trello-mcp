package credentials

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDotenvPreservesOtherLines(t *testing.T) {
	for _, unrelated := range []string{"# shared secrets\nOTHER=value=with=equals\n\n# end\n", "# shared secrets\r\nOTHER=unchanged"} {
		path := filepath.Join(credentialTestDir(t), ".secrets", ".trello-mcp.secret")
		if err := Save(path, synthetic()); err != nil {
			t.Fatal(err)
		}
		original := "# first\nTRELLO_API_KEY=old-key\n\nTRELLO_TOKEN=old-token\nTRELLO_TOKEN_SCOPE=read,write\n" + unrelated
		if err := os.WriteFile(path, []byte(original), 0600); err != nil {
			t.Fatal(err)
		}
		if err := Save(path, synthetic()); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		want := "# first\nTRELLO_API_KEY=synthetic-key\n\nTRELLO_TOKEN=synthetic-token\nTRELLO_TOKEN_SCOPE=read\n" + unrelated
		if err != nil || string(data) != want {
			t.Fatal("save changed unrelated lines or their order")
		}
		got, err := Load(path)
		if err != nil || got != synthetic() {
			t.Fatal("dotenv did not round trip")
		}
		if err := Logout(path); err != nil {
			t.Fatal(err)
		}
		data, err = os.ReadFile(path)
		if err != nil || string(data) != "# first\n\n"+unrelated {
			t.Fatal("logout did not preserve unrelated lines")
		}
		got, source, _, err := Resolve(func(string) string { return "" }, Source{Path: path})
		if err != nil || source != "none" || got != (File{}) {
			t.Fatal("unrelated entries counted as credentials")
		}
	}
}

func TestDotenvUnterminatedUnrelatedLine(t *testing.T) {
	original := []byte("# comment\nOTHER=unchanged")
	c := synthetic()
	got := rewrite(original, &c)
	if !strings.HasSuffix(string(got), string(original)) {
		t.Fatal("save changed unterminated unrelated line")
	}
	if string(rewrite(got, nil)) != string(original) {
		t.Fatal("logout changed unterminated unrelated line")
	}
}

func TestValueValidation(t *testing.T) {
	for _, value := range []string{"", " ", " leading", "trailing\t", " boundary", "embedded\x00nul", "embedded\rcarriage", "embedded\nnewline", "trailing\n", "'quoted'", "\"quoted\""} {
		if ValidValue(value) {
			t.Error("unsafe value accepted")
		}
		for _, field := range []string{"key", "token", "scope"} {
			c := synthetic()
			switch field {
			case "key":
				c.APIKey = value
			case "token":
				c.Token = value
			case "scope":
				c.Scope = value
			}
			if field == "scope" && value == "" {
				continue
			} // Missing scope means unknown.
			if err := Save(filepath.Join(credentialTestDir(t), ".secrets", ".trello-mcp.secret"), c); err == nil {
				t.Error("unsafe credential saved")
			}
		}
	}
	for _, value := range []string{"synthetic", "one=two", "one two", "one#two", "'unpaired", "\"unpaired"} {
		if !ValidValue(value) {
			t.Error("valid unquoted value refused")
		}
	}
}

func TestDotenvLoadValidation(t *testing.T) {
	path := filepath.Join(credentialTestDir(t), ".secrets", ".trello-mcp.secret")
	if err := Save(path, synthetic()); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"", " leading", "trailing ", "'quoted'", "\"quoted\"", "nul\x00", "carriage\r", "embedded\ncontinuation"} {
		data := "TRELLO_API_KEY=synthetic-key\nTRELLO_TOKEN=" + token + "\n"
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil || strings.Contains(err.Error(), "synthetic-key") {
			t.Error("invalid dotenv value accepted or leaked")
		}
	}
	for _, data := range []string{
		"TRELLO_API_KEY=synthetic-key\nTRELLO_TOKEN=synthetic-token\nTRELLO_TOKEN=duplicate\n",
		" TRELLO_API_KEY=synthetic-key\nTRELLO_TOKEN=synthetic-token\n",
		"TRELLO_TOKEN_SCOPE=read\n",
	} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Error("malformed credentials accepted")
		}
	}
	// A final LF is a line terminator, not part of the value; scope is optional.
	for _, ending := range []string{"", "\n"} {
		if err := os.WriteFile(path, []byte("TRELLO_API_KEY=synthetic-key\nTRELLO_TOKEN=synthetic-token"+ending), 0600); err != nil {
			t.Fatal(err)
		}
		c, err := Load(path)
		if err != nil || c.Scope != "" || c.APIKey != "synthetic-key" || c.Token != "synthetic-token" {
			t.Fatal("optional scope or final LF rejected")
		}
	}
}

func TestPathPrecedence(t *testing.T) {
	home := credentialTestDir(t)
	fallback := filepath.Join(home, ".secrets", ".trello-mcp.secret")
	override := filepath.Join(home, "other", "custom.secret")
	for _, path := range []string{fallback, override} {
		c := synthetic()
		c.Token = filepath.Base(path)
		if err := Save(path, c); err != nil {
			t.Fatal(err)
		}
	}
	values := map[string]string{}
	env := func(key string) string { return values[key] }
	getHome := func() (string, error) { return home, nil }
	path, err := Path(env, getHome)
	if err != nil || path != fallback {
		t.Fatal("incorrect default path")
	}
	values["TRELLO_MCP_CREDENTIALS"] = override
	path, err = Path(env, getHome)
	c, source, _, resolveErr := Resolve(env, Source{Path: path})
	if err != nil || resolveErr != nil || source != "file" || c.Token != filepath.Base(override) {
		t.Fatal("override ignored")
	}
	values["TRELLO_API_KEY"], values["TRELLO_TOKEN"] = "env-key", "env-token"
	c, source, _, err = Resolve(env, Source{Path: path})
	if err != nil || source != "env" || c.Token != "env-token" || c.Scope != "" {
		t.Fatal("environment pair did not override file")
	}
	delete(values, "TRELLO_TOKEN")
	if _, _, _, err := Resolve(env, Source{Path: path}); err == nil {
		t.Fatal("partial pair fell back to file")
	}
	values["TRELLO_MCP_CREDENTIALS"] = "relative.secret"
	if _, err := Path(env, getHome); err == nil {
		t.Fatal("relative override accepted")
	}
	delete(values, "TRELLO_MCP_CREDENTIALS")
	if _, err := Path(env, func() (string, error) { return "", errors.New("unavailable") }); err == nil {
		t.Fatal("missing home accepted")
	}
}
