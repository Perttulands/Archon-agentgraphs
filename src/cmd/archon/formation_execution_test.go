package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func TestFormationExecutionCLIAuthorsAndClearsDuration(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	path := store.BoardPath("budget")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("schema = 1\nid = \"brd_budget\"\nslug = \"budget\"\ntitle = \"Budget\"\nrev = 1\n[[formation]]\nid = \"fmn_work\"\ntype = \"solo\"\ntitle = \"Work\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, seconds := range []int{41, 0} {
		var stdout, stderr bytes.Buffer
		code := runFormationSetExecution(store, []string{"budget", "fmn_work", "--timeout-seconds", strconv.Itoa(seconds), "--json"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("code=%d stderr=%s", code, stderr.String())
		}
		board, err := store.ReadBoard("budget")
		if err != nil {
			t.Fatal(err)
		}
		policy := board.Formations[0].Execution
		if seconds == 0 && policy != nil || seconds > 0 && (policy == nil || policy.TimeoutSeconds != seconds) {
			t.Fatalf("policy=%+v", policy)
		}
	}
}
