package formations

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPersonaHarnessSettingsRoundTrip(t *testing.T) {
	s := NewPersonaStore(t.TempDir())
	card, err := s.CreatePersona(CreatePersonaRequest{ID: "worker", Kind: "builder", Harness: "openai-codex", Model: "test-model", Effort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	card, err = s.ReadPersona(card.ID)
	if err != nil {
		t.Fatal(err)
	}
	var decoded PersonaCard
	raw, err := json.Marshal(card)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	v := decoded.DefaultVariant()
	if v.Model != "test-model" || v.Effort != "high" {
		t.Fatalf("settings lost: %+v", v)
	}
	model, effort := "second-model", "medium"
	card, err = s.EditPersona(card.ID, EditPersonaRequest{ExpectedETag: card.ETag, SetModel: &model, SetEffort: &effort})
	if err != nil {
		t.Fatal(err)
	}
	if v := card.DefaultVariant(); v.Model != model || v.Effort != effort {
		t.Fatalf("settings not edited: %+v", v)
	}
}

func TestHarnessLaunchAndMismatch(t *testing.T) {
	for _, harness := range []string{"openai-codex", "claude-code"} {
		t.Run(harness, func(t *testing.T) {
			v := HarnessVariant{ID: harness, Model: "test-model", Launch: "ignored --effort max"}
			launch, err := v.RenderLaunch("test-harness")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(launch, "--model 'test-model'") || !strings.Contains(launch, "medium") || strings.Contains(launch, "max") {
				t.Fatalf("launch: %s", launch)
			}
			for _, turn := range []codexTranscriptTurn{{Model: "wrong", Effort: "medium"}, {Model: "test-model", Effort: "high"}} {
				if err := v.verifyTurnSettings(turn); err == nil {
					t.Fatalf("accepted mismatch %+v", turn)
				}
			}
			if err := v.verifyTurnSettings(codexTranscriptTurn{Model: "test-model", Effort: "medium"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
