package sysproxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const journalVersion = 1

// Manager makes system-proxy changes transactional and recoverable after an
// unclean process exit.
type Manager struct {
	mu     sync.Mutex
	path   string
	active bool
	backup *ProxySettings
	ops    proxyOperations
}

type proxyOperations struct {
	supported func() bool
	backup    func() (*ProxySettings, error)
	current   func() (*ProxySettings, error)
	enable    func(string) error
	restore   func(*ProxySettings) error
}

type recoveryJournal struct {
	Version         int            `json:"version"`
	PID             int            `json:"pid"`
	CreatedAt       time.Time      `json:"created_at"`
	ManagedAddress  string         `json:"managed_address"`
	ManagedOverride string         `json:"managed_override"`
	Backup          *ProxySettings `json:"backup"`
}

// DefaultJournalPath returns the per-user crash recovery journal location.
func DefaultJournalPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(configDir, "multi-isp-proxy", "proxy-recovery.json"), nil
}

// NewManager creates a system-proxy transaction manager.
func NewManager(path string) *Manager {
	return newManager(path, proxyOperations{
		supported: Supported,
		backup:    Backup,
		current:   Current,
		enable:    Enable,
		restore:   Restore,
	})
}

func newManager(path string, ops proxyOperations) *Manager {
	return &Manager{path: path, ops: ops}
}

// Recover restores a stale managed configuration if a prior process exited
// without cleanup. It refuses to overwrite settings that no longer resemble
// either the backup or this application's managed values.
func (m *Manager) Recover() (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.ops.supported() {
		return false, nil
	}
	journal, err := m.readJournal()
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	current, err := m.ops.current()
	if err != nil {
		return false, fmt.Errorf("read current proxy settings for recovery: %w", err)
	}
	if settingsEqual(current, journal.Backup) {
		return false, m.removeJournal()
	}
	if !matchesManagedTransition(current, journal) {
		return false, fmt.Errorf(
			"recovery journal exists but current proxy settings were changed externally; refusing to overwrite them (journal: %s)",
			m.path,
		)
	}
	if err := m.ops.restore(journal.Backup); err != nil {
		return false, fmt.Errorf("restore proxy settings from recovery journal: %w", err)
	}
	if err := m.removeJournal(); err != nil {
		return true, err
	}
	return true, nil
}

// Begin records the original settings durably before enabling the proxy.
func (m *Manager) Begin(address string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.ops.supported() {
		return nil
	}
	if m.active {
		return errors.New("system proxy transaction already active")
	}
	backup, err := m.ops.backup()
	if err != nil {
		return fmt.Errorf("backup system proxy: %w", err)
	}
	journal := recoveryJournal{
		Version:         journalVersion,
		PID:             os.Getpid(),
		CreatedAt:       time.Now().UTC(),
		ManagedAddress:  address,
		ManagedOverride: managedOverride,
		Backup:          backup,
	}
	if err := m.writeJournal(journal); err != nil {
		return err
	}

	m.backup = backup
	m.active = true
	if err := m.ops.enable(address); err != nil {
		restoreErr := m.ops.restore(backup)
		if restoreErr == nil {
			m.active = false
			m.backup = nil
		}
		removeErr := error(nil)
		if restoreErr == nil {
			removeErr = m.removeJournal()
		}
		return errors.Join(
			fmt.Errorf("enable system proxy: %w", err),
			wrapOptional("rollback system proxy", restoreErr),
			wrapOptional("remove recovery journal", removeErr),
		)
	}
	return nil
}

// Restore ends the active transaction and removes its recovery journal.
func (m *Manager) Restore() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.active {
		return nil
	}
	if err := m.ops.restore(m.backup); err != nil {
		return fmt.Errorf("restore system proxy: %w", err)
	}
	m.active = false
	m.backup = nil
	if err := m.removeJournal(); err != nil {
		return err
	}
	return nil
}

// Active reports whether this manager still owns settings that need restore.
func (m *Manager) Active() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active
}

func (m *Manager) writeJournal(journal recoveryJournal) error {
	if m.path == "" {
		return errors.New("system proxy recovery journal path is empty")
	}
	directory := filepath.Dir(m.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create recovery directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".proxy-recovery-*.tmp")
	if err != nil {
		return fmt.Errorf("create recovery journal: %w", err)
	}
	temporaryPath := temporary.Name()
	keep := false
	defer func() {
		_ = temporary.Close()
		if !keep {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("secure recovery journal: %w", err)
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(journal); err != nil {
		return fmt.Errorf("encode recovery journal: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("flush recovery journal: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close recovery journal: %w", err)
	}
	if err := os.Rename(temporaryPath, m.path); err != nil {
		return fmt.Errorf("install recovery journal: %w", err)
	}
	keep = true
	return nil
}

func (m *Manager) readJournal() (*recoveryJournal, error) {
	file, err := os.Open(m.path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var journal recoveryJournal
	if err := decoder.Decode(&journal); err != nil {
		return nil, fmt.Errorf("decode recovery journal %s: %w", m.path, err)
	}
	if journal.Version != journalVersion || journal.Backup == nil || journal.ManagedAddress == "" {
		return nil, fmt.Errorf("invalid recovery journal %s", m.path)
	}
	return &journal, nil
}

func (m *Manager) removeJournal() error {
	if err := os.Remove(m.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove recovery journal %s: %w", m.path, err)
	}
	return nil
}

func settingsEqual(left, right *ProxySettings) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.ProxyEnable == right.ProxyEnable &&
		left.ProxyServer == right.ProxyServer &&
		left.ProxyOverride == right.ProxyOverride &&
		left.HasServer == right.HasServer &&
		left.HasOverride == right.HasOverride
}

func matchesManagedTransition(current *ProxySettings, journal *recoveryJournal) bool {
	if current == nil || journal == nil || journal.Backup == nil {
		return false
	}
	enableKnown := current.ProxyEnable == journal.Backup.ProxyEnable || current.ProxyEnable == 1
	serverKnown := settingValueKnown(
		current.HasServer, current.ProxyServer,
		journal.Backup.HasServer, journal.Backup.ProxyServer,
		true, journal.ManagedAddress,
	)
	overrideKnown := settingValueKnown(
		current.HasOverride, current.ProxyOverride,
		journal.Backup.HasOverride, journal.Backup.ProxyOverride,
		true, journal.ManagedOverride,
	)
	return enableKnown && serverKnown && overrideKnown
}

func settingValueKnown(
	currentPresent bool,
	currentValue string,
	backupPresent bool,
	backupValue string,
	managedPresent bool,
	managedValue string,
) bool {
	return currentPresent == backupPresent && currentValue == backupValue ||
		currentPresent == managedPresent && currentValue == managedValue
}

func wrapOptional(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
