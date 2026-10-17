package formations

import (
	"encoding/json"
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

// claudeCodeModels lists the family aliases
// accepted by `claude --model`. The first is a new Claude Code slot's default.
var claudeCodeModels = []string{
	"opus", "sonnet", "haiku", "fable",
}

// HarnessModels lists the models offered for a harness, the one a new slot on
// that harness takes first. Claude Code offers family aliases. Codex
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
