package core

import "testing"

func TestTmuxBin_PrefersPinnedBinary(t *testing.T) {
	t.Setenv("CHROTE_TMUX_BIN", " /opt/test-tools/bin/tmux ")
	if got := TmuxBin(); got != "/opt/test-tools/bin/tmux" {
		t.Fatalf("TmuxBin() = %q, want the pinned CHROTE_TMUX_BIN path", got)
	}
}

func TestTmuxBin_FallsBackToPathLookup(t *testing.T) {
	t.Setenv("CHROTE_TMUX_BIN", "")
	if got := TmuxBin(); got != "tmux" {
		t.Fatalf("TmuxBin() = %q, want %q when CHROTE_TMUX_BIN is unset", got, "tmux")
	}
}
