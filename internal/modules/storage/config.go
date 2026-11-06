package storage

import (
	"fmt"
	"strings"
	"time"
)

// Config of the storage module.
type Config struct {
	// DataDir holds the module database.
	DataDir string
	// ScanInterval between scans of every location; zero disables them.
	ScanInterval time.Duration
}

// DefaultConfig returns the default configuration.
func DefaultConfig(dataDir string) Config {
	return Config{DataDir: dataDir, ScanInterval: 6 * time.Hour}
}

const envPrefix = "MSERVER_STORAGE_"

// ConfigFromEnv reads the MSERVER_STORAGE_* variables from environ (as
// returned by os.Environ):
//
//	MSERVER_STORAGE_SCAN_INTERVAL  duration, 0 disables (default 6h)
//
// Unknown MSERVER_STORAGE_* variables are rejected to catch typos.
func ConfigFromEnv(dataDir string, environ []string) (Config, error) {
	cfg := DefaultConfig(dataDir)

	for _, kv := range environ {
		key, value, _ := strings.Cut(kv, "=")
		if !strings.HasPrefix(key, envPrefix) {
			continue
		}

		switch key {
		case envPrefix + "SCAN_INTERVAL":
			d, err := time.ParseDuration(value)
			if err != nil || d < 0 {
				return Config{}, fmt.Errorf("storage: %s: invalid duration %q", key, value)
			}
			cfg.ScanInterval = d
		default:
			return Config{}, fmt.Errorf("storage: unknown variable %s", key)
		}
	}
	return cfg, nil
}
