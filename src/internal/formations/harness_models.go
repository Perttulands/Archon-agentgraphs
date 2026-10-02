package formations

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"
)

// HarnessModel is a model a harness is known to run. Efforts lists the
// efforts the model accepts when the host knows them; without them the
// harness's efforts apply.
type HarnessModel struct {
	ID      string   `json:"id"`
	Efforts []string `json:"efforts,omitempty"`
}

// claudeCodeModels are the aliases `claude --model` resolves to the latest
// model of each family. The first is the model a new Claude Code slot takes.
var claudeCodeModels = []string{"opus", "sonnet", "haiku", "fable"}

// HarnessModels lists the models offered for a harness, the one a new slot on
// that harness takes first. Claude Code offers its family aliases. Codex
// offers the models its CLI lists, by priority, from the host's
// models_cache.json; without that cache it offers none.
func HarnessModels(harnessID string) []HarnessModel {
	switch harnessID {
	case "claude-code":
		models := make([]HarnessModel, 0, len(claudeCodeModels))
		for _, id := range claudeCodeModels {
			models = append(models, HarnessModel{ID: id})
		}
		return models
	case "openai-codex":
		cache := readCodexModels()
		models := make([]HarnessModel, 0, len(cache))
		for _, model := range cache {
			if model.listed {
				models = append(models, HarnessModel{ID: model.id, Efforts: slices.Clone(model.efforts)})
			}
		}
		return models
	}
	return nil
}

// modelEfforts returns the efforts the host knows a model accepts. Codex
// models the CLI hides from its picker still name their levels.
func modelEfforts(harnessID, model string) ([]string, bool) {
	if harnessID != "openai-codex" || model == "" {
		return nil, false
	}
	for _, known := range readCodexModels() {
		if known.id == model && len(known.efforts) > 0 {
			return known.efforts, true
		}
	}
	return nil, false
}

// SlotModelWarning names a model the harness is not known to run. Such a
// model is still accepted, since the harness decides; a blank model, a known
// one, or a harness whose models the host does not know warns of nothing.
func SlotModelWarning(slot, harnessID, model string) string {
	if model == "" {
		return ""
	}
	if _, ok := modelEfforts(harnessID, model); ok {
		return ""
	}
	models := HarnessModels(harnessID)
	if len(models) == 0 {
		return ""
	}
	for _, known := range models {
		if known.ID == model {
			return ""
		}
	}
	return fmt.Sprintf("slot %s model %q is not in the %s catalog; the harness decides", slot, model, harnessID)
}

type codexModel struct {
	id      string
	listed  bool
	efforts []string
}

var codexModelsCache struct {
	sync.Mutex
	path    string
	modTime time.Time
	size    int64
	models  []codexModel
}

// codexModelsPath is the Codex CLI's model cache: $CODEX_HOME, else ~/.codex.
func codexModelsPath() string {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		user, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		home = filepath.Join(user, ".codex")
	}
	return filepath.Join(home, "models_cache.json")
}

// readCodexModels reads the Codex model cache, re-reading it only when the
// file changes. A missing, unreadable or malformed cache means no known
// models. A file that does not parse is remembered as such until it changes,
// so it is not parsed again on every request; a read that fails, as while
// Codex rewrites the file, is tried again next time.
func readCodexModels() []codexModel {
	path := codexModelsPath()
	info, err := os.Stat(path)
	if path == "" || err != nil {
		return nil
	}
	codexModelsCache.Lock()
	defer codexModelsCache.Unlock()
	if codexModelsCache.path == path && codexModelsCache.modTime.Equal(info.ModTime()) && codexModelsCache.size == info.Size() {
		return codexModelsCache.models
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	models := parseCodexModels(raw)
	codexModelsCache.path, codexModelsCache.modTime, codexModelsCache.size, codexModelsCache.models = path, info.ModTime(), info.Size(), models
	return models
}

// parseCodexModels reads the models a Codex model cache lists, by priority;
// nil when it does not parse.
func parseCodexModels(raw []byte) []codexModel {
	var cache struct {
		Models []struct {
			Slug       string `json:"slug"`
			Visibility string `json:"visibility"`
			Priority   int    `json:"priority"`
			Levels     []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &cache); err != nil {
		return nil
	}
	sort.SliceStable(cache.Models, func(i, j int) bool { return cache.Models[i].Priority < cache.Models[j].Priority })
	models := make([]codexModel, 0, len(cache.Models))
	for _, entry := range cache.Models {
		if entry.Slug == "" {
			continue
		}
		model := codexModel{id: entry.Slug, listed: entry.Visibility == "list"}
		for _, level := range entry.Levels {
			if level.Effort != "" {
				model.efforts = append(model.efforts, level.Effort)
			}
		}
		models = append(models, model)
	}
	return models
}

// SlotWarnings lists what a staffed slot's settings should make its author
// look at again; today that is a model outside its harness's catalog.
func SlotWarnings(board *BoardDocument, formationID, slotID string) []string {
	if board == nil {
		return nil
	}
	for _, formation := range board.Formations {
		if formation.ID != formationID {
			continue
		}
		for _, slot := range formation.Slots {
			if slot.ID == slotID {
				if warning := SlotModelWarning(slotName(slot), slot.Harness, slot.Model); warning != "" {
					return []string{warning}
				}
			}
		}
	}
	return nil
}
