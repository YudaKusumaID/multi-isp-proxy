package instance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireEnforcesSingleInstanceAndReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "instance.lock")
	first, err := Acquire(path)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	if _, err := Acquire(path); err == nil {
		t.Fatal("second Acquire should fail while this process owns the lock")
	}
	if err := first.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	second, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire after release: %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatalf("second Release: %v", err)
	}
}

func TestAcquireRemovesStaleLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "instance.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(file).Encode(lockRecord{PID: 1 << 30, Token: "stale"}); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	lock, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire stale lock: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
}
