package sysproxy

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type fakeProxy struct {
	current    *ProxySettings
	enableErr  error
	restoreErr error
	enables    int
	restores   int
}

func (f *fakeProxy) operations() proxyOperations {
	return proxyOperations{
		supported: func() bool { return true },
		backup:    func() (*ProxySettings, error) { return cloneSettings(f.current), nil },
		current:   func() (*ProxySettings, error) { return cloneSettings(f.current), nil },
		enable: func(address string) error {
			f.enables++
			if f.enableErr != nil {
				return f.enableErr
			}
			f.current = &ProxySettings{
				ProxyEnable:   1,
				ProxyServer:   address,
				ProxyOverride: managedOverride,
				HasServer:     true,
				HasOverride:   true,
			}
			return nil
		},
		restore: func(settings *ProxySettings) error {
			f.restores++
			if f.restoreErr != nil {
				return f.restoreErr
			}
			f.current = cloneSettings(settings)
			return nil
		},
	}
}

func TestManagerBeginRestoreTransaction(t *testing.T) {
	original := &ProxySettings{ProxyEnable: 0, ProxyServer: "old:8080", HasServer: true}
	fake := &fakeProxy{current: cloneSettings(original)}
	path := filepath.Join(t.TempDir(), "recovery.json")
	manager := newManager(path, fake.operations())

	if err := manager.Begin("127.0.0.1:1080"); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if !manager.Active() {
		t.Fatal("manager should be active")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("journal was not persisted: %v", err)
	}
	if fake.current.ProxyEnable != 1 || fake.current.ProxyServer != "127.0.0.1:1080" {
		t.Fatalf("proxy was not enabled: %+v", fake.current)
	}

	if err := manager.Restore(); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if manager.Active() || !settingsEqual(fake.current, original) {
		t.Fatalf("proxy was not restored: %+v", fake.current)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("journal still exists: %v", err)
	}
}

func TestManagerRecoversUncleanExit(t *testing.T) {
	original := &ProxySettings{ProxyEnable: 0}
	fake := &fakeProxy{current: cloneSettings(original)}
	path := filepath.Join(t.TempDir(), "recovery.json")
	first := newManager(path, fake.operations())
	if err := first.Begin("127.0.0.1:1080"); err != nil {
		t.Fatal(err)
	}

	second := newManager(path, fake.operations())
	recovered, err := second.Recover()
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if !recovered || !settingsEqual(fake.current, original) {
		t.Fatalf("recovery result=%v current=%+v", recovered, fake.current)
	}
}

func TestManagerRefusesExternalChanges(t *testing.T) {
	fake := &fakeProxy{current: &ProxySettings{ProxyEnable: 0}}
	path := filepath.Join(t.TempDir(), "recovery.json")
	first := newManager(path, fake.operations())
	if err := first.Begin("127.0.0.1:1080"); err != nil {
		t.Fatal(err)
	}
	fake.current.ProxyServer = "external:3128"

	second := newManager(path, fake.operations())
	if _, err := second.Recover(); err == nil {
		t.Fatal("external change should cause a recovery conflict")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("conflicting journal should be preserved: %v", err)
	}
}

func TestManagerKeepsFailedRollbackRetryable(t *testing.T) {
	fake := &fakeProxy{
		current:    &ProxySettings{ProxyEnable: 0},
		enableErr:  errors.New("enable failed"),
		restoreErr: errors.New("restore failed"),
	}
	manager := newManager(filepath.Join(t.TempDir(), "recovery.json"), fake.operations())
	if err := manager.Begin("127.0.0.1:1080"); err == nil {
		t.Fatal("Begin should report enable and rollback failure")
	}
	if !manager.Active() {
		t.Fatal("failed rollback must remain active for retry")
	}
	fake.restoreErr = nil
	if err := manager.Restore(); err != nil {
		t.Fatalf("retry Restore: %v", err)
	}
	if manager.Active() {
		t.Fatal("successful retry should clear active state")
	}
}

func cloneSettings(settings *ProxySettings) *ProxySettings {
	if settings == nil {
		return nil
	}
	copy := *settings
	return &copy
}
