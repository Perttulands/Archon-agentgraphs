package api

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Perttulands/chrote-agent-formations/internal/core"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// theme_default.json is the canonical default for the server and dashboard.
//
//go:embed theme_default.json
var themeDefaultJSON []byte

var themeColorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}([0-9A-Fa-f]{2})?$`)
var themeArtNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
var themeUIKeys = []string{"background", "surface", "surfaceRaised", "divider", "text", "textSecondary", "textDim", "accent", "error"}

type themeDocument struct {
	Schema   int               `json:"schema"`
	Name     string            `json:"name"`
	UI       map[string]string `json:"ui"`
	Terminal themeTerminal     `json:"terminal"`
	Shelves  []string          `json:"shelves"`
	Identity []string          `json:"identity"`
	Art      []string          `json:"art"`
}
type themeTerminal struct {
	Background          string   `json:"background"`
	Foreground          string   `json:"foreground"`
	Cursor              string   `json:"cursor"`
	SelectionBackground string   `json:"selectionBackground"`
	ANSI                []string `json:"ansi"`
}

// NewThemeHandler reads an optional operator-owned file on each request. Invalid
// configured documents fail this endpoint, without disabling mission execution.
func NewThemeHandler(path string) (http.Handler, error) {
	if path != "" && !filepath.IsAbs(path) {
		return nil, errors.New("--theme-file requires an absolute path")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := themeDefaultJSON
		if path != "" {
			raw, err := os.ReadFile(path)
			if err != nil {
				log.Printf("theme file unreadable: %v", err)
				core.WriteError(w, 500, "THEME_UNREADABLE", "configured theme file could not be read")
				return
			}
			if err := validateTheme(raw); err != nil {
				core.WriteError(w, 500, "INVALID_THEME", fmt.Sprintf("configured theme file is invalid: %v", err))
				return
			}
			body = raw
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(body)
	}), nil
}

func isThemeArtName(name string) bool {
	return name != "." && name != ".." && themeArtNamePattern.MatchString(name)
}

func validateTheme(raw []byte) error {
	var theme themeDocument
	if err := json.Unmarshal(raw, &theme); err != nil {
		return fmt.Errorf("not valid theme JSON: %w", err)
	}

	if theme.Schema != 1 {
		return fmt.Errorf("schema must be 1, got %d", theme.Schema)
	}
	if strings.TrimSpace(theme.Name) == "" {
		return errors.New("name must not be empty")
	}

	for _, key := range themeUIKeys {
		if err := validateThemeColor(fmt.Sprintf("ui.%s", key), theme.UI[key]); err != nil {
			return err
		}
	}

	terminalColors := []struct {
		field string
		value string
	}{
		{"terminal.background", theme.Terminal.Background},
		{"terminal.foreground", theme.Terminal.Foreground},
		{"terminal.cursor", theme.Terminal.Cursor},
		{"terminal.selectionBackground", theme.Terminal.SelectionBackground},
	}
	for _, color := range terminalColors {
		if err := validateThemeColor(color.field, color.value); err != nil {
			return err
		}
	}

	if len(theme.Terminal.ANSI) != 16 {
		return fmt.Errorf("terminal.ansi must have exactly 16 colours, got %d", len(theme.Terminal.ANSI))
	}
	for index, color := range theme.Terminal.ANSI {
		if err := validateThemeColor(fmt.Sprintf("terminal.ansi[%d]", index), color); err != nil {
			return err
		}
	}

	for index, color := range theme.Shelves {
		if err := validateThemeColor(fmt.Sprintf("shelves[%d]", index), color); err != nil {
			return err
		}
	}
	if len(theme.Identity) == 0 {
		return errors.New("identity must have at least 1 colour")
	}
	for index, color := range theme.Identity {
		if err := validateThemeColor(fmt.Sprintf("identity[%d]", index), color); err != nil {
			return err
		}
	}

	for index, name := range theme.Art {
		if !isThemeArtName(name) {
			return fmt.Errorf("art[%d] %q is not a valid art file name", index, name)
		}
	}

	return nil
}

func validateThemeColor(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s is missing", field)
	}
	if !themeColorPattern.MatchString(value) {
		return fmt.Errorf("%s %q is not #rrggbb or #rrggbbaa", field, value)
	}
	return nil
}
