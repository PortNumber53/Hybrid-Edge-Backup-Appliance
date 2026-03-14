// Package api provides the local HTTP server that serves the Vite UI
// and exposes management endpoints for drive and orchestrator operations.
package api

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/PortNumber53/Hybrid-Edge-Backup-Appliance/pkg/diskops"
	"github.com/PortNumber53/Hybrid-Edge-Backup-Appliance/pkg/orchestrator"
)

// Server is the local HTTP API server.
type Server struct {
	listenAddr   string
	diskManager  *diskops.Manager
	orchestrator *orchestrator.Orchestrator
	httpServer   *http.Server
}

// NewServer creates a new API server.
func NewServer(addr string, dm *diskops.Manager, orch *orchestrator.Orchestrator) *Server {
	return &Server{
		listenAddr:   addr,
		diskManager:  dm,
		orchestrator: orch,
	}
}

// Start binds and serves the HTTP API.
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Drive management endpoints.
	mux.HandleFunc("/api/drives", s.corsMiddleware(s.handleDrives))
	mux.HandleFunc("/api/drives/eject", s.corsMiddleware(s.handleEject))

	// Orchestrator endpoints.
	mux.HandleFunc("/api/status", s.corsMiddleware(s.handleStatus))

	// Health endpoint for internal checks.
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	s.httpServer = &http.Server{
		Addr:         s.listenAddr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Binds the server to the address specified in the configuration.
	listener, err := s.localListener()
	if err != nil {
		return err
	}

	log.Printf("[api] server listening on %s", listener.Addr().String())
	go func() {
		if err := s.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("[api] server error: %v", err)
		}
	}()
	return nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop(ctx context.Context) error {
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

func (s *Server) localListener() (net.Listener, error) {
	return net.Listen("tcp", s.listenAddr)
}

func (s *Server) corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		// Only allow local origins.
		if origin != "" && isLocalOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func isLocalOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	hostname := u.Hostname()

	if hostname == "localhost" {
		return true
	}

	ip := net.ParseIP(hostname)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

// handleDrives returns the list of mounted drives.
func (s *Server) handleDrives(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mounts := s.diskManager.Mounter.ListMounts()
	writeJSON(w, http.StatusOK, mounts)
}

// handleEject safely unmounts a drive by UUID.
func (s *Server) handleEject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		UUID string `json:"uuid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UUID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "uuid is required"})
		return
	}

	if err := s.diskManager.Mounter.Unmount(req.UUID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Drive safely ejected. You may now unplug it.",
		"uuid":    req.UUID,
	})
}

// handleStatus returns overall system status.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	status := map[string]interface{}{
		"drives":            s.diskManager.Mounter.ListMounts(),
		"containers_active": s.orchestrator.IsRunning(ctx),
	}
	writeJSON(w, http.StatusOK, status)
}

func writeJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("[api] error writing json response: %v", err)
	}
}
