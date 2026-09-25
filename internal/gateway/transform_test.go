package gateway

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPrepareBodyPatchesOnlyWhenNeeded(t *testing.T) {
	original := []byte(`{"model":"m","input":"hi","instructions":"keep"}`)
	got, err := prepareBody(original)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("body changed: %s", got)
	}
	patched, err := prepareBody([]byte(`{"model":"m","input":"hi","reasoning":{"effort":"minimal"}}`))
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(patched, &body); err != nil {
		t.Fatal(err)
	}
	instructions, _ := body["instructions"].(string)
	if strings.TrimSpace(instructions) == "" {
		t.Fatal("missing instructions")
	}
	reasoning, _ := body["reasoning"].(map[string]any)
	if reasoning["effort"] != "none" {
		t.Fatalf("effort = %#v", reasoning["effort"])
	}
}

func TestPrepareBodyKeepsUntouchedValuesVerbatim(t *testing.T) {
	patched, err := prepareBody([]byte(`{"seed":12345678901234567890,"input":[{"role":"user","content":"a<b && c>d"}],"reasoning":{"effort":"minimal","summary":"auto"}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"seed":12345678901234567890`, `"a<b && c>d"`, `"summary":"auto"`, `"effort":"none"`} {
		if !strings.Contains(string(patched), want) {
			t.Fatalf("missing %s in %s", want, patched)
		}
	}
}
