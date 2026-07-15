package config

import (
	"testing"
	"time"
)

// Regression test: config/relayly.yaml previously shipped ping_interval/deadline as
// bare integers (e.g. `deadline: 60`), which mapstructure decodes directly into
// time.Duration as nanoseconds, not seconds — silently giving every connection an
// effective ~60ns read deadline whenever this file was actually loaded (repo root,
// Docker, systemd with a matching WorkingDirectory). Loads the real shipped file to
// guard against the value shape regressing.
func TestLoad_ShippedConfigDurationsAreSeconds(t *testing.T) {
	cfg, err := Load("../../config/relayly.yaml", nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.WebSocket.PingInterval < time.Second {
		t.Errorf("ping_interval = %v, want at least 1s (got a sub-second duration — check the YAML value is a duration string like \"30s\", not a bare integer)", cfg.WebSocket.PingInterval)
	}
	if cfg.WebSocket.Deadline < time.Second {
		t.Errorf("deadline = %v, want at least 1s (got a sub-second duration — check the YAML value is a duration string like \"60s\", not a bare integer)", cfg.WebSocket.Deadline)
	}
}
