package filewatch

import (
	"fmt"
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

// Peer conversations watch an already-open directory through /proc. Keeping
// the trailing /. makes WalkDir traverse it instead of stopping at the link.
func TestReportsDescriptorDirectoryWrites(t *testing.T) {
	root := t.TempDir()
	dir, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	w, err := New(fmt.Sprintf("/proc/self/fd/%d/.", dir.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := os.WriteFile(filepath.Join(root, "conversation.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-w.Events:
		if e.Err != nil || filepath.Base(e.Path) != "conversation.json" {
			t.Fatalf("event = %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no descriptor-directory write event")
	}
}
