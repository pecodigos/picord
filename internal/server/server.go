package server

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/pecodigos/picord/internal/profile"
)

//go:embed all:web
var webAssets embed.FS

type AppState struct {
	mu            sync.RWMutex
	activeName    string
	activeProc    string
	detectedProcs []profile.DetectedProcess
	override      *profile.Profile
	autoDetect    bool
}

func NewAppState() *AppState {
	return &AppState{
		autoDetect: true,
	}
}

func (s *AppState) SetActive(name, proc string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeName = name
	s.activeProc = proc
}

func (s *AppState) ClearActive() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeName = ""
	s.activeProc = ""
}

func (s *AppState) SetDetected(procs []profile.DetectedProcess) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.detectedProcs = procs
}

func (s *AppState) AutoDetectEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.autoDetect
}

func (s *AppState) SetAutoDetect(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.autoDetect = v
}

func (s *AppState) HasOverride() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.override != nil
}

func (s *AppState) SetOverride(p *profile.Profile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.override = p
}

type Server struct {
	state          *AppState
	profileManager *profile.Manager

	OnOverrideSet    func(*profile.Profile)
	OnOverrideClear  func()
	OnAutoDetectSet  func(bool)
	OnReloadConfig   func()
	OnProfilesSaved  func([]profile.Profile)
}

type statusResponse struct {
	ActiveName    string                    `json:"active_name"`
	ActiveProcess string                    `json:"active_process"`
	DetectedProcs []profile.DetectedProcess `json:"detected_processes"`
	AutoDetect    bool                      `json:"auto_detect"`
	HasOverride   bool                      `json:"has_override"`
}

func New(s *AppState, pm *profile.Manager) *Server {
	return &Server{
		state:          s,
		profileManager: pm,
	}
}

func (srv *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	webFS, _ := fs.Sub(webAssets, "web")
	mux.Handle("/", http.FileServer(http.FS(webFS)))

	mux.HandleFunc("/api/status", srv.handleStatus)
	mux.HandleFunc("/api/profiles", srv.handleProfiles)
	mux.HandleFunc("/api/profiles/", srv.handleProfileByID)
	mux.HandleFunc("/api/defaults", srv.handleDefaults)
	mux.HandleFunc("/api/override", srv.handleOverride)
	mux.HandleFunc("/api/settings", srv.handleSettings)
	mux.HandleFunc("/api/reload", srv.handleReload)

	return withCORS(mux)
}

func (srv *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}

	srv.state.mu.RLock()
	defer srv.state.mu.RUnlock()

	resp := statusResponse{
		ActiveName:    srv.state.activeName,
		ActiveProcess: srv.state.activeProc,
		DetectedProcs: srv.state.detectedProcs,
		AutoDetect:    srv.state.AutoDetectEnabled(),
		HasOverride:   srv.state.HasOverride(),
	}
	writeJSON(w, resp)
}

func (srv *Server) handleProfiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		profiles := srv.profileManager.All()
		userProfiles := make([]profile.Profile, 0)
		for _, p := range profiles {
			if !p.IsDefault() {
				userProfiles = append(userProfiles, p)
			}
		}
		writeJSON(w, userProfiles)

	case http.MethodPost:
		var p profile.Profile
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			writeError(w, "invalid profile JSON", 400)
			return
		}
		if p.Name == "" {
			writeError(w, "profile name is required", 400)
			return
		}
		if p.Match.Type == "" {
			p.Match.Type = profile.MatchProcessName
		}
		if p.Priority == 0 {
			p.Priority = 5
		}
		p.Enabled = true
		srv.profileManager.Add(p)
		srv.notifyProfilesChanged()
		writeJSON(w, map[string]string{"status": "ok"})

	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (srv *Server) handleProfileByID(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/profiles/")
	if name == "" {
		http.Error(w, "profile name required", 400)
		return
	}

	switch r.Method {
	case http.MethodGet:
		p := srv.profileManager.Get(name)
		if p == nil {
			http.Error(w, "not found", 404)
			return
		}
		writeJSON(w, p)

	case http.MethodPut:
		var p profile.Profile
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			writeError(w, "invalid profile JSON", 400)
			return
		}
		p.Name = name
		srv.profileManager.Add(p)
		srv.notifyProfilesChanged()
		writeJSON(w, map[string]string{"status": "ok"})

	case http.MethodDelete:
		if !srv.profileManager.Delete(name) {
			http.Error(w, "not found", 404)
			return
		}
		srv.notifyProfilesChanged()
		writeJSON(w, map[string]string{"status": "ok"})

	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (srv *Server) handleDefaults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	defaults := profile.DefaultProfiles()
	writeJSON(w, defaults)
}

func (srv *Server) handleOverride(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var p profile.Profile
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			writeError(w, "invalid profile JSON", 400)
			return
		}
		if srv.OnOverrideSet != nil {
			srv.OnOverrideSet(&p)
		}
		writeJSON(w, map[string]string{"status": "ok"})

	case http.MethodDelete:
		if srv.OnOverrideClear != nil {
			srv.OnOverrideClear()
		}
		writeJSON(w, map[string]string{"status": "ok"})

	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (srv *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{
			"auto_detect": srv.state.AutoDetectEnabled(),
		})

	case http.MethodPut:
		var settings map[string]any
		if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
			writeError(w, "invalid JSON", 400)
			return
		}
		if ad, ok := settings["auto_detect"]; ok {
			if v, ok := ad.(bool); ok && srv.OnAutoDetectSet != nil {
				srv.OnAutoDetectSet(v)
			}
		}
		writeJSON(w, map[string]string{"status": "ok"})

	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (srv *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if srv.OnReloadConfig != nil {
		srv.OnReloadConfig()
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (srv *Server) notifyProfilesChanged() {
	if srv.OnProfilesSaved == nil {
		return
	}
	all := srv.profileManager.All()
	userProfiles := make([]profile.Profile, 0)
	for _, p := range all {
		if !p.IsDefault() {
			userProfiles = append(userProfiles, p)
		}
	}
	srv.OnProfilesSaved(userProfiles)
}

func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(200)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func StartServer(addr string, srv *Server) *http.Server {
	httpServer := &http.Server{
		Addr:    addr,
		Handler: srv.Handler(),
	}
	go func() {
		log.Printf("Web GUI: http://%s\n", addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v\n", err)
		}
	}()
	return httpServer
}
