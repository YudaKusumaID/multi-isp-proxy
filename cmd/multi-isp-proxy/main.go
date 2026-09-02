package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/YudaKusumaID/multi-isp-proxy/internal/instance"
	"github.com/YudaKusumaID/multi-isp-proxy/internal/proxy"
	"github.com/YudaKusumaID/multi-isp-proxy/internal/sysproxy"
	"github.com/YudaKusumaID/multi-isp-proxy/internal/tui"
)

var version = "dev"

const maxLogSize = 5 << 20

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("multi-isp-proxy", flag.ContinueOnError)
	flags.SetOutput(stderr)

	addr := flags.String("addr", "127.0.0.1:1080", "Proxy listen address")
	allowRemote := flags.Bool("allow-remote", false, "Allow a non-loopback listener (requires authentication)")
	authFile := flags.String("auth-file", "", "Read proxy credentials (username:password) from a file")
	showVersion := flags.Bool("version", false, "Show version")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	if *showVersion {
		fmt.Fprintf(stdout, "multi-isp-proxy %s\n", version)
		return 0
	}

	credentialValue, err := loadCredentials(*authFile, os.Getenv("MULTI_ISP_PROXY_AUTH"))
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 2
	}
	username, password, err := parseCredentials(credentialValue)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 2
	}

	logFile, logPath, err := openLogFile()
	if err != nil {
		fmt.Fprintf(stderr, "Warning: could not open log file: %v\n", err)
	} else {
		log.SetOutput(logFile)
		defer logFile.Close()
		log.Printf("Logging to %s", logPath)
	}

	log.Printf("Starting Multi ISP Proxy %s", version)

	lockPath, err := instance.DefaultPath()
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	instanceLock, err := instance.Acquire(lockPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	journalPath, err := sysproxy.DefaultJournalPath()
	if err != nil {
		_ = instanceLock.Release()
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	systemProxy := sysproxy.NewManager(journalPath)
	recovered, err := systemProxy.Recover()
	if err != nil {
		_ = instanceLock.Release()
		fmt.Fprintf(stderr, "Recovery error: %v\n", err)
		return 1
	}
	if recovered {
		log.Println("Recovered Windows proxy settings from an unclean prior exit")
		fmt.Fprintln(stderr, "Recovered Windows proxy settings from the previous unclean exit.")
	}

	model := tui.NewModelWithConfig(tui.Config{
		Proxy: proxy.Config{
			HTTPAddr:    *addr,
			AllowRemote: *allowRemote,
			Username:    username,
			Password:    password,
		},
		SystemProxy: systemProxy,
	})
	program := tea.NewProgram(model, tea.WithOutput(stdout))
	finalModel, runErr := program.Run()

	cleanupModel := model
	if model, ok := finalModel.(tui.Model); ok {
		cleanupModel = model
	}
	cleanupErr := cleanupModel.Cleanup()

	if runErr != nil {
		log.Printf("Error running TUI: %v", runErr)
		fmt.Fprintf(stderr, "Error: %v\n", runErr)
	}
	if cleanupErr != nil {
		log.Printf("Error cleaning up: %v", cleanupErr)
		fmt.Fprintf(stderr, "Cleanup error: %v\n", cleanupErr)
	}
	exitCode := 0
	if runErr != nil || cleanupErr != nil {
		exitCode = 1
	}
	if err := instanceLock.Release(); err != nil {
		log.Printf("Error releasing instance lock: %v", err)
		fmt.Fprintf(stderr, "Cleanup error: %v\n", err)
		exitCode = 1
	}

	if exitCode == 0 {
		log.Println("Multi ISP Proxy exited cleanly")
	}
	return exitCode
}

func parseCredentials(value string) (string, string, error) {
	if value == "" {
		return "", "", nil
	}
	username, password, ok := strings.Cut(value, ":")
	if !ok || username == "" || password == "" {
		return "", "", fmt.Errorf("authentication must use non-empty username:password")
	}
	return username, password, nil
}

func loadCredentials(path, environmentValue string) (string, error) {
	if path == "" {
		return environmentValue, nil
	}
	if environmentValue != "" {
		return "", fmt.Errorf("use either -auth-file or MULTI_ISP_PROXY_AUTH, not both")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read authentication file: %w", err)
	}
	if len(contents) > 4096 {
		return "", fmt.Errorf("authentication file is unexpectedly large")
	}
	return strings.TrimSpace(string(contents)), nil
}

func openLogFile() (*os.File, string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return nil, "", fmt.Errorf("resolve user cache directory: %w", err)
	}

	logDir := filepath.Join(cacheDir, "multi-isp-proxy")
	return openLogFileAt(logDir)
}

func openLogFileAt(logDir string) (*os.File, string, error) {
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, "", fmt.Errorf("create log directory: %w", err)
	}

	logPath := filepath.Join(logDir, "multi-isp-proxy.log")
	if info, err := os.Stat(logPath); err == nil && info.Size() >= maxLogSize {
		rotatedPath := logPath + ".1"
		if err := os.Remove(rotatedPath); err != nil && !os.IsNotExist(err) {
			return nil, "", fmt.Errorf("remove old rotated log: %w", err)
		}
		if err := os.Rename(logPath, rotatedPath); err != nil {
			return nil, "", fmt.Errorf("rotate log file: %w", err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return nil, "", fmt.Errorf("inspect log file: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, "", fmt.Errorf("open log file: %w", err)
	}

	return logFile, logPath, nil
}
