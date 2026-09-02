package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRunVersion(t *testing.T) {
	originalVersion := version
	version = "v1.2.3"
	t.Cleanup(func() { version = originalVersion })

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := run([]string{"-version"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", exitCode, stderr.String())
	}
	if got, want := stdout.String(), "multi-isp-proxy v1.2.3\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestParseCredentials(t *testing.T) {
	username, password, err := parseCredentials("alice:secret:with-colon")
	if err != nil || username != "alice" || password != "secret:with-colon" {
		t.Fatalf("parseCredentials = %q, %q, %v", username, password, err)
	}
	for _, value := range []string{"missing-separator", ":password", "username:"} {
		if _, _, err := parseCredentials(value); err == nil {
			t.Fatalf("parseCredentials(%q) should fail", value)
		}
	}
}

func TestLoadCredentialsFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.txt")
	if err := os.WriteFile(path, []byte("alice:secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := loadCredentials(path, "")
	if err != nil || value != "alice:secret" {
		t.Fatalf("loadCredentials = %q, %v", value, err)
	}
	if _, err := loadCredentials(path, "also:set"); err == nil {
		t.Fatal("file and environment credentials should conflict")
	}
}

func TestOpenLogFileRotatesLargeLog(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "multi-isp-proxy.log")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxLogSize); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	opened, openedPath, err := openLogFileAt(directory)
	if err != nil {
		t.Fatalf("openLogFileAt: %v", err)
	}
	if err := opened.Close(); err != nil {
		t.Fatal(err)
	}
	if openedPath != path {
		t.Fatalf("opened path = %q, want %q", openedPath, path)
	}
	rotated, err := os.Stat(path + ".1")
	if err != nil || rotated.Size() != maxLogSize {
		t.Fatalf("rotated log: info=%v err=%v", rotated, err)
	}
	current, err := os.Stat(path)
	if err != nil || current.Size() != 0 {
		t.Fatalf("current log: info=%v err=%v", current, err)
	}
}

func TestRunRejectsUnknownFlag(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := run([]string{"-unknown"}, &stdout, &stderr); exitCode != 2 {
		t.Fatalf("exit code = %d, want 2", exitCode)
	}
}
