package formations

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"sync"
)

// ledgerCache keeps runs' parsed ledgers, so a read parses only the lines
// appended since the store last read the ledger (archon-o7p.19). A run's
// waiters, its streams and its own appends all read the ledger; parsing the
// whole file on each read made fifty waiters on a large ledger take seconds
// and held the run's appends up behind them.
//
// Ledgers only grow by whole lines. A read still checks the bytes the cache
// covers against a checksum, so a replaced, shrunk or rewritten ledger is
// parsed again from the start and never read through a stale cache. The
// events a read returns are shared: callers must not change them, and their
// slice cannot grow into the cache. The cache holds the most recently read
// ledgers.
type ledgerCache struct {
	mu      sync.Mutex
	entries map[string]*ledgerEntry
	clock   uint64
}

type ledgerEntry struct {
	mu       sync.Mutex
	lastUsed uint64
	// size is the byte length the events cover, always the end of a line, and
	// sum the checksum of those bytes.
	size   int64
	sum    uint32
	events []RunEvent
}

// ledgerCacheRuns is how many ledgers the cache keeps.
const ledgerCacheRuns = 64

var ledgerChecksum = crc32.MakeTable(crc32.Castagnoli)

// entry is the cache entry for a ledger path, made on first use; the least
// recently used ledger leaves when the cache is full.
func (c *ledgerCache) entry(key string) *ledgerEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]*ledgerEntry{}
	}
	c.clock++
	entry := c.entries[key]
	if entry == nil {
		if len(c.entries) >= ledgerCacheRuns {
			oldest := ""
			for candidate, cached := range c.entries {
				if oldest == "" || cached.lastUsed < c.entries[oldest].lastUsed {
					oldest = candidate
				}
			}
			delete(c.entries, oldest)
		}
		entry = &ledgerEntry{}
		c.entries[key] = entry
	}
	entry.lastUsed = c.clock
	return entry
}

// readLedger returns the ledger's events, parsing only what was appended
// since the last read. A caller holding the ledger lock reads strictly: bytes
// after the last line end are parsed as an event, as a full read always has.
// A caller reading without the lock sees whole lines only, since an append may
// be half written.
func (s *Store) readLedger(ledger *runLedgerHandle, runID string, locked bool) ([]RunEvent, error) {
	if ledger == nil || ledger.file == nil {
		return nil, ErrRunLedgerInvalid
	}
	info, err := ledger.file.Stat()
	if err != nil {
		return nil, fmt.Errorf("%w: stat run ledger: %v", ErrRunLedgerInvalid, err)
	}
	raw := make([]byte, info.Size())
	read, err := ledger.file.ReadAt(raw, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: read run ledger: %v", ErrRunLedgerInvalid, err)
	}
	raw = raw[:read]
	whole := bytes.LastIndexByte(raw, '\n') + 1
	if whole < len(raw) && locked {
		// A torn last line: read the ledger as a whole, which fails on it.
		return readRunEventsFrom(ledger.file, runID)
	}
	entry := s.ledgers.entry(ledger.path)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if int64(whole) < entry.size || crc32.Checksum(raw[:entry.size], ledgerChecksum) != entry.sum {
		entry.size, entry.sum, entry.events = 0, 0, nil
	}
	if fresh := raw[entry.size:whole]; len(fresh) > 0 {
		added := make([]RunEvent, 0, bytes.Count(fresh, []byte{'\n'}))
		err := readLedgerLinesFrom(bytes.NewReader(fresh), runID, len(entry.events), func(line []byte) error {
			var event RunEvent
			if err := json.Unmarshal(line, &event); err != nil {
				return err
			}
			added = append(added, event)
			return nil
		})
		if err != nil {
			entry.size, entry.sum, entry.events = 0, 0, nil
			return nil, fmt.Errorf("%w: %v", ErrRunLedgerInvalid, err)
		}
		entry.events = append(entry.events[:len(entry.events):len(entry.events)], added...)
		entry.sum = crc32.Update(entry.sum, ledgerChecksum, fresh)
		entry.size = int64(whole)
	}
	if len(entry.events) == 0 {
		return nil, fmt.Errorf("%w: ledger is empty", ErrRunLedgerInvalid)
	}
	return entry.events[:len(entry.events):len(entry.events)], nil
}
