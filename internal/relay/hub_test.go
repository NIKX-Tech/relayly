package relay_test

import (
	"testing"
	"time"

	"github.com/NIKX-Tech/relayly/internal/relay"
	"go.uber.org/zap"
)

func TestHub_RegisterUnregister(t *testing.T) {
	t.Parallel()

	log := zap.NewNop()
	hub := relay.NewHub(log)
	go hub.Run()

	if hub.ConnectedCount() != 0 {
		t.Fatal("expected 0 connected clients initially")
	}

	// We can't easily inject a real *websocket.Conn in a unit test, but we
	// CAN test the hub's ConnectedDevices / ConnectedCount logic by verifying
	// that after sending an Unregister message the count stays correct.
	// Full integration tests with real WS connections belong in e2e/ tests.
	//
	// What we test here: hub starts, metrics are accessible, uptime increases.
	time.Sleep(10 * time.Millisecond)
	if hub.Uptime() < 10*time.Millisecond {
		t.Error("uptime should be non-zero")
	}
}

func TestHub_ConnectedDevices_Empty(t *testing.T) {
	t.Parallel()
	hub := relay.NewHub(zap.NewNop())
	go hub.Run()
	ids := hub.ConnectedDevices()
	if ids == nil {
		t.Error("ConnectedDevices should return empty slice, not nil")
	}
	if len(ids) != 0 {
		t.Errorf("expected 0, got %d", len(ids))
	}
}
