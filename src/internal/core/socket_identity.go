package core

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// SocketIdentity pins both the socket inode and its kernel-authenticated server
// process. tmux changes socket ctime on attach/detach, so ctime is not identity.
// Boot and process start time prevent PID or inode reuse from adopting a server.
func SocketIdentity(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return "", fmt.Errorf("tmux socket is unavailable")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != filepath.Clean(path) {
		return "", fmt.Errorf("tmux socket path is not stable")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("socket inode identity unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	raw, err := conn.(*net.UnixConn).SyscallConn()
	if err != nil {
		return "", err
	}
	var cred *syscall.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return "", err
	}
	if credErr != nil {
		return "", credErr
	}
	process, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", cred.Pid))
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(process)[strings.LastIndex(string(process), ")")+1:])
	if len(fields) < 20 {
		return "", fmt.Errorf("server process start identity unavailable")
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%d:%d:%s:%s", stat.Dev, stat.Ino, cred.Pid, cred.Uid, fields[19], boot)))), nil
}
