package cases

import (
	"encoding/json"
	"testing"
)

func TestCacheSystemPromptIsStableAndBulk(t *testing.T) {
	first := CacheSystemPrompt
	second := CacheSystemPrompt
	if first != second {
		t.Error("CacheSystemPrompt is not deterministic across reads")
	}
	if len(first) < 8000 {
		t.Errorf("CacheSystemPrompt = %d bytes, want > 8000 (~2k tokens, clears automatic-cache minimums)", len(first))
	}
}

func TestCacheToolsAreValidJSONSchemas(t *testing.T) {
	if len(CacheTools) < 5 {
		t.Fatalf("got %d tools, want at least 5", len(CacheTools))
	}
	for _, tl := range CacheTools {
		if tl.Name == "" || tl.Description == "" {
			t.Errorf("tool %+v: missing name or description", tl)
		}
		if _, err := json.Marshal(tl.Schema); err != nil {
			t.Errorf("tool %s: schema does not marshal: %v", tl.Name, err)
		}
	}
}

func TestCacheQuestions(t *testing.T) {
	if len(CacheQuestions) < 8 {
		t.Fatalf("got %d questions, want at least 8", len(CacheQuestions))
	}
	seen := map[string]bool{}
	for _, q := range CacheQuestions {
		if q == "" {
			t.Error("empty question")
		}
		if seen[q] {
			t.Errorf("duplicate question %q", q)
		}
		seen[q] = true
	}
}
