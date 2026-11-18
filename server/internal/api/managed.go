package api

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AsCat/jellyhere/server/internal/bridge"
	"github.com/gorilla/websocket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	managedStateVersion = 1
	maxTenants          = 100
	maxTenantDevices    = 8
	maxTenantClients    = 16
	maxMCPPerMinute     = 60
)

//go:embed managed.html
var managedHTML string

//go:embed managed.js
var managedJS string

// ManagedConfig enables an invite-only, single-process pilot. It is separate
// from the legacy self-hosted handler and never accepts its global tokens.
type ManagedConfig struct {
	StatePath          string
	OperatorToken      string
	PublicURL          string
	GitHubClientID     string
	GitHubClientSecret string
	GitHubOAuthURL     string // Tests may replace GitHub endpoints.
	GitHubAPIURL       string
	HTTPClient         *http.Client
	SMTPHost           string
	SMTPPort           string
	SMTPUser           string
	SMTPPassword       string
	SMTPFrom           string
}

type managedState struct {
	Version       int                            `json:"version"`
	Tenants       map[string]managedTenant       `json:"tenants"`
	Accounts      map[string]string              `json:"accounts,omitempty"` // GitHub numeric user ID -> tenant ID.
	EmailAccounts map[string]managedEmailAccount `json:"email_accounts,omitempty"`
	PendingEmails map[string]managedPendingEmail `json:"pending_emails,omitempty"` // Verification token hash -> pending account.
}

type managedEmailAccount struct {
	TenantID     string `json:"tenant_id"`
	PasswordHash string `json:"password_hash"`
}

type managedPendingEmail struct {
	Email        string `json:"email"`
	PasswordHash string `json:"password_hash"`
	TenantID     string `json:"tenant_id,omitempty"`
	Expires      int64  `json:"expires"`
}

type managedTenant struct {
	OwnerHash string                   `json:"owner_token_hash"`
	Devices   map[string]string        `json:"devices"` // Device ID -> token hash.
	Clients   map[string]managedClient `json:"clients"` // Client ID -> credential.
}

type managedClient struct {
	TokenHash string   `json:"token_hash"`
	Name      string   `json:"name,omitempty"`
	Scope     string   `json:"scope"` // viewer or controller.
	DeviceIDs []string `json:"device_ids"`
}

type managedService struct {
	mu                 sync.RWMutex
	path               string
	state              managedState
	hubs               map[string]*bridge.Hub
	op                 string
	usage              map[string]managedUsage
	publicURL          string
	githubClientID     string
	githubClientSecret string
	githubOAuthURL     string
	githubAPIURL       string
	httpClient         *http.Client
	authMu             sync.Mutex
	oauthStates        map[string]time.Time
	sessions           map[string]managedSession
	loginAttempts      map[string]managedLoginAttempt
	passwordSlots      chan struct{}
	smtpHost           string
	smtpPort           string
	smtpUser           string
	smtpPassword       string
	smtpFrom           string
	pairMu             sync.Mutex
	pairings           map[string]managedPairing
}

type managedUsage struct {
	started time.Time
	count   int
}

type managedFailure struct {
	status  int
	code    string
	message string
}

func (e managedFailure) Error() string { return e.message }

func managedError(w http.ResponseWriter, err error) {
	var failure managedFailure
	if errors.As(err, &failure) {
		apiError(w, failure.status, failure.code, failure.message)
		return
	}
	log.Printf("managed state error: %v", err)
	apiError(w, 500, "state_error", "could not save managed state")
}

func NewManagedHandler(cfg ManagedConfig) (http.Handler, error) {
	if cfg.StatePath == "" || len(cfg.OperatorToken) < 32 {
		return nil, errors.New("JELLYHERE_MANAGED_STATE and a 32-character or longer JELLYHERE_OPERATOR_TOKEN are required")
	}
	path, err := filepath.Abs(cfg.StatePath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	state := managedState{Version: managedStateVersion, Tenants: make(map[string]managedTenant)}
	data, err := os.ReadFile(path)
	if err == nil {
		info, statErr := os.Stat(path)
		if statErr != nil {
			return nil, statErr
		}
		if info.Mode().Perm()&0077 != 0 {
			return nil, fmt.Errorf("managed state file %s must have mode 0600", path)
		}
		if err := json.Unmarshal(data, &state); err != nil {
			return nil, fmt.Errorf("decode managed state: %w", err)
		}
		if state.Version != managedStateVersion || state.Tenants == nil {
			return nil, errors.New("unsupported managed state version")
		}
		if err := validateManagedState(state); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	publicURL := strings.TrimRight(cfg.PublicURL, "/")
	if publicURL != "" && !strings.HasPrefix(publicURL, "https://") {
		return nil, errors.New("JELLYHERE_PUBLIC_URL must be https://")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	s := &managedService{path: path, state: state, hubs: make(map[string]*bridge.Hub),
		op: cfg.OperatorToken, usage: make(map[string]managedUsage), publicURL: publicURL,
		githubClientID: cfg.GitHubClientID, githubClientSecret: cfg.GitHubClientSecret,
		githubOAuthURL: cfg.GitHubOAuthURL, githubAPIURL: cfg.GitHubAPIURL, httpClient: client,
		oauthStates: make(map[string]time.Time), sessions: make(map[string]managedSession),
		loginAttempts: make(map[string]managedLoginAttempt),
		passwordSlots: make(chan struct{}, 2),
		smtpHost:      cfg.SMTPHost, smtpPort: cfg.SMTPPort, smtpUser: cfg.SMTPUser,
		smtpPassword: cfg.SMTPPassword, smtpFrom: cfg.SMTPFrom,
		pairings: make(map[string]managedPairing)}
	if s.githubOAuthURL == "" {
		s.githubOAuthURL = "https://github.com"
	}
	if s.githubAPIURL == "" {
		s.githubAPIURL = "https://api.github.com"
	}
	for id := range state.Tenants {
		s.hubs[id] = bridge.NewHub()
	}
	return s.handler(), nil
}

func (s *managedService) change(fn func(*managedState) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	encoded, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	var next managedState
	if err := json.Unmarshal(encoded, &next); err != nil {
		return err
	}
	if err := fn(&next); err != nil {
		return err
	}
	if err := validateManagedState(next); err != nil {
		return err
	}
	if err := writeManagedState(s.path, next); err != nil {
		return err
	}
	s.state = next
	for id := range next.Tenants {
		if s.hubs[id] == nil {
			s.hubs[id] = bridge.NewHub()
		}
	}
	return nil
}

func (s *managedService) changeOwned(r *http.Request, tenantID string, fn func(*managedState) error) error {
	ownerID, ok := s.owner(r)
	if !ok || ownerID != tenantID || !s.validMutationOrigin(r) {
		return managedFailure{401, "unauthorized", "owner token required"}
	}
	return s.change(func(state *managedState) error {
		if !s.ownerMatchesState(r, tenantID, state) {
			return managedFailure{401, "unauthorized", "owner token required"}
		}
		return fn(state)
	})
}

func validateManagedState(state managedState) error {
	if state.Version != managedStateVersion || state.Tenants == nil || len(state.Tenants) > maxTenants {
		return errors.New("invalid managed state header")
	}
	seenHashes := make(map[string]bool)
	validHash := func(value string) bool {
		if len(value) != 64 || seenHashes[value] {
			return false
		}
		if _, err := hex.DecodeString(value); err != nil {
			return false
		}
		seenHashes[value] = true
		return true
	}
	for tenantID, tenant := range state.Tenants {
		if !validDeviceID.MatchString(tenantID) || !validHash(tenant.OwnerHash) ||
			tenant.Devices == nil || tenant.Clients == nil ||
			len(tenant.Devices) > maxTenantDevices || len(tenant.Clients) > maxTenantClients {
			return fmt.Errorf("invalid managed tenant %q", tenantID)
		}
		for id, hash := range tenant.Devices {
			if !validDeviceID.MatchString(id) || !validHash(hash) {
				return fmt.Errorf("invalid managed device in %q", tenantID)
			}
		}
		for id, client := range tenant.Clients {
			if !validDeviceID.MatchString(id) || !validHash(client.TokenHash) ||
				len(client.Name) > 128 || strings.ContainsAny(client.Name, "\r\n\x00") ||
				(client.Scope != "viewer" && client.Scope != "controller") ||
				len(client.DeviceIDs) == 0 || len(client.DeviceIDs) > maxTenantDevices {
				return fmt.Errorf("invalid managed client in %q", tenantID)
			}
			allowed := make(map[string]bool)
			for _, deviceID := range client.DeviceIDs {
				if _, exists := tenant.Devices[deviceID]; !exists || allowed[deviceID] {
					return fmt.Errorf("invalid client device scope in %q", tenantID)
				}
				allowed[deviceID] = true
			}
		}
	}
	for githubID, tenantID := range state.Accounts {
		if githubID == "" || state.Tenants[tenantID].OwnerHash == "" {
			return errors.New("invalid managed account")
		}
	}
	for email, account := range state.EmailAccounts {
		if !validManagedEmail(email) || state.Tenants[account.TenantID].OwnerHash == "" || !validPasswordHash(account.PasswordHash) {
			return errors.New("invalid email account")
		}
	}
	if len(state.PendingEmails) > 1000 {
		return errors.New("too many pending email accounts")
	}
	for hash, pending := range state.PendingEmails {
		if len(hash) != 64 || !validManagedEmail(pending.Email) || !validPasswordHash(pending.PasswordHash) || pending.Expires == 0 ||
			(pending.TenantID != "" && state.Tenants[pending.TenantID].OwnerHash == "") {
			return errors.New("invalid pending email account")
		}
		if _, err := hex.DecodeString(hash); err != nil {
			return errors.New("invalid email verification hash")
		}
	}
	return nil
}

func writeManagedState(path string, state managedState) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".jellyhere-state-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if err := json.NewEncoder(file).Encode(state); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func bearerHash(r *http.Request) (string, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || len(token) != 64 || strings.TrimSpace(token) != token {
		return "", false
	}
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:]), true
}

func (s *managedService) owner(r *http.Request) (string, bool) {
	hash, ok := bearerHash(r)
	if ok {
		s.mu.RLock()
		defer s.mu.RUnlock()
		for id, tenant := range s.state.Tenants {
			if tenant.OwnerHash == hash {
				return id, true
			}
		}
	}
	return s.sessionOwner(r)
}

func (s *managedService) device(r *http.Request, id string) (*bridge.Hub, string, bool) {
	hash, ok := bearerHash(r)
	if !ok {
		return nil, "", false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for tenantID, tenant := range s.state.Tenants {
		if tenant.Devices[id] == hash {
			return s.hubs[tenantID], tenantID, true
		}
	}
	return nil, "", false
}

type managedPrincipal struct {
	hub    *bridge.Hub
	client managedClient
}

func (s *managedService) client(r *http.Request) (managedPrincipal, bool) {
	hash, ok := bearerHash(r)
	if !ok {
		return managedPrincipal{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for id, tenant := range s.state.Tenants {
		for _, client := range tenant.Clients {
			if client.TokenHash != hash {
				continue
			}
			allowed := make([]string, 0, len(client.DeviceIDs))
			for _, deviceID := range client.DeviceIDs {
				if _, exists := tenant.Devices[deviceID]; exists {
					allowed = append(allowed, deviceID)
				}
			}
			client.DeviceIDs = allowed
			return managedPrincipal{hub: s.hubs[id], client: client}, true
		}
	}
	return managedPrincipal{}, false
}

func (s *managedService) handler() http.Handler {
	mux := http.NewServeMux()
	health := func(w http.ResponseWriter, _ *http.Request) { jsonResponse(w, 200, map[string]bool{"ok": true}) }
	mux.HandleFunc("GET /healthz", health)
	mux.HandleFunc("GET /health", health)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'unsafe-inline'; img-src data:; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		_, _ = w.Write([]byte(managedHTML))
	})
	mux.HandleFunc("GET /managed.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write([]byte(managedJS))
	})
	mux.HandleFunc("GET /auth/github", s.githubLogin)
	mux.HandleFunc("GET /auth/github/callback", s.githubCallback)
	mux.HandleFunc("GET /auth/session", s.authSession)
	mux.HandleFunc("POST /auth/email/login", s.emailLogin)
	mux.HandleFunc("POST /auth/email/register", s.emailRegister)
	mux.HandleFunc("POST /auth/email/link", s.emailLink)
	mux.HandleFunc("POST /auth/email/verify", s.emailVerify)
	mux.HandleFunc("GET /auth/config", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, 200, map[string]bool{"github_enabled": s.githubClientID != "" && s.githubClientSecret != "" && s.publicURL != "",
			"email_registration_enabled": s.emailEnabled()})
	})
	mux.HandleFunc("POST /auth/logout", s.authLogout)
	mux.HandleFunc("POST /tenant/pairings", s.createPairing)
	mux.HandleFunc("POST /pair/inspect", s.pairInfo)
	mux.HandleFunc("POST /pair/redeem", s.redeemPairing)
	mux.HandleFunc("POST /manage/tenants", s.createTenant)
	mux.HandleFunc("POST /manage/email-accounts", s.createOperatorEmailAccount)
	mux.HandleFunc("GET /tenant", s.tenantInfo)
	mux.HandleFunc("POST /tenant/devices", s.createDevice)
	mux.HandleFunc("DELETE /tenant/devices/{id}", s.deleteDevice)
	mux.HandleFunc("POST /tenant/clients", s.createClient)
	mux.HandleFunc("PATCH /tenant/clients/{id}", s.updateClient)
	mux.HandleFunc("DELETE /tenant/clients/{id}", s.deleteClient)
	mux.HandleFunc("POST /tenant/owner-token/rotate", s.rotateOwner)
	mux.HandleFunc("GET /device", s.connectDevice)
	mux.HandleFunc("/mcp", s.serveMCP)
	return mux
}

func (s *managedService) createTenant(w http.ResponseWriter, r *http.Request) {
	if !bearerMatches(r, s.op) {
		apiError(w, 401, "unauthorized", "operator token required")
		return
	}
	var in struct {
		TenantID string `json:"tenant_id"`
	}
	if decodeBody(r, &in) != nil || !validDeviceID.MatchString(in.TenantID) {
		apiError(w, 400, "invalid_request", "valid tenant_id required")
		return
	}
	token, hash, err := randomToken()
	if err != nil {
		managedError(w, err)
		return
	}
	err = s.change(func(state *managedState) error {
		if _, exists := state.Tenants[in.TenantID]; exists {
			return managedFailure{409, "tenant_exists", "tenant already exists"}
		}
		if len(state.Tenants) >= maxTenants {
			return managedFailure{429, "tenant_limit", "tenant limit reached"}
		}
		state.Tenants[in.TenantID] = managedTenant{OwnerHash: hex.EncodeToString(hash[:]),
			Devices: make(map[string]string), Clients: make(map[string]managedClient)}
		return nil
	})
	if err != nil {
		managedError(w, err)
		return
	}
	jsonResponse(w, 201, map[string]string{"tenant_id": in.TenantID, "owner_token": token})
}

func (s *managedService) tenantInfo(w http.ResponseWriter, r *http.Request) {
	id, ok := s.owner(r)
	if !ok {
		apiError(w, 401, "unauthorized", "owner token required")
		return
	}
	s.mu.RLock()
	tenant := s.state.Tenants[id]
	hub := s.hubs[id]
	s.mu.RUnlock()
	devices := make([]bridge.DeviceStatus, 0, len(tenant.Devices))
	for deviceID := range tenant.Devices {
		status, known := hub.LookupStatus(deviceID)
		if !known {
			status = bridge.DeviceStatus{ID: deviceID}
		}
		devices = append(devices, status)
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].ID < devices[j].ID })
	type clientView struct {
		ID        string   `json:"id"`
		Name      string   `json:"name"`
		Scope     string   `json:"scope"`
		DeviceIDs []string `json:"device_ids"`
	}
	clients := make([]clientView, 0, len(tenant.Clients))
	for clientID, client := range tenant.Clients {
		name := client.Name
		if name == "" {
			name = clientID
		}
		clients = append(clients, clientView{clientID, name, client.Scope, client.DeviceIDs})
	}
	sort.Slice(clients, func(i, j int) bool { return clients[i].ID < clients[j].ID })
	jsonResponse(w, 200, map[string]any{"tenant_id": id, "devices": devices, "clients": clients})
}

func (s *managedService) createDevice(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.owner(r)
	if !ok {
		apiError(w, 401, "unauthorized", "owner token required")
		return
	}
	var in struct {
		DeviceID string `json:"device_id"`
	}
	if decodeBody(r, &in) != nil || !validDeviceID.MatchString(in.DeviceID) {
		apiError(w, 400, "invalid_request", "valid device_id required")
		return
	}
	token, hash, err := randomToken()
	if err != nil {
		managedError(w, err)
		return
	}
	err = s.changeOwned(r, tenantID, func(state *managedState) error {
		tenant := state.Tenants[tenantID]
		if _, exists := tenant.Devices[in.DeviceID]; exists {
			return managedFailure{409, "device_exists", "device already exists"}
		}
		if len(tenant.Devices) >= maxTenantDevices {
			return managedFailure{429, "device_limit", "device limit reached"}
		}
		tenant.Devices[in.DeviceID] = hex.EncodeToString(hash[:])
		state.Tenants[tenantID] = tenant
		return nil
	})
	if err != nil {
		managedError(w, err)
		return
	}
	s.mu.RLock()
	hub := s.hubs[tenantID]
	s.mu.RUnlock()
	hub.Disconnect(in.DeviceID)
	jsonResponse(w, 201, map[string]string{"device_id": in.DeviceID, "device_token": token})
}

func (s *managedService) deleteDevice(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.owner(r)
	if !ok {
		apiError(w, 401, "unauthorized", "owner token required")
		return
	}
	id := r.PathValue("id")
	if !validDeviceID.MatchString(id) {
		apiError(w, 404, "device_not_found", "device not found")
		return
	}
	revoked := 0
	err := s.changeOwned(r, tenantID, func(state *managedState) error {
		tenant := state.Tenants[tenantID]
		if _, exists := tenant.Devices[id]; !exists {
			return managedFailure{404, "device_not_found", "device not found"}
		}
		delete(tenant.Devices, id)
		for clientID, client := range tenant.Clients {
			for _, allowedID := range client.DeviceIDs {
				if allowedID == id {
					delete(tenant.Clients, clientID)
					revoked++
					break
				}
			}
		}
		state.Tenants[tenantID] = tenant
		return nil
	})
	if err != nil {
		managedError(w, err)
		return
	}
	s.mu.RLock()
	hub := s.hubs[tenantID]
	s.mu.RUnlock()
	hub.Disconnect(id)
	jsonResponse(w, 200, map[string]any{"device_id": id, "revoked_clients": revoked})
}

func (s *managedService) createClient(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.owner(r)
	if !ok {
		apiError(w, 401, "unauthorized", "owner token required")
		return
	}
	var in struct {
		ClientID   string   `json:"client_id"`
		ClientName string   `json:"client_name"`
		Scope      string   `json:"scope"`
		DeviceIDs  []string `json:"device_ids"`
	}
	if decodeBody(r, &in) != nil || (in.ClientID != "" && !validDeviceID.MatchString(in.ClientID)) ||
		(in.ClientID == "" && strings.TrimSpace(in.ClientName) == "") ||
		len(in.ClientName) > 128 || strings.ContainsAny(in.ClientName, "\r\n\x00") ||
		(in.Scope != "viewer" && in.Scope != "controller") || len(in.DeviceIDs) == 0 || len(in.DeviceIDs) > maxTenantDevices {
		apiError(w, 400, "invalid_request", "client name, scope and at least one device are required")
		return
	}
	if in.ClientID == "" {
		in.ClientID = "client_" + requestID()[:12]
	}
	if in.ClientName == "" {
		in.ClientName = in.ClientID
	}
	seen := make(map[string]bool)
	for _, id := range in.DeviceIDs {
		if !validDeviceID.MatchString(id) || seen[id] {
			apiError(w, 400, "invalid_request", "device_ids must be unique valid IDs")
			return
		}
		seen[id] = true
	}
	token, hash, err := randomToken()
	if err != nil {
		managedError(w, err)
		return
	}
	err = s.changeOwned(r, tenantID, func(state *managedState) error {
		tenant := state.Tenants[tenantID]
		if _, exists := tenant.Clients[in.ClientID]; exists {
			return managedFailure{409, "client_exists", "client already exists"}
		}
		if len(tenant.Clients) >= maxTenantClients {
			return managedFailure{429, "client_limit", "client limit reached"}
		}
		for _, id := range in.DeviceIDs {
			if _, exists := tenant.Devices[id]; !exists {
				return managedFailure{400, "invalid_device", "all devices must belong to this tenant"}
			}
		}
		tenant.Clients[in.ClientID] = managedClient{TokenHash: hex.EncodeToString(hash[:]), Name: in.ClientName,
			Scope: in.Scope, DeviceIDs: append([]string(nil), in.DeviceIDs...)}
		state.Tenants[tenantID] = tenant
		return nil
	})
	if err != nil {
		managedError(w, err)
		return
	}
	jsonResponse(w, 201, map[string]any{"client_id": in.ClientID, "client_name": in.ClientName, "scope": in.Scope,
		"device_ids": in.DeviceIDs, "mcp_token": token})
}

func (s *managedService) updateClient(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.owner(r)
	if !ok {
		apiError(w, 401, "unauthorized", "owner token required")
		return
	}
	id := r.PathValue("id")
	if !validDeviceID.MatchString(id) {
		apiError(w, 404, "client_not_found", "client not found")
		return
	}
	var in struct {
		DeviceIDs []string `json:"device_ids"`
	}
	if decodeBody(r, &in) != nil || len(in.DeviceIDs) == 0 || len(in.DeviceIDs) > maxTenantDevices {
		apiError(w, 400, "invalid_request", "select at least one device")
		return
	}
	seen := make(map[string]bool)
	for _, deviceID := range in.DeviceIDs {
		if !validDeviceID.MatchString(deviceID) || seen[deviceID] {
			apiError(w, 400, "invalid_request", "device IDs must be unique and valid")
			return
		}
		seen[deviceID] = true
	}
	err := s.changeOwned(r, tenantID, func(state *managedState) error {
		tenant := state.Tenants[tenantID]
		client, exists := tenant.Clients[id]
		if !exists {
			return managedFailure{404, "client_not_found", "client not found"}
		}
		for _, deviceID := range in.DeviceIDs {
			if _, exists := tenant.Devices[deviceID]; !exists {
				return managedFailure{400, "invalid_device", "all devices must belong to this tenant"}
			}
		}
		client.DeviceIDs = append([]string(nil), in.DeviceIDs...)
		tenant.Clients[id] = client
		state.Tenants[tenantID] = tenant
		return nil
	})
	if err != nil {
		managedError(w, err)
		return
	}
	jsonResponse(w, 200, map[string]any{"client_id": id, "device_ids": in.DeviceIDs})
}

func (s *managedService) deleteClient(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.owner(r)
	if !ok {
		apiError(w, 401, "unauthorized", "owner token required")
		return
	}
	id := r.PathValue("id")
	if !validDeviceID.MatchString(id) {
		apiError(w, 404, "client_not_found", "client not found")
		return
	}
	err := s.changeOwned(r, tenantID, func(state *managedState) error {
		tenant := state.Tenants[tenantID]
		if _, exists := tenant.Clients[id]; !exists {
			return managedFailure{404, "client_not_found", "client not found"}
		}
		delete(tenant.Clients, id)
		state.Tenants[tenantID] = tenant
		return nil
	})
	if err != nil {
		managedError(w, err)
		return
	}
	jsonResponse(w, 200, map[string]bool{"revoked": true})
}

func (s *managedService) rotateOwner(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.owner(r)
	if !ok {
		apiError(w, 401, "unauthorized", "owner token required")
		return
	}
	token, hash, err := randomToken()
	if err != nil {
		managedError(w, err)
		return
	}
	err = s.changeOwned(r, tenantID, func(state *managedState) error {
		tenant := state.Tenants[tenantID]
		tenant.OwnerHash = hex.EncodeToString(hash[:])
		state.Tenants[tenantID] = tenant
		return nil
	})
	if err != nil {
		managedError(w, err)
		return
	}
	jsonResponse(w, 200, map[string]string{"owner_token": token})
}

func (s *managedService) connectDevice(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if !validDeviceID.MatchString(id) {
		apiError(w, 400, "invalid_device", "valid device id required")
		return
	}
	hub, tenantID, ok := s.device(r, id)
	if !ok {
		apiError(w, 401, "unauthorized", "device token required")
		return
	}
	upgrader := websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("managed device upgrade: %v", err)
		return
	}
	// A token revoked while the WebSocket upgrade was in progress cannot attach.
	if current, _, stillValid := s.device(r, id); !stillValid || current != hub {
		_ = conn.Close()
		return
	}
	log.Printf("managed tenant=%s device=%s connected", tenantID, id)
	hub.AttachWithReady(id, conn, func() {
		if current, _, valid := s.device(r, id); !valid || current != hub {
			hub.DisconnectConnection(id, conn)
		}
	})
}

func (s *managedService) serveMCP(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.client(r)
	if !ok {
		apiError(w, 401, "unauthorized", "client token required")
		return
	}
	hash, _ := bearerHash(r)
	s.mu.Lock()
	usage := s.usage[hash]
	now := time.Now()
	if now.Sub(usage.started) >= time.Minute {
		usage = managedUsage{started: now}
	}
	usage.count++
	s.usage[hash] = usage
	s.mu.Unlock()
	if usage.count > maxMCPPerMinute {
		apiError(w, 429, "rate_limit", "MCP request limit exceeded")
		return
	}
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return newScopedMCPServer(principal.hub, principal.client.DeviceIDs, principal.client.Scope == "viewer")
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	handler.ServeHTTP(w, r)
}
