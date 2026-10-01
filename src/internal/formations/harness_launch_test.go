package formations

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A role is role text: neither a new card nor a built-in role states a
// harness, model or effort, and the role's JSON names none.
func TestARoleCardCarriesNoHarnessModelOrEffort(t *testing.T) {
	s := NewPersonaStore(t.TempDir())
	if _, err := s.CreatePersona(CreatePersonaRequest{ID: "worker", Kind: "builder"}); err != nil {
		t.Fatal(err)
	}
	cards, err := s.ListPersonas()
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range cards {
		raw, err := json.Marshal(card)
		if err != nil {
			t.Fatal(err)
		}
		for _, word := range []string{"harness", "model", "effort", "session"} {
			if strings.Contains(card.TOML, word) || strings.Contains(string(raw), `"`+word) {
				t.Fatalf("role %s names a %s:\n%s\n%s", card.ID, word, card.TOML, raw)
			}
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
