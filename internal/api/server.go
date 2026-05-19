// Package api — HTTP mux factory for the Relayly REST API.
package api

import (
	"net/http"
	"time"

	"github.com/NIKX-Tech/relayly/internal/database"
	"github.com/NIKX-Tech/relayly/internal/relay"
	"github.com/NIKX-Tech/relayly/pkg/version"
	"go.uber.org/zap"
)

// New returns an http.Handler that serves all /api/v1/* endpoints.
// Mount it on the relay mux under "/api/" to expose the REST API.
func New(db *database.DB, hub *relay.Hub, log *zap.Logger, ver string) http.Handler {
	if ver == "" {
		ver = version.Version
	}
	s := &server{
		db:      db,
		hub:     hub,
		log:     log,
		version: ver,
		startAt: time.Now(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/devices", s.handleListDevices)
	mux.HandleFunc("POST /api/v1/pair", s.handlePair)
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)

	// Wrap every route with CORS middleware.
	return corsMiddleware(mux)
}
