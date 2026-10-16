package credentials

import (
	"crypto/rand"
	"errors"
	"fmt"
	mathrand "math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

const (
	writeLockTimeout = 3 * time.Second
	writeLockRetry   = 20 * time.Millisecond
	writeLockName    = ".write-lock"
)

// withWriteLock shares secret stash's mkdir protocol. The timeout is passed by
// value so tests can use short deadlines without changing concurrent writers.
func withWriteLock(root *os.Root, path string, timeout time.Duration, update func() error) error {
	release, err := acquireWriteLock(root, path, timeout)
	if err != nil {
		return err
	}
	defer release()
	return update()
}

func acquireWriteLock(root *os.Root, path string, timeout time.Duration) (func(), error) {
	lockPath := filepath.Join(filepath.Dir(path), writeLockName)
	token := strconv.Itoa(os.Getpid()) + "." + rand.Text() + "\n"
	if err := waitForWriteLock(lockPath, timeout, func() error {
		return root.Mkdir(writeLockName, 0700)
	}); err != nil {
		return nil, err
	}
	lock, err := root.OpenRoot(writeLockName)
	if err != nil {
		return nil, fmt.Errorf("open credentials write lock %s; inspect and manually remove it if unused (never auto-stolen)", lockPath)
	}
	info, err := lock.Stat(".")
	if err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("inspect credentials write lock %s", lockPath)
	}
	release := func() {
		current, statErr := root.Lstat(writeLockName)
		owner, readErr := readFile(lock, "token")
		if statErr != nil || !os.SameFile(info, current) || readErr != nil || string(owner) != token {
			_ = lock.Close()
			return
		}
		_ = lock.Remove("token")
		_ = lock.Remove("pid")
		// Close our handle before removing so it cannot delay deletion.
		_ = lock.Close()
		_ = root.Remove(writeLockName)
	}
	initialized := false
	defer func() {
		if !initialized {
			// We created this lock, so remove it even if the token write failed.
			_ = lock.Remove("token")
			_ = lock.Remove("pid")
			_ = lock.Close()
			_ = root.Remove(writeLockName)
		}
	}()
	for _, entry := range []struct{ name, value string }{{"token", token}, {"pid", strconv.Itoa(os.Getpid()) + "\n"}} {
		if err = writeLockEntry(lock, entry.name, entry.value); err != nil {
			return nil, fmt.Errorf("initialize credentials write lock %s; inspect and manually remove it if unused (never auto-stolen)", lockPath)
		}
	}
	initialized = true
	return release, nil
}

// waitForWriteLock retries the atomic mkdir operation. The callback lets tests
// isolate polling from real filesystem delays when using fake time.
func waitForWriteLock(lockPath string, timeout time.Duration, mkdir func() error) error {
	deadline := time.Now().Add(timeout)
	for {
		err := mkdir()
		if err == nil {
			return nil
		}
		// A Windows permission denial may be transient, so retry it until the
		// deadline before reporting directory permission guidance.
		retry, permission := lockRetry(runtime.GOOS, err)
		if !retry {
			return fmt.Errorf("create credentials write lock %s; check directory permissions", lockPath)
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			if permission {
				return fmt.Errorf("create credentials write lock %s; check directory permissions", lockPath)
			}
			return fmt.Errorf("credentials write lock timed out after %s; verify its PID is stopped, then manually remove %s (never auto-stolen)", timeout, lockPath)
		}
		// Jitter separates contenders instead of waking them in a convoy. A
		// 10-20 ms delay budgets at most 300 ms of polling gaps for 16 writers
		// instead of 1.5 s, leaving more of the 3 s timeout for filesystem work.
		delay := writeLockRetry/2 + time.Duration(mathrand.Int64N(int64(writeLockRetry/2)))
		time.Sleep(min(delay, remaining))
	}
}

func lockRetry(goos string, err error) (retry, permission bool) {
	permission = goos == "windows" && errors.Is(err, os.ErrPermission)
	return errors.Is(err, os.ErrExist) || permission, permission
}

func writeLockEntry(lock *os.Root, name, value string) error {
	file, err := createPrivateFile(lock, name)
	if err != nil {
		return err
	}
	_, err = file.WriteString(value)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}
