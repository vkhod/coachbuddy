package modelcatalog

import (
	"strings"
	"testing"
)

func TestEmbeddedListIsValid(t *testing.T) {
	c := Embedded()
	if got := c.Default().ID; got != "gemini/gemini-3.8-flash" {
		t.Errorf("default model %q", got)
	}
	if id, ok := c.DefaultFor("anthropic"); !ok || id != "anthropic/claude-sonnet-5-5" {
		t.Errorf("anthropic default %q %v", id, ok)
	}
	if c.HasProvider("groq") {
		t.Error("groq was dropped from the list on 2026-11-18")
	}
	if !c.Allows("openai", "openai/gpt-5.4-mini") || c.Allows("gemini", "openai/gpt-5.4-mini") {
		t.Error("Allows must match model and provider")
	}
}

func TestParseRejectsBrokenLists(t *testing.T) {
	cases := map[string]string{
		"two defaults":     `{"providers":[{"id":"a"}],"models":[{"id":"a/x","provider":"a","vision":true,"default":true},{"id":"a/y","provider":"a","vision":true,"default":true}]}`,
		"no default":       `{"providers":[{"id":"a"}],"models":[{"id":"a/x","provider":"a","vision":true}]}`,
		"unknown provider": `{"providers":[{"id":"a"}],"models":[{"id":"b/x","provider":"b","vision":true,"default":true}]}`,
		"bad prefix":       `{"providers":[{"id":"a"}],"models":[{"id":"x","provider":"a","vision":true,"default":true}]}`,
		"default no image": `{"providers":[{"id":"a"}],"models":[{"id":"a/x","provider":"a","default":true}]}`,
		"unknown field":    `{"providers":[{"id":"a"}],"models":[],"extra":1}`,
	}
	for name, body := range cases {
		if _, err := Parse([]byte(body)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestLoadFromFile(t *testing.T) {
	path := t.TempDir() + "/models.json"
	if err := writeFile(path, `{"providers":[{"id":"a"}],"models":[{"id":"a/x","provider":"a","vision":true,"default":true}]}`); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil || c.Default().ID != "a/x" {
		t.Fatalf("load: %v %+v", err, c)
	}
	if _, err := Load(path + ".missing"); err == nil || !strings.Contains(err.Error(), "model list") {
		t.Fatalf("missing file: %v", err)
	}
}
