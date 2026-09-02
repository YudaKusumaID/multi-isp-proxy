package instance

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Lock is a per-user single-instance lease represented by an exclusive file.
type Lock struct {
	path  string
	token string
}

type lockRecord struct {
	PID   int    `json:"pid"`
	Token string `json:"token"`
}

// DefaultPath returns the per-user lock file path.
func DefaultPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(configDir, "multi-isp-proxy", "instance.lock"), nil
}

// Acquire obtains the single-instance lease and removes a stale lease once.
func Acquire(path string) (*Lock, error) {
	if path == "" {
		return nil, errors.New("instance lock path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create instance directory: %w", err)
	}

	for attempt := 0; attempt < 3; attempt++ {
		lock, err := create(path)
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}

		record, readErr := read(path)
		if readErr != nil {
			return nil, fmt.Errorf("read existing instance lock: %w", readErr)
		}
		alive, aliveErr := processAlive(record.PID)
		if aliveErr != nil {
			return nil, fmt.Errorf("check existing process %d: %w", record.PID, aliveErr)
		}
		if alive {
			return nil, fmt.Errorf("multi-isp-proxy is already running with PID %d", record.PID)
		}
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return nil, fmt.Errorf("remove stale instance lock: %w", removeErr)
		}
	}
	return nil, errors.New("could not acquire instance lock after removing stale lock")
}

func create(path string) (*Lock, error) {
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("generate instance token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = os.Remove(path)
		}
	}()
	if err := json.NewEncoder(file).Encode(lockRecord{PID: os.Getpid(), Token: token}); err != nil {
		return nil, fmt.Errorf("write instance lock: %w", err)
	}
	if err := file.Sync(); err != nil {
		return nil, fmt.Errorf("flush instance lock: %w", err)
	}
	keep = true
	return &Lock{path: path, token: token}, nil
}

func read(path string) (*lockRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var record lockRecord
	if err := json.NewDecoder(file).Decode(&record); err != nil {
		return nil, err
	}
	if record.PID <= 0 || record.Token == "" {
		return nil, errors.New("invalid instance lock")
	}
	return &record, nil
}

// Release removes the lease only if it still belongs to this Lock.
func (l *Lock) Release() error {
	if l == nil || l.path == "" {
		return nil
	}
	record, err := read(l.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read instance lock during release: %w", err)
	}
	if record.Token != l.token {
		return errors.New("instance lock ownership changed; refusing to remove it")
	}
	if err := os.Remove(l.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove instance lock: %w", err)
	}
	l.path = ""
	return nil
}
