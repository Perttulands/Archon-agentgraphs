package formations

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

// The daemon's own state (run ledgers, snapshots, definitions, peer journals)
// is read and written relative to opened directory descriptors, so atomic
// replacement, appends and locks act on the directory that was opened.

const (
	// runRecordMaxBytes bounds a run snapshot or bindings record.
	runRecordMaxBytes = int64(1 << 20)
	// runEventMaxBytes bounds one ledger event line.
	runEventMaxBytes = 1 << 20
	// directoryBatchSize is how many names a directory scan reads at a time.
	directoryBatchSize = 128
)

// validPathComponent accepts one path element: no separators, NUL, "." or "..".
func validPathComponent(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsRune(name, 0) && !strings.ContainsRune(name, filepath.Separator)
}

// openAbsoluteDirectory opens an absolute clean directory path one component
// at a time from the filesystem root.
func openAbsoluteDirectory(root string) (*os.File, error) {
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, fmt.Errorf("directory %q must be an absolute clean path", root)
	}
	fd, err := syscall.Open(string(filepath.Separator), syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_DIRECTORY, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: string(filepath.Separator), Err: err}
	}
	current := os.NewFile(uintptr(fd), string(filepath.Separator))
	if current == nil {
		_ = syscall.Close(fd)
		return nil, errors.New("could not open the filesystem root")
	}
	trimmed := strings.TrimPrefix(root, string(filepath.Separator))
	if trimmed == "" {
		return current, nil
	}
	openedPath := string(filepath.Separator)
	for _, component := range strings.Split(trimmed, string(filepath.Separator)) {
		next, err := openDirectoryAt(current, component)
		if err != nil {
			current.Close()
			return nil, &os.PathError{Op: "open", Path: filepath.Join(openedPath, component), Err: err}
		}
		current.Close()
		current = next
		openedPath = filepath.Join(openedPath, component)
	}
	return current, nil
}

// openDirectoryAt opens one child directory of parent.
func openDirectoryAt(parent *os.File, name string) (*os.File, error) {
	if parent == nil || !validPathComponent(name) {
		return nil, &os.PathError{Op: "openat", Path: name, Err: syscall.EINVAL}
	}
	fd, err := syscall.Openat(int(parent.Fd()), name, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_DIRECTORY, 0)
	if err != nil {
		return nil, &os.PathError{Op: "openat", Path: name, Err: err}
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		_ = syscall.Close(fd)
		return nil, errors.New("could not open directory")
	}
	return file, nil
}

// readDirectoryNameBatch reads up to batchSize names, sorted, and reports
// whether the directory is exhausted.
func readDirectoryNameBatch(directory *os.File, batchSize int) ([]string, bool, error) {
	if directory == nil || batchSize <= 0 {
		return nil, false, errors.New("invalid directory scan")
	}
	names, err := directory.Readdirnames(batchSize)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, false, err
	}
	sort.Strings(names)
	return names, errors.Is(err, io.EOF), nil
}

// ledgerEnvelope is what every ledger line must carry to be a valid event.
type ledgerEnvelope struct {
	Seq       *int   `json:"seq"`
	Timestamp string `json:"ts"`
	RunID     string `json:"runId"`
	Type      string `json:"type"`
	Actor     string `json:"actor"`
}

// readLedgerLines streams a run ledger and hands each valid event line to
// visit. A valid ledger is non-empty UTF-8 JSON, one event per line, with
// sequence numbers 1, 2, 3 and so on, RFC 3339 timestamps, an actor and type
// on every event, and one run ID throughout (expectedRunID when given).
func readLedgerLines(input io.Reader, expectedRunID string, visit func(line []byte) error) error {
	reader := bufio.NewReaderSize(input, runEventMaxBytes+1)
	runID := expectedRunID
	count := 0
	for {
		line, err := readLedgerLine(reader)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if !utf8.Valid(line) || len(bytes.TrimSpace(line)) == 0 {
			return errors.New("ledger event is blank or not UTF-8")
		}
		count++
		var event ledgerEnvelope
		if err := json.Unmarshal(line, &event); err != nil {
			return fmt.Errorf("ledger event %d: %w", count, err)
		}
		if event.Seq == nil || event.Timestamp == "" || event.RunID == "" || event.Type == "" || event.Actor == "" {
			return fmt.Errorf("ledger event %d is missing seq, ts, runId, type or actor", count)
		}
		if *event.Seq != count {
			return fmt.Errorf("ledger event %d has seq %d", count, *event.Seq)
		}
		if _, err := time.Parse(time.RFC3339Nano, event.Timestamp); err != nil {
			return fmt.Errorf("ledger event %d: %w", count, err)
		}
		if runID == "" {
			runID = event.RunID
		}
		if event.RunID != runID {
			return fmt.Errorf("ledger event %d belongs to run %q, not %q", count, event.RunID, runID)
		}
		if err := visit(line); err != nil {
			return err
		}
	}
	if count == 0 {
		return errors.New("ledger is empty")
	}
	return nil
}

func readLedgerLine(reader *bufio.Reader) ([]byte, error) {
	line, err := reader.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		return nil, errors.New("ledger event exceeds the byte limit")
	}
	if errors.Is(err, io.EOF) {
		if len(line) == 0 {
			return nil, io.EOF
		}
		return line, nil
	}
	if err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(line, []byte{'\n'}), nil
}
