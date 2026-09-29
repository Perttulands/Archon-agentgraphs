package formations

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

// DefaultHarnessEffort is the effort a harness variant with no effort runs at.
const DefaultHarnessEffort = "medium"

// LaunchableHarness is a harness whose seats Archon starts from card settings.
// Efforts lists the values its CLI accepts: `claude --effort` for Claude Code,
// and the union of every Codex model's supported reasoning levels (Codex
// models_cache.json) for Codex, whose `-c model_reasoning_effort` takes them.
type LaunchableHarness struct {
	ID            string   `json:"id"`
	Executable    string   `json:"executable"`
	Efforts       []string `json:"efforts"`
	DefaultEffort string   `json:"defaultEffort"`
}

var launchableHarnesses = []LaunchableHarness{
	{ID: "claude-code", Executable: "claude", Efforts: []string{"low", "medium", "high", "xhigh", "max"}, DefaultEffort: DefaultHarnessEffort},
	{ID: "openai-codex", Executable: "codex", Efforts: []string{"low", "medium", "high", "xhigh", "max", "ultra"}, DefaultEffort: DefaultHarnessEffort},
}

// LaunchableHarnesses returns the harnesses Archon can start seats for.
func LaunchableHarnesses() []LaunchableHarness {
	out := make([]LaunchableHarness, len(launchableHarnesses))
	for i, harness := range launchableHarnesses {
		harness.Efforts = slices.Clone(harness.Efforts)
		out[i] = harness
	}
	return out
}

func launchableHarness(id string) (LaunchableHarness, bool) {
	for _, harness := range launchableHarnesses {
		if harness.ID == id {
			return harness, true
		}
	}
	return LaunchableHarness{}, false
}

// validateHarnessSettings checks a variant's model and effort before they are
// written. Blank values mean the harness default model and the default effort.
func validateHarnessSettings(agentID, harnessID, model, effort string) error {
	model, effort = strings.TrimSpace(model), strings.TrimSpace(effort)
	if model == "" && effort == "" {
		return nil
	}
	harness, ok := launchableHarness(harnessID)
	if !ok {
		return fmt.Errorf("%w: agent %q harness %q has no model or effort setting; only claude-code and openai-codex seats take them", ErrInvalidAgentCard, agentID, harnessID)
	}
	if strings.IndexFunc(model, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return fmt.Errorf("%w: agent %q model %q must be one model name without spaces", ErrInvalidAgentCard, agentID, model)
	}
	if effort != "" && !slices.Contains(harness.Efforts, effort) {
		return fmt.Errorf("%w: agent %q effort %q is not one %s accepts; use %s", ErrInvalidAgentCard, agentID, effort, harness.ID, strings.Join(harness.Efforts, ", "))
	}
	return nil
}

func (v HarnessVariant) effectiveEffort() string {
	if strings.TrimSpace(v.Effort) == "" {
		return DefaultHarnessEffort
	}
	return strings.TrimSpace(v.Effort)
}

// RenderLaunch makes card settings authoritative over legacy launch strings.
// The executable is supplied by the adapter after resolving it on PATH.
func (v HarnessVariant) RenderLaunch(executable string) (string, error) {
	command := "exec " + shellQuote(executable)
	if v.Model != "" {
		command += " --model " + shellQuote(v.Model)
	}
	switch v.ID {
	case "openai-codex":
		command += " -c " + shellQuote("model_reasoning_effort=\""+v.effectiveEffort()+"\"")
		command += " -c check_for_update_on_startup=false --dangerously-bypass-approvals-and-sandbox"
	case "claude-code":
		command += " --effort " + shellQuote(v.effectiveEffort()) + " --dangerously-skip-permissions"
	default:
		return "", fmt.Errorf("unsupported seat harness %q", v.ID)
	}
	return command, nil
}

// LaunchCommand is the command a seat for this variant runs: the harness CLI
// resolved on this process's PATH, rendered by RenderLaunch. When the CLI is
// not on PATH the command names it bare and the error says so.
func (v HarnessVariant) LaunchCommand() (string, error) {
	harness, ok := launchableHarness(v.ID)
	if !ok {
		return "", fmt.Errorf("unsupported seat harness %q", v.ID)
	}
	bin, err := exec.LookPath(harness.Executable)
	if err == nil {
		bin, err = filepath.Abs(bin)
	}
	if err != nil {
		command, renderErr := v.RenderLaunch(harness.Executable)
		if renderErr != nil {
			return "", renderErr
		}
		return command, fmt.Errorf("%s is not on PATH: %w", harness.Executable, err)
	}
	return v.RenderLaunch(bin)
}

// SpawnCommand is what `archon agent spawn` runs: the seat command for a
// harness Archon renders, else the card's launch string for a harness it
// cannot (such as hermes), which may be empty.
func (v HarnessVariant) SpawnCommand() (string, error) {
	if _, ok := launchableHarness(v.ID); ok {
		return v.LaunchCommand()
	}
	return v.Launch, nil
}

// DescribeLaunches fills each variant's resolved effort, the efforts its
// harness accepts and the command its seats run, for readers; none of them is
// stored in the card.
func (c *PersonaCard) DescribeLaunches() {
	for i := range c.HarnessVariants {
		variant := &c.HarnessVariants[i]
		variant.EffectiveEffort, variant.Efforts, variant.SeatLaunch, variant.SeatLaunchError = "", nil, "", ""
		if harness, ok := launchableHarness(variant.ID); ok {
			variant.EffectiveEffort = variant.effectiveEffort()
			variant.Efforts = slices.Clone(harness.Efforts)
		}
		command, err := variant.LaunchCommand()
		variant.SeatLaunch = command
		if err != nil {
			variant.SeatLaunchError = err.Error()
		}
	}
}

func (v HarnessVariant) verifyTurnSettings(turn codexTranscriptTurn) error {
	// Claude records model on assistant messages but may omit effort. Codex
	// records both in turn_context, so missing evidence there is a mismatch.
	if v.Model != "" && turn.Model != v.Model || (v.ID == "openai-codex" || turn.Effort != "") && turn.Effort != v.effectiveEffort() {
		return fmt.Errorf("seat model/effort mismatch: expected %s/%s, observed %s/%s", v.Model, v.effectiveEffort(), turn.Model, turn.Effort)
	}
	return nil
}
