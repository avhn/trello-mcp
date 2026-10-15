package credentials

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConcurrentSavesPreserveKeys(t *testing.T) {
	path := filepath.Join(credentialTestDir(t), "store", "test.secret")
	if err := Save(path, synthetic()); err != nil {
		t.Fatal(err)
	}
	root, err := openDir(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	const writers = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range writers {
		wg.Go(func() {
			<-start
			if err := Save(path, synthetic()); err != nil {
				t.Error(err)
			}
		})
		wg.Go(func() {
			<-start
			// Model secret stash: acquire the same directory lock before reading,
			// then atomically add an unrelated key to the same dotenv file.
			err := withWriteLock(root, path, writeLockTimeout, func() error {
				data, err := readFile(root, filepath.Base(path))
				if err != nil {
					return err
				}
				return replace(root, filepath.Base(path), append(data, fmt.Appendf(nil, "OTHER_%d=synthetic\n", i)...))
			})
			if err != nil {
				t.Error(err)
			}
		})
	}
	close(start)
	wg.Wait()
	data, err := readFile(root, filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}
	for i := range writers {
		if strings.Count(string(data), fmt.Sprintf("OTHER_%d=synthetic\n", i)) != 1 {
			t.Errorf("concurrent save lost unrelated key %d", i)
		}
	}
	if got, err := Load(path); err != nil || got != synthetic() {
		t.Fatal("concurrent updates lost managed credentials")
	}
	assertLockReleased(t, path)
}

func TestHeldWriteLock(t *testing.T) {
	path := filepath.Join(credentialTestDir(t), "store", "test.secret")
	if err := Save(path, synthetic()); err != nil {
		t.Fatal(err)
	}
	root, err := openDir(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	release, err := acquireWriteLock(root, path, writeLockTimeout)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	lockPath := filepath.Join(filepath.Dir(path), writeLockName)
	before, err := os.ReadFile(filepath.Join(lockPath, "token"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := os.ReadFile(filepath.Join(lockPath, "pid"))
	if err != nil || string(pid) != strconv.Itoa(os.Getpid())+"\n" {
		t.Fatal("lock PID missing or incorrect")
	}
	if runtime.GOOS != "windows" {
		for _, name := range []string{"token", "pid"} {
			info, err := os.Stat(filepath.Join(lockPath, name))
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatal("lock metadata is not 0600")
			}
		}
	}
	// Pure reads do not acquire or wait on the write lock.
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []func() error{
		func() error { return save(path, synthetic(), 20*time.Millisecond) },
		func() error { return logout(path, 20*time.Millisecond) },
	} {
		err := operation()
		if err == nil || !strings.Contains(err.Error(), "timed out") || !strings.Contains(err.Error(), lockPath) || !strings.Contains(err.Error(), "never auto-stolen") || !strings.Contains(err.Error(), "manually remove") {
			t.Fatalf("missing actionable lock timeout: %v", err)
		}
		after, err := os.ReadFile(filepath.Join(lockPath, "token"))
		if err != nil || string(after) != string(before) {
			t.Fatal("timed-out writer removed or changed a held lock")
		}
	}
}

func assertLockReleased(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(filepath.Dir(path), writeLockName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("write lock remained: %v", err)
	}
}

func TestWriteLockReleasedAfterFailure(t *testing.T) {
	path := filepath.Join(credentialTestDir(t), "store", "test.secret")
	if err := Save(path, synthetic()); err != nil {
		t.Fatal(err)
	}
	root, err := openDir(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	// A nonregular target fails after acquiring the lock in both operations.
	if err := root.Remove(filepath.Base(path)); err != nil {
		t.Fatal(err)
	}
	if err := root.Mkdir(filepath.Base(path), 0700); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []func() error{
		func() error { return Save(path, synthetic()) },
		func() error { return Logout(path) },
	} {
		if err := operation(); err == nil {
			t.Fatal("nonregular target accepted")
		}
		assertLockReleased(t, path)
	}
	func() {
		defer func() {
			if recover() != "synthetic panic" {
				t.Error("update did not propagate panic")
			}
		}()
		_ = withWriteLock(root, path, writeLockTimeout, func() error { panic("synthetic panic") })
	}()
	assertLockReleased(t, path)
}

func TestWriteLockReleaseChecksToken(t *testing.T) {
	path := filepath.Join(credentialTestDir(t), "store", "test.secret")
	root, err := openDir(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	err = withWriteLock(root, path, writeLockTimeout, func() error {
		return os.WriteFile(filepath.Join(filepath.Dir(path), writeLockName, "token"), []byte("another-owner\n"), 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(path), writeLockName, "token"))
	if err != nil || string(data) != "another-owner\n" {
		t.Fatal("release removed another owner's lock")
	}
}

func TestLogoutChangedAfterRead(t *testing.T) {
	for _, unrelated := range []string{"OTHER=synthetic\n", ""} {
		t.Run(fmt.Sprintf("unrelated=%t", unrelated != ""), func(t *testing.T) {
			path := filepath.Join(credentialTestDir(t), "store", "test.secret")
			if err := Save(path, synthetic()); err != nil {
				t.Fatal(err)
			}
			root, err := openDir(path, false)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = root.Close() }()
			err = withWriteLock(root, path, writeLockTimeout, func() error {
				name := filepath.Base(path)
				original, err := readFile(root, name)
				if err != nil {
					return err
				}
				// Simulate an external writer bypassing the protocol after logout's
				// first read, including a changed file that will still become empty.
				changed := "TRELLO_API_KEY=changed-key\nTRELLO_TOKEN=changed-token\n" + unrelated
				if err := replace(root, name, []byte(changed)); err != nil {
					return err
				}
				return removeManaged(root, name, original)
			})
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != unrelated {
				t.Fatal("logout deleted a changed file or lost unrelated content")
			}
		})
	}
}
