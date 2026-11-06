package storage

import (
	"testing"
	"time"
)

func TestConfigFromEnv(t *testing.T) {
	cfg, err := ConfigFromEnv("/data", []string{
		"PATH=/usr/bin",
		"MSERVER_DATA_DIR=/ignored",
		"MSERVER_STORAGE_SCAN_INTERVAL=1h",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != "/data" || cfg.ScanInterval != time.Hour {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestConfigFromEnvRejectsInvalid(t *testing.T) {
	for _, kv := range []string{
		"MSERVER_STORAGE_TYPO=1",
		"MSERVER_STORAGE_SCAN_INTERVAL=often",
		"MSERVER_STORAGE_SCAN_INTERVAL=-1h",
	} {
		if _, err := ConfigFromEnv("/data", []string{kv}); err == nil {
			t.Errorf("%s: expected an error", kv)
		}
	}
}
