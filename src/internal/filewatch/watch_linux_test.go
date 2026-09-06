package filewatch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReportsExistingAndNewDirectoryWrites(t *testing.T) {
	root := t.TempDir()
	w, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for _, name := range []string{"first.jsonl", "new/sub/second.jsonl"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("event\n"), 0600); err != nil {
			t.Fatal(err)
		}
		deadline := time.NewTimer(3 * time.Second)
		found := false
		for !found {
			select {
			case event := <-w.Events:
				if event.Err != nil {
					t.Fatal(event.Err)
				}
				found = event.Path == path
			case <-deadline.C:
				t.Fatalf("no event for %s", path)
			}
		}
		deadline.Stop()
	}
}
