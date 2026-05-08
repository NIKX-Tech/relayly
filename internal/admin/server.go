// Package admin provides a minimal web UI for monitoring and managing Relayly.
// Templates are embedded at compile time — no external assets are required.
package admin

import (
	"embed"
	"encoding/json"
	"html/template"
	"io/fs"
	"net/http"
	"time"

	"github.com/nikx-one/relayly/internal/database"
	"github.com/nikx-one/relayly/internal/relay"
	"github.com/nikx-one/relayly/pkg/version"
	"go.uber.org/zap"
)

//go:embed templates/*.html templates/partials/*.html
var templateFS embed.FS

// Server is the admin HTTP server.
type Server struct {
	hub   *relay.Hub
	db    *database.DB
	log   *zap.Logger
	tmpls *template.Template
	mux   *http.ServeMux
}

// New creates a new admin Server and registers all routes.
func New(hub *relay.Hub, db *database.DB, log *zap.Logger) (*Server, error) {
	subFS, err := fs.Sub(templateFS, "templates")
	if err != nil {
		return nil, err
	}
	tmpls, err := template.ParseFS(subFS, "*.html", "partials/*.html")
	if err != nil {
		return nil, err
	}

	s := &Server{hub: hub, db: db, log: log, tmpls: tmpls, mux: http.NewServeMux()}
	s.registerRoutes()
	return s, nil
}

// ServeHTTP implements http.Handler so Server can be passed to http.Server.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) registerRoutes() {
	// Pages
	s.mux.HandleFunc("GET /", s.handleIndex)
	s.mux.HandleFunc("GET /devices", s.handleDevices)

	// HTMX partials
	s.mux.HandleFunc("GET /htmx/devices", s.handleDevicesPartial)
	s.mux.HandleFunc("DELETE /htmx/devices/{id}", s.handleDeleteDevice)
	s.mux.HandleFunc("POST /htmx/pair", s.handlePairDevices)

	// REST API (used by `relayly status` CLI command)
	s.mux.HandleFunc("GET /api/v1/status", s.apiStatus)
	s.mux.HandleFunc("GET /api/v1/devices", s.apiDevices)
}

// ── Page handlers ─────────────────────────────────────────────────────────────

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	devices, err := s.db.ListDevices()
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	s.render(w, "index.html", map[string]any{
		"Version":   version.Version,
		"Connected": s.hub.ConnectedCount(),
		"Uptime":    s.hub.Uptime().Round(time.Second).String(),
		"Devices":   devices,
		"Online":    onlineSet(s.hub.ConnectedDevices()),
	})
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := s.db.ListDevices()
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	s.render(w, "devices.html", map[string]any{
		"Devices": devices,
		"Online":  onlineSet(s.hub.ConnectedDevices()),
	})
}

// ── HTMX partial handlers ──────────────────────────────────────────────────────

func (s *Server) handleDevicesPartial(w http.ResponseWriter, r *http.Request) {
	devices, err := s.db.ListDevices()
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	s.render(w, "devices_table", map[string]any{
		"Devices": devices,
		"Online":  onlineSet(s.hub.ConnectedDevices()),
	})
}

func (s *Server) handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.db.DeleteDevice(id); err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	s.log.Info("device deleted via admin UI", zap.String("device_id", id))
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handlePairDevices(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	id1 := r.FormValue("id1")
	id2 := r.FormValue("id2")

	if id1 == "" || id2 == "" || id1 == id2 {
		http.Error(w, "invalid device IDs", http.StatusBadRequest)
		return
	}

	if err := s.db.PairDevices(id1, id2); err != nil {
		s.log.Error("pairing error", zap.Error(err))
		http.Error(w, "pairing failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.log.Info("devices paired via admin UI", zap.String("id1", id1), zap.String("id2", id2))

	// Refresh the whole table
	s.handleDevicesPartial(w, r)
}

// ── API handlers ───────────────────────────────────────────────────────────────

type statusResponse struct {
	Status    string `json:"status"`
	Version   string `json:"version"`
	Connected int    `json:"connected"`
	Uptime    string `json:"uptime"`
}

func (s *Server) apiStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, statusResponse{
		Status:    "ok",
		Version:   version.Version,
		Connected: s.hub.ConnectedCount(),
		Uptime:    s.hub.Uptime().Round(time.Second).String(),
	})
}

func (s *Server) apiDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := s.db.ListDevices()
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, devices)
}

// ── Helpers ────────────────────────────────────────────────────────────────────

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Try the name as provided, then try the base name if it fails
	err := s.tmpls.ExecuteTemplate(w, name, data)
	if err != nil {
		s.log.Error("template render error", zap.String("template", name), zap.Error(err))
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func onlineSet(ids []string) map[string]bool {
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}
