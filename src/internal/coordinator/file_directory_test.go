package coordinator

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// CHROTE directory behavior: real paths, directory-first listing, symlink folders.
func TestFileDirectoryListsHostPaths(t *testing.T) {
	c, _, _ := fixture(t)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "z folder"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("proof"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "z folder"), filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	c.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/files/directory?path="+url.QueryEscape(root), nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct {
		Data struct {
			Items []struct {
				Name  string `json:"name"`
				Path  string `json:"path"`
				IsDir bool   `json:"isDir"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	items := result.Data.Items
	if len(items) != 3 || !items[0].IsDir || !items[1].IsDir || items[2].Name != "a.txt" || items[2].Path != filepath.Join(root, "a.txt") {
		t.Fatalf("items %+v", items)
	}
}
