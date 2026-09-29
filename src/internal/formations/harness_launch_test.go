package formations

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersonaEffortIsValidatedPerHarness(t *testing.T) {
	s := NewPersonaStore(t.TempDir())
	if _, err := s.CreatePersona(CreatePersonaRequest{ID: "claude", Harness: "claude-code", Effort: "ultra"}); !errors.Is(err, ErrInvalidAgentCard) || !strings.Contains(err.Error(), "low, medium, high, xhigh, max") {
		t.Fatalf("claude ultra error = %v, want the efforts claude-code accepts", err)
	}
	if _, err := s.CreatePersona(CreatePersonaRequest{ID: "codex", Harness: "openai-codex", Model: "two words"}); !errors.Is(err, ErrInvalidAgentCard) {
		t.Fatalf("model with a space error = %v", err)
	}
	if _, err := s.CreatePersona(CreatePersonaRequest{ID: "hermes", Harness: "hermes", Model: "any"}); !errors.Is(err, ErrInvalidAgentCard) {
		t.Fatalf("hermes model error = %v", err)
	}
	if _, err := os.Stat(s.PersonaPath("claude")); !os.IsNotExist(err) {
		t.Fatalf("rejected persona was written: %v", err)
	}
	card, err := s.CreatePersona(CreatePersonaRequest{ID: "codex", Harness: "openai-codex", Effort: "ultra"})
	if err != nil {
		t.Fatal(err)
	}
	card, err = s.EditPersona(card.ID, EditPersonaRequest{ExpectedETag: card.ETag, AddHarness: "claude-code", Model: "claude-opus-5", Effort: "max"})
	if err != nil {
		t.Fatal(err)
	}

	// Model and effort edit the named variant; the default harness is untouched.
	low := "low"
	card, err = s.EditPersona(card.ID, EditPersonaRequest{ExpectedETag: card.ETag, Variant: "claude-code", SetEffort: &low})
	if err != nil {
		t.Fatal(err)
	}
	if claude, _ := card.SelectHarnessVariant("claude-code"); claude.Effort != "low" || claude.Model != "claude-opus-5" {
		t.Fatalf("claude variant = %+v", claude)
	}
	if codex := card.DefaultVariant(); codex.Effort != "ultra" {
		t.Fatalf("default variant changed: %+v", codex)
	}
	ultra := "ultra"
	if _, err := s.EditPersona(card.ID, EditPersonaRequest{ExpectedETag: card.ETag, Variant: "claude-code", SetEffort: &ultra}); !errors.Is(err, ErrInvalidAgentCard) {
		t.Fatalf("claude ultra edit error = %v", err)
	}
	if _, err := s.EditPersona(card.ID, EditPersonaRequest{ExpectedETag: card.ETag, Variant: "hermes", SetEffort: &low}); !errors.Is(err, ErrInvalidAgentCard) || !strings.Contains(err.Error(), `no harness variant "hermes"`) {
		t.Fatalf("missing variant error = %v", err)
	}

	// Blank clears the setting from the card: the harness default model and medium.
	blank := ""
	card, err = s.EditPersona(card.ID, EditPersonaRequest{ExpectedETag: card.ETag, Variant: "claude-code", SetModel: &blank, SetEffort: &blank})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.PersonaPath(card.ID))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "claude-opus-5") || strings.Count(string(raw), "effort = ") != 1 {
		t.Fatalf("cleared settings remain:\n%s", raw)
	}
	if claude, _ := card.SelectHarnessVariant("claude-code"); claude.Model != "" || claude.effectiveEffort() != "medium" {
		t.Fatalf("cleared claude variant = %+v", claude)
	}
}

func TestSeatLaunchIsTheRenderedCardSettings(t *testing.T) {
	bin := t.TempDir()
	for _, name := range []string{"claude", "codex"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	card := PersonaCard{ID: "p", HarnessDefault: "claude-code", HarnessVariants: []HarnessVariant{
		{ID: "claude-code", Launch: `claude --effort="max"`},
		{ID: "openai-codex", Model: "gpt-6-sol", Effort: "high"},
		{ID: "hermes", Launch: "hermes --profile x"},
	}}
	card.DescribeLaunches()
	claude, codex, hermes := card.HarnessVariants[0], card.HarnessVariants[1], card.HarnessVariants[2]
	if want := "exec '" + filepath.Join(bin, "claude") + "' --effort 'medium' --dangerously-skip-permissions"; claude.SeatLaunch != want || claude.EffectiveEffort != "medium" || claude.SeatLaunchError != "" {
		t.Fatalf("claude = %+v, want seat launch %s", claude, want)
	}
	if !strings.Contains(codex.SeatLaunch, "--model 'gpt-6-sol'") || !strings.Contains(codex.SeatLaunch, `model_reasoning_effort="high"`) || codex.EffectiveEffort != "high" {
		t.Fatalf("codex = %+v", codex)
	}
	if hermes.SeatLaunch != "" || !strings.Contains(hermes.SeatLaunchError, `unsupported seat harness "hermes"`) || hermes.EffectiveEffort != "" {
		t.Fatalf("hermes = %+v", hermes)
	}
	if command, err := card.HarnessVariants[0].SpawnCommand(); err != nil || command != claude.SeatLaunch {
		t.Fatalf("claude spawn = %q, %v; want the seat launch", command, err)
	}
	if command, err := card.HarnessVariants[2].SpawnCommand(); err != nil || command != "hermes --profile x" {
		t.Fatalf("hermes spawn = %q, %v; want its launch string", command, err)
	}

	t.Setenv("PATH", t.TempDir())
	card.DescribeLaunches()
	if claude := card.HarnessVariants[0]; claude.SeatLaunch != "exec 'claude' --effort 'medium' --dangerously-skip-permissions" || !strings.Contains(claude.SeatLaunchError, "claude is not on PATH") {
		t.Fatalf("claude without PATH = %+v", claude)
	}
}

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
