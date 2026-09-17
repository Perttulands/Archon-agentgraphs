package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func runPeerCommand(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return runPeer(ctx, store, args, stdout, stderr)
}

// Peer commands are local agent operations on an existing attempt journal.
// They do not admit runs, deliver prompts or manage seats.
func runPeer(ctx context.Context, store *formations.Store, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: archon --workspace <state-dir> peer <read|wait|post|propose|ack> --run <id> --node <id> --attempt <n> [--slot <id>]")
		return 2
	}
	command := args[0]
	if command != "read" && command != "wait" && command != "post" && command != "propose" && command != "ack" {
		fmt.Fprintf(stderr, "unknown peer command %q\n", command)
		return 2
	}
	fs := flag.NewFlagSet("peer "+command, flag.ContinueOnError)
	fs.SetOutput(stderr)
	runID := fs.String("run", "", "run ID")
	nodeID := fs.String("node", "", "formation ID")
	attempt := fs.Int("attempt", 0, "formation attempt")
	slotID := fs.String("slot", "", "participant slot ID, required for writes")
	text := fs.String("text", "", "message or proposed final output")
	textFile := fs.String("text-file", "", "UTF-8 file containing the complete message or proposal")
	proposal := fs.Int("proposal", 0, "exact proposal sequence for acknowledgement or dissent")
	after := fs.Int("after", 0, "wait until a later sequence or final state")
	dissent := fs.Bool("dissent", false, "post an objection to --proposal; a new proposal is required before agreement")
	fs.Bool("json", true, "responses are always JSON")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if fs.NArg() != 0 || *runID == "" || *nodeID == "" || *attempt < 1 || *after < 0 {
		fmt.Fprintln(stderr, "peer commands require --run, --node and positive --attempt, with no positional arguments")
		return 2
	}
	seen := map[string]bool{}
	fs.Visit(func(value *flag.Flag) { seen[value.Name] = true })
	writes := command == "post" || command == "propose" || command == "ack"
	needsText := command == "post" || command == "propose"
	if writes && *slotID == "" || needsText && !seen["text"] && !seen["text-file"] || seen["text"] && seen["text-file"] || !needsText && (seen["text"] || seen["text-file"]) || *dissent && command != "post" || seen["after"] && command != "wait" {
		fmt.Fprintln(stderr, "post/propose require --slot and exactly one of --text or --text-file; ack requires --slot; --after belongs to wait; --dissent belongs to post")
		return 2
	}
	if (command == "ack" || *dissent) && *proposal <= 0 || command != "ack" && !*dissent && seen["proposal"] {
		fmt.Fprintln(stderr, "ack and post --dissent require --proposal <sequence>; other commands do not accept it")
		return 2
	}
	if seen["text-file"] {
		file, err := os.Open(*textFile)
		if err != nil {
			return fail(stderr, err)
		}
		defer file.Close()
		raw, err := io.ReadAll(io.LimitReader(file, formations.PeerMessageMaxBytes+1))
		if err != nil {
			return fail(stderr, err)
		}
		if len(raw) > formations.PeerMessageMaxBytes {
			return fail(stderr, errors.New("peer text file exceeds message byte limit"))
		}
		*text = string(raw)
	}
	id := formations.PeerConversationID{RunID: *runID, NodeID: *nodeID, Attempt: *attempt}
	var state *formations.PeerConversation
	var err error
	switch command {
	case "read":
		state, err = store.ReadPeerConversation(id)
	case "wait":
		state, err = store.WaitPeerConversation(ctx, id, *after)
	default:
		kind := map[string]string{"post": "message", "propose": "proposal", "ack": "ack"}[command]
		if *dissent {
			kind = "dissent"
		}
		state, err = store.AppendPeerConversation(id, formations.PeerAppendRequest{SlotID: *slotID, Kind: kind, Text: *text, ProposalSeq: *proposal})
	}
	// A canceled or expired wait still returns its last readable evidence.
	if state != nil {
		if result := writeJSON(stdout, state); result != 0 {
			return result
		}
	}
	if err != nil {
		return fail(stderr, err)
	}
	return 0
}
