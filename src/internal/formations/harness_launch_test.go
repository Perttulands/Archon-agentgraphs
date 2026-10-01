package formations

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A role is role text: a new card states no model or effort, and a card that
// still holds them reads, and serves, without them (ADR-0021).
func TestARoleCardCarriesNoModelOrEffort(t *testing.T) {
	s := NewPersonaStore(t.TempDir())
	card, err := s.CreatePersona(CreatePersonaRequest{ID: "worker", Kind: "builder", Harness: "openai-codex"})
	if err != nil {
		t.Fatal(err)
	}
	if raw := readFile(t, s.PersonaPath(card.ID)); strings.Contains(raw, "model") || strings.Contains(raw, "effort") {
		t.Fatalf("new card holds a model or effort:\n%s", raw)
	}
	legacy := "schema = 1\n\n[card]\nid = \"old\"\nkind = \"reviewer\"\n\n[harness]\ndefault = \"claude-code\"\n\n" +
		"[[harness.variant]]\nid = \"claude-code\"\nsession_stem = \"old\"\nmodel = \"claude-opus-5\"\neffort = \"xhigh\"\n"
	if err := os.WriteFile(s.PersonaPath("old"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	old, err := s.ReadPersona("old")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(old.HarnessVariants)
	if err != nil {
		t.Fatal(err)
	}
	if v := old.DefaultVariant(); v.Model != "" || v.Effort != "" || strings.Contains(string(raw), "claude-opus-5") || strings.Contains(string(raw), "xhigh") {
		t.Fatalf("legacy card read with settings: %+v\n%s", v, raw)
	}
	for _, preset := range personaPresetCatalog {
		card, _ := builtinPresetPersona(preset.ID)
		if v := card.DefaultVariant(); v.Model != "" || v.Effort != "" {
			t.Fatalf("preset %s carries settings: %+v", preset.ID, v)
		}
	}
}

// agent spawn states what a role's own session runs, as a slot does.
func TestSpawnRunsTheSettingsItStates(t *testing.T) {
	bin := t.TempDir()
	for _, name := range []string{"claude", "codex"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	command, err := HarnessVariant{ID: "openai-codex", SessionStem: "w", Model: "gpt-6-sol", Effort: "high"}.SpawnCommand()
	if err != nil || !strings.Contains(command, "--model 'gpt-6-sol'") || !strings.Contains(command, `model_reasoning_effort="high"`) {
		t.Fatalf("codex spawn = %q, %v", command, err)
	}
	if command, err := (HarnessVariant{ID: "hermes"}).SpawnCommand(); err == nil || command != "" || !strings.Contains(err.Error(), `Archon cannot start harness "hermes"`) {
		t.Fatalf("hermes spawn = %q, %v; want a plain refusal", command, err)
	}
	for _, check := range []struct{ harness, model, effort, want string }{
		{"claude-code", "opus", "ultra", `agent "w" effort "ultra" is not one claude-code accepts`},
		{"claude-code", "opus", "", `agent "w" needs an effort; the policy is`},
		{"hermes", "", "low", `agent "w" harness "hermes" cannot start seats`},
	} {
		if err := ValidateSpawnSettings("w", check.harness, check.model, check.effort); !errors.Is(err, ErrInvalidSlotSettings) || !strings.Contains(err.Error(), check.want) {
			t.Errorf("%+v: error = %v, want %q", check, err, check.want)
		}
	}
	if err := ValidateSpawnSettings("w", "claude-code", "opus", "low"); err != nil {
		t.Fatal(err)
	}
}

func TestHarnessLaunchAndMismatch(t *testing.T) {
	for _, harness := range []string{"openai-codex", "claude-code"} {
		t.Run(harness, func(t *testing.T) {
			v := HarnessVariant{ID: harness, Model: "test-model"}
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
