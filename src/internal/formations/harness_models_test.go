package formations

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// useCodexModels points the Codex model cache at testdata/codex-home, or at an
// empty directory when fixture is false.
func useCodexModels(t *testing.T, fixture bool) string {
	t.Helper()
	dir := t.TempDir()
	if fixture {
		raw, err := os.ReadFile(filepath.Join("testdata", "codex-home", "models_cache.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "models_cache.json"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CODEX_HOME", dir)
	return dir
}

func TestHarnessesOfferTheirKnownModels(t *testing.T) {
	useCodexModels(t, true)
	if got := HarnessModels("claude-code"); !reflect.DeepEqual(got, []HarnessModel{{ID: "opus"}, {ID: "sonnet"}, {ID: "haiku"}, {ID: "fable"}}) {
		t.Fatalf("claude-code models = %+v", got)
	}
	// Codex lists the models its picker shows, by priority, with each model's levels.
	want := []HarnessModel{
		{ID: "gpt-6-astra", Efforts: []string{"low", "medium", "high", "xhigh", "max", "ultra"}},
		{ID: "gpt-5.6-sol", Efforts: []string{"low", "medium", "high", "xhigh", "max", "ultra"}},
		{ID: "gpt-5.5", Efforts: []string{"low", "medium", "high", "xhigh"}},
	}
	if got := HarnessModels("openai-codex"); !reflect.DeepEqual(got, want) {
		t.Fatalf("openai-codex models = %+v, want %+v", got, want)
	}
	if got := HarnessModels("hermes"); got != nil {
		t.Fatalf("hermes models = %+v, want none", got)
	}
	for _, harness := range LaunchableHarnesses() {
		if harness.ID == "openai-codex" && !reflect.DeepEqual(harness.Models, want) {
			t.Fatalf("launchable openai-codex models = %+v", harness.Models)
		}
	}

	// Without the host's cache, Codex offers no models and nothing is narrowed.
	useCodexModels(t, false)
	if got := HarnessModels("openai-codex"); len(got) != 0 {
		t.Fatalf("openai-codex models without a cache = %+v", got)
	}
}

func TestTheCodexCacheIsReadAgainWhenItChanges(t *testing.T) {
	dir := useCodexModels(t, true)
	if got := len(HarnessModels("openai-codex")); got != 3 {
		t.Fatalf("models = %d, want 3", got)
	}
	path := filepath.Join(dir, "models_cache.json")
	if err := os.WriteFile(path, []byte(`{"models":[{"slug":"gpt-7","visibility":"list","priority":1,"supported_reasoning_levels":[{"effort":"low"}]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if got := HarnessModels("openai-codex"); !reflect.DeepEqual(got, []HarnessModel{{ID: "gpt-7", Efforts: []string{"low"}}}) {
		t.Fatalf("models after the cache changed = %+v", got)
	}
}

// A malformed cache is read once: it means no known models until the file
// changes, however often the models are asked for.
func TestAMalformedCodexCacheIsRememberedUntilItChanges(t *testing.T) {
	dir := useCodexModels(t, false)
	path := filepath.Join(dir, "models_cache.json")
	valid := `{"models":[{"slug":"gpt-7","visibility":"list","priority":1}]}`
	malformed := strings.Repeat("{", len(valid))
	if err := os.WriteFile(path, []byte(malformed), 0o644); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if got := HarnessModels("openai-codex"); len(got) != 0 {
		t.Fatalf("models from a malformed cache = %+v", got)
	}
	// Same size and modification time: the remembered failure answers, and the file is not read again.
	if err := os.WriteFile(path, []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if got := HarnessModels("openai-codex"); len(got) != 0 {
		t.Fatalf("models before the cache changed = %+v, want the remembered failure", got)
	}
	// A new modification time reads it again.
	later := stamp.Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if got := HarnessModels("openai-codex"); !reflect.DeepEqual(got, []HarnessModel{{ID: "gpt-7"}}) {
		t.Fatalf("models after the cache changed = %+v", got)
	}
}

// A known Codex model narrows the efforts a slot may take; an unknown one
// keeps the harness's list (archon-n7u.50).
func TestASlotsEffortIsCheckedAgainstItsModel(t *testing.T) {
	useCodexModels(t, true)
	for _, check := range []struct {
		name, model, effort, want string
	}{
		{"a model's own level", "gpt-5.5", "xhigh", ""},
		{"a level the model lacks", "gpt-5.5", "ultra", `effort "ultra" is not one gpt-5.5 accepts; use low, medium, high, xhigh`},
		{"a hidden model still names its levels", "gpt-reserve", "ultra", `effort "ultra" is not one gpt-reserve accepts; use low, medium, high, xhigh, max`},
		{"an unknown model keeps the harness list", "gpt-7-nova", "ultra", ""},
		{"the harness default model keeps the harness list", "", "ultra", ""},
	} {
		err := validateSlotSettings(`"Worker"`, "openai-codex", check.model, check.effort)
		if check.want == "" {
			if err != nil {
				t.Errorf("%s: %v", check.name, err)
			}
			continue
		}
		if !errors.Is(err, ErrInvalidSlotSettings) || !strings.Contains(err.Error(), check.want) {
			t.Errorf("%s: error = %v, want %q", check.name, err, check.want)
		}
	}

	// Admission names the slot and the model's levels before any seat starts.
	store, personas := s4RunFixture(t)
	writeFixture(t, store.BoardPath("session-search"), vanillaSlotBoard("harness = \"openai-codex\"\nmodel = \"gpt-5.5\"\neffort = \"ultra\"\n"))
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	report := ValidateRunAdmission(board, personas, RunAdmissionScope{MissionID: "mis_showcase"})
	if len(report.Errors) != 1 || report.Errors[0].Code != FindingInvalidSlotSettings || !strings.Contains(report.Errors[0].Message, `slot "Researcher" (slot_research) effort "ultra" is not one gpt-5.5 accepts`) {
		t.Fatalf("admission = %+v", report.Errors)
	}
}

func TestAModelOutsideTheCatalogIsAcceptedWithAWarning(t *testing.T) {
	useCodexModels(t, true)
	for _, check := range []struct {
		harness, model, want string
	}{
		{"openai-codex", "gpt-7-nova", `slot "Worker" model "gpt-7-nova" is not in the openai-codex catalog; the harness decides`},
		{"claude-code", "claude-opus-5", `slot "Worker" model "claude-opus-5" is not in the claude-code catalog; the harness decides`},
		{"openai-codex", "gpt-6-astra", ""},
		{"openai-codex", "gpt-reserve", ""},
		{"claude-code", "opus", ""},
		{"claude-code", "", ""},
	} {
		if got := SlotModelWarning(`"Worker"`, check.harness, check.model); got != check.want {
			t.Errorf("%s %s: warning = %q, want %q", check.harness, check.model, got, check.want)
		}
	}
	// Without a catalog for the harness there is nothing to warn about.
	useCodexModels(t, false)
	if got := SlotModelWarning(`"Worker"`, "openai-codex", "gpt-7-nova"); got != "" {
		t.Fatalf("warning without a Codex cache = %q", got)
	}
}
