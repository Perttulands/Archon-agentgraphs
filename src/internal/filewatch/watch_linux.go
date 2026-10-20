// Package filewatch reports filesystem changes without timed polling.
package filewatch

import (
	"encoding/binary"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type Event struct {
	Path string
	// Removed reports a file or subtree leaving the watched tree.
	Removed bool
	Err     error
}
type Watcher struct {
	file   *os.File
	Events chan Event
	done   chan struct{}
}

// New watches an existing directory tree and any directories subsequently added.
func New(root string) (*Watcher, error) {
	// Preserve a trailing /. used to traverse descriptor-backed directories.
	watchedRoot := filepath.Clean(root)
	fd, err := syscall.InotifyInit1(syscall.IN_CLOEXEC | syscall.IN_NONBLOCK)
	if err != nil {
		return nil, err
	}
	w := &Watcher{file: os.NewFile(uintptr(fd), "inotify"), Events: make(chan Event, 128), done: make(chan struct{})}
	paths := map[int]string{}
	var add func(string) error
	add = func(root string) error {
		return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() {
				return nil
			}
			wd, err := syscall.InotifyAddWatch(fd, path, syscall.IN_CREATE|syscall.IN_MODIFY|syscall.IN_MOVED_TO|syscall.IN_DELETE|syscall.IN_MOVED_FROM|syscall.IN_DELETE_SELF|syscall.IN_MOVE_SELF)
			if err == nil {
				paths[wd] = path
			}
			return err
		})
	}
	if err := add(root); err != nil {
		w.file.Close()
		return nil, err
	}
	go func() {
		defer close(w.Events)
		emit := func(e Event) bool {
			select {
			case w.Events <- e:
				return true
			case <-w.done:
				return false
			}
		}
		buf := make([]byte, 64*1024)
		for {
			n, err := w.file.Read(buf)
			if err != nil {
				if !errors.Is(err, os.ErrClosed) {
					emit(Event{Err: err})
				}
				return
			}
			for off := 0; off+syscall.SizeofInotifyEvent <= n; {
				wd := int(int32(binary.NativeEndian.Uint32(buf[off:])))
				mask := binary.NativeEndian.Uint32(buf[off+4:])
				length := int(binary.NativeEndian.Uint32(buf[off+12:]))
				end := off + syscall.SizeofInotifyEvent + length
				if end > n {
					emit(Event{Err: errors.New("truncated filesystem event")})
					return
				}
				name := strings.TrimRight(string(buf[off+syscall.SizeofInotifyEvent:end]), "\x00")
				directory, known := paths[wd]
				path := filepath.Join(directory, name)
				off = end
				if mask&syscall.IN_Q_OVERFLOW != 0 {
					emit(Event{Err: errors.New("filesystem event overflow")})
					return
				}
				if !known || mask&syscall.IN_IGNORED != 0 {
					continue
				}
				if mask&(syscall.IN_DELETE_SELF|syscall.IN_MOVE_SELF|syscall.IN_DELETE|syscall.IN_MOVED_FROM) != 0 {
					if path == watchedRoot {
						emit(Event{Err: errors.New("transcript root moved or removed")})
						return
					}
					// A sibling conversation leaving the shared root is discovery
					// bookkeeping, not failure of every seat observing the root.
					// Also stop watching descendants moved outside the tree.
					for watched, candidate := range paths {
						candidate = filepath.Clean(candidate)
						if candidate == path || strings.HasPrefix(candidate, path+string(filepath.Separator)) {
							syscall.InotifyRmWatch(fd, uint32(watched))
							delete(paths, watched)
						}
					}
					if !emit(Event{Path: path, Removed: true}) {
						return
					}
					continue
				}
				if mask&syscall.IN_ISDIR != 0 {
					if err := add(path); err != nil {
						if errors.Is(err, os.ErrNotExist) {
							continue
						}
						emit(Event{Err: err})
						return
					}
					// A newly moved directory may already contain a completed transcript.
					filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
						if err == nil && !d.IsDir() {
							emit(Event{Path: p})
						}
						return err
					})
				} else if !emit(Event{Path: path}) {
					return
				}
			}
		}
	}()
	return w, nil
}

func (w *Watcher) Close() error { close(w.done); return w.file.Close() }
