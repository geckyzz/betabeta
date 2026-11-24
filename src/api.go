// This file is part of betabeta.
// Copyright (C) 2026  geckyzz <gecko.geckyzz@proton.me>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package main

import (
	"encoding/json"
	"log"
	"net/http"
)

// writeJSON sends a JSON response.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		prnv("writeJSON: encoding response failed: %v", err)
	}
}

// APIHandlers holds the HTTP handlers.
type APIHandlers struct {
	mgr *Manager
	cfg *Config
	log *log.Logger
}

// acquireHandler handles POST /v1/acquire
// Body: {"country": "jp"}
// Response: {"country":"jp","provider":"protonvpn","proxy_url":"socks5://0.0.0.0:10801","container":"vpn-protonvpn-jp","socks_port":10801}
func (h *APIHandlers) acquireHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !checkAuth(r, h.cfg.Server.AuthToken) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Country string `json:"country"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Country == "" {
		http.Error(w, "country is required", http.StatusBadRequest)
		return
	}

	proxy, err := h.mgr.Acquire(req.Country)
	if err != nil {
		h.log.Printf("acquire %s failed: %v", req.Country, err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, proxy)
}

// releaseHandler handles POST /v1/release
// Body: {"country": "jp"}
func (h *APIHandlers) releaseHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !checkAuth(r, h.cfg.Server.AuthToken) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Country string `json:"country"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Country == "" {
		http.Error(w, "country is required", http.StatusBadRequest)
		return
	}

	if err := h.mgr.Release(req.Country); err != nil {
		h.log.Printf("release %s failed: %v", req.Country, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"released": req.Country})
}

// statusHandler handles GET /v1/status
func (h *APIHandlers) statusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !checkAuth(r, h.cfg.Server.AuthToken) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, h.mgr.Status())
}

// healthHandler handles GET /v1/health
func (h *APIHandlers) healthHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// checkAuth validates the Bearer token. If the config token is empty, auth is
// disabled (local-only binding is expected).
func checkAuth(r *http.Request, token string) bool {
	if token == "" {
		return true
	}
	auth := r.Header.Get("Authorization")
	expected := "Bearer " + token
	return auth == expected
}

// corsMiddleware adds CORS headers for local/Tailscale access.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /v1/status exposes container and proxy details; keep it
		// same-origin by not advertising CORS there.
		if r.URL.Path != "/v1/status" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware logs each request.
func loggingMiddleware(next http.Handler, l *log.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l.Printf("%s %s %s", r.RemoteAddr, r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func (h *APIHandlers) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/acquire", h.acquireHandler)
	mux.HandleFunc("/v1/release", h.releaseHandler)
	mux.HandleFunc("/v1/status", h.statusHandler)
	mux.HandleFunc("/v1/health", h.healthHandler)
	return corsMiddleware(loggingMiddleware(mux, h.log))
}

// prnv is a package-level verbose log (mirrors Python's prnv).
func prnv(format string, args ...interface{}) {
	log.Printf("[betabeta] "+format, args...)
}

// prni is a package-level info log.
func prni(format string, args ...interface{}) {
	log.Printf("[betabeta] "+format, args...)
}
