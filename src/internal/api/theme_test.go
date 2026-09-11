package api

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestThemeDefaultAndConfiguredRead(t *testing.T) {
	if err := validateTheme(themeDefaultJSON); err != nil {
		t.Fatal(err)
	}
	if _, err := NewThemeHandler("relative.json"); err == nil {
		t.Fatal("relative path accepted")
	}
	path := filepath.Join(t.TempDir(), "theme.json")
	for _, configured := range []string{"", path} {
		h, err := NewThemeHandler(configured)
		if err != nil {
			t.Fatal(err)
		}
		if configured != "" {
			if err := os.WriteFile(path, themeDefaultJSON, 0600); err != nil {
				t.Fatal(err)
			}
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/theme", nil))
		if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), themeDefaultJSON) {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	h, _ := NewThemeHandler(path)
	cases := []struct {
		body []byte
		code string
	}{
		{[]byte(`{"schema":1}`), "INVALID_THEME"},
		{bytes.Replace(themeDefaultJSON, []byte(`"schema": 1`), []byte(`"schema": 2`), 1), "INVALID_THEME"},
		{bytes.Replace(themeDefaultJSON, []byte("#0f0f0f"), []byte("red"), 1), "INVALID_THEME"},
		{nil, "THEME_UNREADABLE"},
	}
	for _, tc := range cases {
		if tc.body == nil {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(path, tc.body, 0600); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/theme", nil))
		if w.Code != 500 || !bytes.Contains(w.Body.Bytes(), []byte(tc.code)) {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	// The same handler observes a repaired host file without a daemon restart.
	if err := os.WriteFile(path, themeDefaultJSON, 0600); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/theme", nil))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
}
