package server

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pecodigos/picord/internal/catalog"
	"github.com/pecodigos/picord/internal/config"
	"github.com/pecodigos/picord/internal/iconfinder"
	"github.com/pecodigos/picord/internal/profile"
)

type ScanMode string

const (
	ScanModeAll           ScanMode = "all_processes"
	ScanModeIPCCandidates ScanMode = "ipc_candidates"
)

type ScanState string

const (
	ScanStatePending ScanState = "pending"
	ScanStateScanned ScanState = "scanned"
	ScanStateError   ScanState = "error"
)

type ScanSnapshot struct {
	Procs []profile.DetectedProcess
	Time  time.Time
	Mode  ScanMode
	State ScanState
}

type AppState struct {
	mu           sync.RWMutex
	activeName   string
	activeProc   string
	snapshot     ScanSnapshot
	matchInfo    profile.MatchInfo
	override     *profile.Profile
	autoDetect   bool
	rpcConnected bool
	appID        string
}

func NewAppState() *AppState {
	return &AppState{
		autoDetect: true,
		snapshot: ScanSnapshot{
			State: ScanStatePending,
		},
	}
}

func (s *AppState) SetRPCConnected(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rpcConnected = v
}

func (s *AppState) SetAppID(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.appID = id
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

func (s *AppState) SetScanSnapshot(ss ScanSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot = ss
}

func (s *AppState) ScanSnapshot() ScanSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot
}

func (s *AppState) SetMatchInfo(mi profile.MatchInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.matchInfo = mi
}

func (s *AppState) MatchInfo() profile.MatchInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.matchInfo
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
	state            *AppState
	profileManager   *profile.Manager
	catalogStore     *catalog.Store
	catalogEnricher  *catalog.Enricher
	settingsProvider func() config.AppConfig
	token            string

	OnOverrideSet   func(*profile.Profile)
	OnOverrideClear func()
	OnAutoDetectSet func(bool)
	OnSettingsSaved func(config.AppConfig) error
	OnReloadConfig  func()
	OnProfilesSaved func([]profile.Profile)
}

func (srv *Server) SetToken(t string) {
	srv.token = t
}

func (srv *Server) SetCatalogEnricher(e *catalog.Enricher) {
	srv.catalogEnricher = e
}

type sanitizedProcess struct {
	PID         int      `json:"pid"`
	Name        string   `json:"name"`
	WindowTitle string   `json:"window_title,omitempty"`
	SteamAppID  string   `json:"steam_app_id,omitempty"`
	DesktopID   string   `json:"desktop_id,omitempty"`
	Aliases     []string `json:"aliases,omitempty"`
}

func sanitizeDetected(procs []profile.DetectedProcess, verbose bool) []sanitizedProcess {
	out := make([]sanitizedProcess, len(procs))
	for i, p := range procs {
		sp := sanitizedProcess{
			PID:         p.PID,
			Name:        p.Name,
			WindowTitle: p.WindowTitle,
		}
		if verbose {
			sp.SteamAppID = p.SteamAppID
			sp.DesktopID = p.DesktopID
			sp.Aliases = p.Aliases
		}
		out[i] = sp
	}
	return out
}

type statusResponse struct {
	ActiveName    string             `json:"active_name"`
	ActiveProcess string             `json:"active_process"`
	DetectedProcs []sanitizedProcess `json:"detected_processes"`
	AutoDetect    bool               `json:"auto_detect"`
	HasOverride   bool               `json:"has_override"`
	ScanState     string             `json:"scan_state"`
	ScanMode      string             `json:"scan_mode,omitempty"`
	LastScanTime  string             `json:"last_scan_time,omitempty"`
	MatchInfo     *profile.MatchInfo `json:"match_info,omitempty"`
	RPCConnected  bool               `json:"rpc_connected"`
	AppID         string             `json:"app_id,omitempty"`
}

func New(s *AppState, pm *profile.Manager, cs *catalog.Store) *Server {
	return &Server{
		state:          s,
		profileManager: pm,
		catalogStore:   cs,
	}
}

func (srv *Server) SetSettingsProvider(fn func() config.AppConfig) {
	srv.settingsProvider = fn
}

func (srv *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, "Picord API: use /api/status or the picord CLI", http.StatusNotFound)
	})

	mux.HandleFunc("/api/status", srv.handleStatus)
	mux.HandleFunc("/api/profiles", srv.handleProfiles)
	mux.HandleFunc("/api/profiles/", srv.handleProfileByID)
	mux.HandleFunc("/api/defaults", srv.handleDefaults)
	mux.HandleFunc("/api/override", srv.handleOverride)
	mux.HandleFunc("/api/settings", srv.handleSettings)
	mux.HandleFunc("/api/reload", srv.handleReload)

	mux.HandleFunc("/api/catalog/status", srv.handleCatalogStatus)
	mux.HandleFunc("/api/catalog/search", srv.handleCatalogSearch)
	mux.HandleFunc("/api/catalog/entries/", srv.handleCatalogEntry)
	mux.HandleFunc("/api/catalog/refresh", srv.handleCatalogRefresh)
	mux.HandleFunc("/api/catalog/enrich", srv.handleCatalogEnrich)
	mux.HandleFunc("/api/catalog/profiles/from-entry/", srv.handleCatalogProfileFromEntry)

	// Serve local assets (game images, etc.) from the filesystem.
	assetsDir := AssetsDir()
	if _, err := os.Stat(assetsDir); err == nil {
		mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir(assetsDir))))
	}

	// Serve local desktop icons resolved by the iconfinder registry.
	mux.HandleFunc("/assets/picord-icons/", srv.handleLocalIcon)

	return withSecurity(srv.token, mux)
}

// AssetsDir returns the directory for local image assets.
func AssetsDir() string {
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Join(filepath.Dir(exe), "assets")
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
	}
	if wd, err := os.Getwd(); err == nil {
		dir := filepath.Join(wd, "assets")
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
	}
	return "assets"
}

func (srv *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	verbose := r.URL.Query().Get("verbose") == "1"

	srv.state.mu.RLock()
	ss := srv.state.snapshot
	mi := srv.state.matchInfo
	resp := statusResponse{
		ActiveName:    srv.state.activeName,
		ActiveProcess: srv.state.activeProc,
		DetectedProcs: sanitizeDetected(ss.Procs, verbose),
		AutoDetect:    srv.state.autoDetect,
		HasOverride:   srv.state.override != nil,
		ScanState:     string(ss.State),
		ScanMode:      string(ss.Mode),
		RPCConnected:  srv.state.rpcConnected,
		AppID:         srv.state.appID,
	}
	if !ss.Time.IsZero() {
		resp.LastScanTime = ss.Time.Format(time.RFC3339)
	}
	if verbose && mi.Source != "" {
		resp.MatchInfo = &mi
	}
	srv.state.mu.RUnlock()
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
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		// Support rename: if the request body name differs from the URL name,
		// delete the old profile and add the new one.
		if p.Name != "" && p.Name != name {
			srv.profileManager.Delete(name)
			srv.profileManager.Replace(p)
		} else {
			p.Name = name
			srv.profileManager.Replace(p)
		}
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
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (srv *Server) handleDefaults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type settingsResponse struct {
	AutoDetect       bool                   `json:"auto_detect"`
	ScanAllProcesses bool                   `json:"scan_all_processes"`
	ShowTrayIcon     bool                   `json:"show_tray_icon"`
	Catalog          config.CatalogConfig   `json:"catalog"`
	Images           config.ImageConfig     `json:"images"`
	Detection        config.DetectionConfig `json:"detection"`
}

type settingsPatch struct {
	AutoDetect       *bool                   `json:"auto_detect"`
	ScanAllProcesses *bool                   `json:"scan_all_processes"`
	ShowTrayIcon     *bool                   `json:"show_tray_icon"`
	Catalog          *config.CatalogConfig   `json:"catalog"`
	Images           *config.ImageConfig     `json:"images"`
	Detection        *config.DetectionConfig `json:"detection"`
}

func (srv *Server) currentConfig() config.AppConfig {
	if srv.settingsProvider != nil {
		return srv.settingsProvider()
	}
	return config.DefaultConfig()
}

func settingsFromConfig(cfg config.AppConfig, autoDetect bool) settingsResponse {
	catalogCfg := cfg.Catalog
	// Do not expose secrets back through the settings API.
	catalogCfg.SteamGridDBAPIKey = ""
	return settingsResponse{
		AutoDetect:       autoDetect,
		ScanAllProcesses: cfg.ScanAllProcesses,
		ShowTrayIcon:     cfg.ShowTrayIcon,
		Catalog:          catalogCfg,
		Images:           cfg.Images,
		Detection:        cfg.Detection,
	}
}

func (srv *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, settingsFromConfig(srv.currentConfig(), srv.state.AutoDetectEnabled()))

	case http.MethodPut:
		var patch settingsPatch
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			writeError(w, "invalid JSON", 400)
			return
		}

		cfg := srv.currentConfig()
		if patch.ScanAllProcesses != nil {
			cfg.ScanAllProcesses = *patch.ScanAllProcesses
		}
		if patch.ShowTrayIcon != nil {
			cfg.ShowTrayIcon = *patch.ShowTrayIcon
		}
		if patch.Catalog != nil {
			secret := cfg.Catalog.SteamGridDBAPIKey
			cfg.Catalog = *patch.Catalog
			if cfg.Catalog.SteamGridDBAPIKey == "" {
				cfg.Catalog.SteamGridDBAPIKey = secret
			}
		}
		if patch.Images != nil {
			cfg.Images = *patch.Images
		}
		if patch.Detection != nil {
			cfg.Detection = *patch.Detection
		}
		if srv.OnSettingsSaved != nil {
			if err := srv.OnSettingsSaved(cfg); err != nil {
				writeError(w, err.Error(), 500)
				return
			}
		}
		if patch.AutoDetect != nil {
			if srv.OnAutoDetectSet != nil {
				srv.OnAutoDetectSet(*patch.AutoDetect)
			} else {
				srv.state.SetAutoDetect(*patch.AutoDetect)
			}
		}
		writeJSON(w, settingsFromConfig(cfg, srv.state.AutoDetectEnabled()))

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (srv *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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

func (srv *Server) handleCatalogStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if srv.catalogStore == nil {
		writeJSON(w, map[string]any{"enabled": false})
		return
	}
	ctx := r.Context()
	entryCount, _ := srv.catalogStore.CountEntries(ctx)
	aliasCount, _ := srv.catalogStore.CountAliases(ctx)
	writeJSON(w, map[string]any{
		"enabled":     true,
		"entry_count": entryCount,
		"alias_count": aliasCount,
	})
}

func (srv *Server) handleCatalogSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if srv.catalogStore == nil {
		writeError(w, "catalog disabled", 503)
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, "missing q parameter", 400)
		return
	}
	results, err := srv.catalogStore.SearchAll(r.Context(), q)
	if err != nil {
		writeError(w, err.Error(), 500)
		return
	}
	resp := make([]catalogEntryResponse, len(results))
	for i, e := range results {
		resp[i] = entryToResponse(e)
	}
	writeJSON(w, resp)
}

func (srv *Server) handleCatalogEntry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if srv.catalogStore == nil {
		writeError(w, "catalog disabled", 503)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/catalog/entries/")
	if id == "" {
		writeError(w, "entry id required", 400)
		return
	}
	entry, err := srv.catalogStore.GetEntry(r.Context(), id)
	if err != nil {
		writeError(w, err.Error(), 500)
		return
	}
	if entry == nil {
		http.Error(w, "not found", 404)
		return
	}
	writeJSON(w, entryToResponse(*entry))
}

type catalogEntryResponse struct {
	ID          string `json:"id"`
	Source      string `json:"source"`
	Title       string `json:"title"`
	ReleaseYear int    `json:"release_year,omitempty"`
	ImageURL    string `json:"image_url,omitempty"`
}

func entryToResponse(e catalog.Entry) catalogEntryResponse {
	return catalogEntryResponse{
		ID:          e.ID,
		Source:      e.Source,
		Title:       e.Title,
		ReleaseYear: e.ReleaseYear,
		ImageURL:    e.ImageURL,
	}
}

type refreshRequest struct {
	Source   string   `json:"source"`
	MaxPages int      `json:"max_pages"`
	Roots    []string `json:"roots,omitempty"`
}

func (srv *Server) handleCatalogRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if srv.catalogStore == nil {
		writeError(w, "catalog disabled", 503)
		return
	}
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid JSON", 400)
		return
	}
	if req.Source == "" {
		writeError(w, "source is required", 400)
		return
	}

	var src catalog.Source
	switch req.Source {
	case "steam_local":
		src = &catalog.SteamLocalSource{SteamPaths: req.Roots}
	case "steam_shortcuts":
		src = &catalog.SteamShortcutsSource{Paths: req.Roots}
	case "lutris_public":
		src = &catalog.LutrisPublicSource{}
	case "desktop":
		src = &catalog.DesktopSource{Roots: req.Roots}
	default:
		writeError(w, "unknown source", 400)
		return
	}

	opts := catalog.RefreshOptions{MaxPages: req.MaxPages}
	if err := src.Refresh(r.Context(), srv.catalogStore, opts); err != nil {
		writeError(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

type enrichRequest struct {
	BatchSize int `json:"batch_size"`
}

type enrichResponse struct {
	Status   string `json:"status"`
	Enriched int    `json:"enriched"`
	Enabled  bool   `json:"enabled"`
	Message  string `json:"message,omitempty"`
}

func (srv *Server) handleCatalogEnrich(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if srv.catalogStore == nil {
		writeError(w, "catalog disabled", 503)
		return
	}
	if srv.catalogEnricher == nil || !srv.catalogEnricher.Enabled() {
		writeJSON(w, enrichResponse{Status: "ok", Enabled: false, Message: "SteamGridDB API key not configured"})
		return
	}

	var req enrichRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid JSON", 400)
		return
	}

	enriched, err := srv.catalogEnricher.EnrichMissingImages(r.Context(), req.BatchSize)
	if err != nil {
		writeJSON(w, enrichResponse{Status: "error", Enabled: true, Enriched: enriched, Message: err.Error()})
		return
	}
	writeJSON(w, enrichResponse{Status: "ok", Enabled: true, Enriched: enriched})
}

func (srv *Server) handleCatalogProfileFromEntry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if srv.catalogStore == nil {
		writeError(w, "catalog disabled", 503)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/catalog/profiles/from-entry/")
	if id == "" {
		writeError(w, "entry id required", 400)
		return
	}
	entry, err := srv.catalogStore.GetEntry(r.Context(), id)
	if err != nil {
		writeError(w, err.Error(), 500)
		return
	}
	if entry == nil {
		http.Error(w, "not found", 404)
		return
	}

	match := profile.MatchRule{Type: profile.MatchProcessName, Value: entry.Title}
	if aliases, err := srv.catalogStore.AliasesForEntry(r.Context(), entry.ID); err == nil {
		for _, a := range aliases {
			if a.Kind == catalog.AliasExecutable && a.Value != "" {
				match.Value = a.Value
				break
			}
		}
	}

	p := profile.Profile{
		Name:  entry.Title,
		Match: match,
		Activity: profile.Activity{
			Details:    "Playing " + entry.Title,
			LargeImage: "",
			LargeText:  entry.Title,
		},
		Priority: 10,
		Enabled:  true,
	}
	srv.profileManager.Add(p)
	srv.notifyProfilesChanged()
	writeJSON(w, map[string]string{"status": "ok"})
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

func isLocalOrigin(origin string) bool {
	return origin == "" ||
		strings.HasPrefix(origin, "http://localhost:") ||
		strings.HasPrefix(origin, "http://127.0.0.1:") ||
		origin == "http://localhost" ||
		origin == "http://127.0.0.1"
}

func isSafeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

func withSecurity(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		local := isLocalOrigin(origin)
		if local {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Picord-Token")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(200)
			return
		}

		// Reject cross-origin mutating requests.
		if !isSafeMethod(r.Method) && !local {
			writeError(w, "Forbidden", http.StatusForbidden)
			return
		}

		// Require token for unsafe methods when token protection is active.
		if token != "" && !isSafeMethod(r.Method) {
			if r.Header.Get("X-Picord-Token") != token {
				writeError(w, "Forbidden", http.StatusForbidden)
				return
			}
		}

		// Require correct Content-Type for JSON body endpoints.
		if !isSafeMethod(r.Method) && strings.HasPrefix(r.URL.Path, "/api/") {
			ct := r.Header.Get("Content-Type")
			if ct != "" && !strings.Contains(ct, "application/json") {
				writeError(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
				return
			}
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
		log.Printf("Picord API: http://%s\n", addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v\n", err)
		}
	}()
	return httpServer
}

func (srv *Server) handleLocalIcon(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Extract hash key from /assets/picord-icons/{hash}
	key := strings.TrimPrefix(r.URL.Path, "/assets/picord-icons/")
	if key == "" || strings.Contains(key, "/") {
		http.NotFound(w, r)
		return
	}
	path, ok := iconfinder.LookupPath(key)
	if !ok {
		http.NotFound(w, r)
		return
	}
	// Prevent traversal outside the registered set.
	http.ServeFile(w, r, path)
}
