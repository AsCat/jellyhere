package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	oauthReadScope    = "device.read"
	oauthControlScope = "device.read device.control"
	accessLifetime    = time.Hour
	refreshLifetime   = 30 * 24 * time.Hour
)

type managedOAuthClient struct {
	Name         string   `json:"name"`
	RedirectURIs []string `json:"redirect_uris"`
	CreatedAt    int64    `json:"created_at"`
}
type managedOAuthCode struct {
	ClientID    string   `json:"client_id"`
	TenantID    string   `json:"tenant_id"`
	RedirectURI string   `json:"redirect_uri"`
	Challenge   string   `json:"challenge"`
	Scope       string   `json:"scope"`
	DeviceIDs   []string `json:"device_ids"`
	Expires     int64    `json:"expires"`
}
type managedOAuthGrant struct {
	ClientID       string   `json:"client_id"`
	TenantID       string   `json:"tenant_id"`
	Scope          string   `json:"scope"`
	DeviceIDs      []string `json:"device_ids"`
	AccessHash     string   `json:"access_hash"`
	AccessExpires  int64    `json:"access_expires"`
	RefreshHash    string   `json:"refresh_hash"`
	RefreshExpires int64    `json:"refresh_expires"`
}
type managedOAuthConsent struct {
	SessionHash string
	ClientID    string
	TenantID    string
	RedirectURI string
	Challenge   string
	Scope       string
	State       string
	Expires     time.Time
}

func oauthScopeValid(scope string) bool { return scope == oauthReadScope || scope == oauthControlScope }
func oauthHashValid(hash string) bool {
	_, err := hex.DecodeString(hash)
	return len(hash) == 64 && err == nil
}

func oauthRedirectValid(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Fragment != "" || u.User != nil {
		return false
	}
	if u.Scheme != "https" || u.Hostname() == "" || u.Port() != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, suffix := range []string{".workbuddy.cn", ".codebuddy.cn", ".workbuddy.ai"} {
		if strings.HasSuffix(host, suffix) && len(host) > len(suffix) {
			return true
		}
	}
	return host == "copilot.tencent.com" || host == "staging-codebuddy.tencent.com"
}
func oauthChallengeValid(v string) bool {
	if len(v) < 43 || len(v) > 128 {
		return false
	}
	for _, c := range v {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == '~') {
			return false
		}
	}
	return true
}
func oauthDevicesValid(ids []string, tenant managedTenant) bool {
	if len(ids) == 0 || len(ids) > maxTenantDevices {
		return false
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if _, ok := tenant.Devices[id]; !ok || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}
func validateManagedOAuthState(state managedState) error {
	if len(state.OAuthClients) > maxOAuthClients || len(state.OAuthCodes) > maxOAuthCodes || len(state.OAuthGrants) > maxOAuthGrants {
		return errors.New("too many OAuth records")
	}
	for id, client := range state.OAuthClients {
		if !validDeviceID.MatchString(id) || (client.Name != "WorkBuddy Connector" && client.Name != "CodeBuddy") || len(client.RedirectURIs) == 0 || len(client.RedirectURIs) > 10 || client.CreatedAt <= 0 {
			return errors.New("invalid OAuth client")
		}
		seen := map[string]bool{}
		for _, uri := range client.RedirectURIs {
			if !oauthRedirectValid(uri) || seen[uri] {
				return errors.New("invalid OAuth redirect")
			}
			seen[uri] = true
		}
	}
	for hash, code := range state.OAuthCodes {
		client := state.OAuthClients[code.ClientID]
		if !oauthHashValid(hash) || client.Name == "" || !containsString(client.RedirectURIs, code.RedirectURI) || !oauthChallengeValid(code.Challenge) || !oauthScopeValid(code.Scope) || code.Expires <= 0 || !oauthDevicesValid(code.DeviceIDs, state.Tenants[code.TenantID]) {
			return errors.New("invalid OAuth code")
		}
	}
	seenTokens := map[string]bool{}
	for id, grant := range state.OAuthGrants {
		if !validDeviceID.MatchString(id) || state.OAuthClients[grant.ClientID].Name == "" || !oauthScopeValid(grant.Scope) || !oauthDevicesValid(grant.DeviceIDs, state.Tenants[grant.TenantID]) || !oauthHashValid(grant.AccessHash) || !oauthHashValid(grant.RefreshHash) || grant.AccessExpires <= 0 || grant.RefreshExpires <= 0 || seenTokens[grant.AccessHash] || seenTokens[grant.RefreshHash] {
			return errors.New("invalid OAuth grant")
		}
		seenTokens[grant.AccessHash], seenTokens[grant.RefreshHash] = true, true
	}
	return nil
}
func containsString(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func (s *managedService) oauthResourceMetadata(w http.ResponseWriter, _ *http.Request) {
	if s.publicURL == "" {
		apiError(w, 503, "unavailable", "OAuth is not configured")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, 200, map[string]any{"resource": s.publicURL + "/mcp", "authorization_servers": []string{s.publicURL}, "scopes_supported": []string{oauthReadScope, "device.control"}, "bearer_methods_supported": []string{"header"}})
}
func (s *managedService) oauthServerMetadata(w http.ResponseWriter, _ *http.Request) {
	if s.publicURL == "" {
		apiError(w, 503, "unavailable", "OAuth is not configured")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, 200, map[string]any{"issuer": s.publicURL, "authorization_endpoint": s.publicURL + "/oauth/authorize", "token_endpoint": s.publicURL + "/oauth/token", "registration_endpoint": s.publicURL + "/oauth/register", "revocation_endpoint": s.publicURL + "/oauth/revoke", "response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token"}, "token_endpoint_auth_methods_supported": []string{"none"}, "code_challenge_methods_supported": []string{"S256"}, "scopes_supported": []string{oauthReadScope, "device.control"}})
}
func oauthError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, status, map[string]string{"error": code, "error_description": description})
}
func (s *managedService) oauthRegister(w http.ResponseWriter, r *http.Request) {
	if s.publicURL == "" {
		oauthError(w, 503, "server_error", "OAuth is not configured")
		return
	}
	if !s.allowEmailAttempt("oauth-register:"+emailClientIP(r), 30, time.Hour) {
		oauthError(w, 429, "rate_limited", "too many registrations")
		return
	}
	var in struct {
		ClientName    string   `json:"client_name"`
		RedirectURIs  []string `json:"redirect_uris"`
		GrantTypes    []string `json:"grant_types"`
		ResponseTypes []string `json:"response_types"`
		TokenAuth     string   `json:"token_endpoint_auth_method"`
	}
	if r.ContentLength > 8192 || json.NewDecoder(io.LimitReader(r.Body, 8193)).Decode(&in) != nil || (in.ClientName != "WorkBuddy Connector" && in.ClientName != "CodeBuddy") || len(in.RedirectURIs) == 0 || len(in.RedirectURIs) > 10 || (in.TokenAuth != "" && in.TokenAuth != "none") {
		oauthError(w, 400, "invalid_client_metadata", "unsupported client metadata")
		return
	}
	for _, v := range in.GrantTypes {
		if v != "authorization_code" && v != "refresh_token" {
			oauthError(w, 400, "invalid_client_metadata", "unsupported grant type")
			return
		}
	}
	for _, v := range in.ResponseTypes {
		if v != "code" {
			oauthError(w, 400, "invalid_client_metadata", "unsupported response type")
			return
		}
	}
	seen := map[string]bool{}
	for _, uri := range in.RedirectURIs {
		if !oauthRedirectValid(uri) || seen[uri] {
			oauthError(w, 400, "invalid_redirect_uri", "redirect must be a WorkBuddy callback")
			return
		}
		seen[uri] = true
	}
	id := "oauth_" + requestID()[:24]
	created := time.Now().Unix()
	if err := s.change(func(state *managedState) error {
		if state.OAuthClients == nil {
			state.OAuthClients = map[string]managedOAuthClient{}
		}
		if len(state.OAuthClients) >= maxOAuthClients {
			return managedFailure{429, "client_limit", "registration limit reached"}
		}
		state.OAuthClients[id] = managedOAuthClient{in.ClientName, in.RedirectURIs, created}
		return nil
	}); err != nil {
		managedError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, 201, map[string]any{"client_id": id, "client_id_issued_at": created, "client_name": in.ClientName, "redirect_uris": in.RedirectURIs, "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}, "token_endpoint_auth_method": "none"})
}
func (s *managedService) oauthAuthorizeParams(r *http.Request) (managedOAuthConsent, error) {
	q := r.URL.Query()
	clientID := q.Get("client_id")
	redirect := q.Get("redirect_uri")
	scope := strings.Join(strings.Fields(q.Get("scope")), " ")
	challenge := q.Get("code_challenge")
	state := q.Get("state")
	s.mu.RLock()
	client := s.state.OAuthClients[clientID]
	s.mu.RUnlock()
	if client.Name == "" || !containsString(client.RedirectURIs, redirect) || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || !oauthChallengeValid(challenge) || !oauthScopeValid(scope) || len(state) > 512 || (q.Get("resource") != "" && q.Get("resource") != s.publicURL+"/mcp") {
		return managedOAuthConsent{}, errors.New("invalid authorization request")
	}
	return managedOAuthConsent{ClientID: clientID, RedirectURI: redirect, Challenge: challenge, Scope: scope, State: state}, nil
}

var oauthConsentHTML = template.Must(template.New("consent").Parse(`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>授权连接 · JellyHere</title><style>body{font:16px system-ui;background:#f6f8fc;color:#202c46;max-width:560px;margin:6vh auto;padding:20px}main{background:white;border:1px solid #e3e8f0;border-radius:22px;padding:32px;box-shadow:0 12px 40px #202c4610}h1{font-size:25px}p{line-height:1.6;color:#526179}.device{display:block;padding:12px;border:1px solid #dce3ee;border-radius:12px;margin:10px 0}button{border:0;border-radius:10px;padding:12px 18px;cursor:pointer;background:#4677e8;color:white;font-weight:650}.deny{background:#edf1f8;color:#34435c}small{color:#65748b}</style><main><small>JellyHere · 连接授权</small><h1>允许 {{.ClientName}} 访问设备？</h1><p>请选择要授权的设备；新设备不会自动加入此授权。</p><form method="post" action="/oauth/authorize"><input type="hidden" name="consent" value="{{.Nonce}}">{{range .Devices}}<label class="device"><input type="checkbox" name="device_id" value="{{.}}"> {{.}}</label>{{else}}<p>当前空间还没有设备，请先添加设备。</p>{{end}}{{if .ControlRequested}}<p>选择权限：</p><label class="device"><input type="radio" name="scope" value="device.read" checked> 仅查看屏幕、设备状态与应用信息</label><label class="device"><input type="radio" name="scope" value="device.read device.control"> 查看并操作手机（点击、滑动、输入等）</label>{{else}}<p>权限：仅查看屏幕、设备状态与应用信息。</p>{{end}}<p><button name="decision" value="allow" {{if not .Devices}}disabled{{end}}>授权所选设备</button> <button class="deny" name="decision" value="deny">取消</button></p></form><small>授权后可随时在 JellyHub 控制台撤销。</small></main></html>`))

func (s *managedService) oauthAuthorizePage(w http.ResponseWriter, r *http.Request) {
	if s.publicURL == "" {
		oauthError(w, 503, "server_error", "OAuth is not configured")
		return
	}
	pending, err := s.oauthAuthorizeParams(r)
	if err != nil {
		oauthError(w, 400, "invalid_request", err.Error())
		return
	}
	tenantID, ok := s.sessionOwner(r)
	if !ok {
		http.Redirect(w, r, "/?oauth_return="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
		return
	}
	prior, err := r.Cookie(managedSessionCookie)
	if err != nil {
		oauthError(w, 401, "login_required", "login required")
		return
	}
	s.mu.RLock()
	tenant := s.state.Tenants[tenantID]
	client := s.state.OAuthClients[pending.ClientID]
	s.mu.RUnlock()
	ids := make([]string, 0, len(tenant.Devices))
	for id := range tenant.Devices {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	nonce, _, err := randomToken()
	if err != nil {
		managedError(w, err)
		return
	}
	pending.TenantID = tenantID
	pending.SessionHash = tokenHash(prior.Value)
	pending.Expires = time.Now().Add(5 * time.Minute)
	s.consentMu.Lock()
	for k, v := range s.consents {
		if time.Now().After(v.Expires) {
			delete(s.consents, k)
		}
	}
	if len(s.consents) >= 1024 {
		s.consentMu.Unlock()
		oauthError(w, 429, "temporarily_unavailable", "too many pending authorizations")
		return
	}
	s.consents[tokenHash(nonce)] = pending
	s.consentMu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = oauthConsentHTML.Execute(w, map[string]any{"ClientName": client.Name, "ControlRequested": pending.Scope == oauthControlScope, "Nonce": nonce, "Devices": ids})
}
func oauthRedirect(w http.ResponseWriter, r *http.Request, redirect, state, code, value string) {
	u, _ := url.Parse(redirect)
	q := u.Query()
	q.Set(code, value)
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}
func (s *managedService) oauthAuthorizeSubmit(w http.ResponseWriter, r *http.Request) {
	// The one-time consent nonce is bound to the login session and provides CSRF
	// protection. Some embedded browsers omit Origin (or send "null") for a
	// top-level form navigation, so do not require that header here.
	if origin := r.Header.Get("Origin"); origin != "" && origin != "null" && origin != s.publicURL {
		oauthError(w, 403, "invalid_request", "invalid form origin")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if r.ContentLength > 8192 || r.ParseForm() != nil {
		oauthError(w, 400, "invalid_request", "invalid authorization form")
		return
	}
	nonce := r.PostForm.Get("consent")
	if len(nonce) != 64 {
		oauthError(w, 400, "invalid_request", "authorization expired")
		return
	}
	s.consentMu.Lock()
	pending, ok := s.consents[tokenHash(nonce)]
	delete(s.consents, tokenHash(nonce))
	s.consentMu.Unlock()
	cookie, err := r.Cookie(managedSessionCookie)
	if err != nil || !ok || time.Now().After(pending.Expires) || tokenHash(cookie.Value) != pending.SessionHash {
		oauthError(w, 400, "invalid_request", "authorization expired")
		return
	}
	tenantID, ok := s.sessionOwner(r)
	if !ok || tenantID != pending.TenantID {
		oauthError(w, 401, "login_required", "login required")
		return
	}
	if r.PostForm.Get("decision") == "deny" {
		oauthRedirect(w, r, pending.RedirectURI, pending.State, "error", "access_denied")
		return
	}
	if r.PostForm.Get("decision") != "allow" {
		oauthError(w, 400, "invalid_request", "invalid decision")
		return
	}
	grantedScope := oauthReadScope
	if pending.Scope == oauthControlScope {
		selected := r.PostForm.Get("scope")
		if selected != oauthReadScope && selected != oauthControlScope {
			oauthError(w, 400, "invalid_scope", "select a valid permission")
			return
		}
		grantedScope = selected
	}
	ids := r.PostForm["device_id"]
	code, _, err := randomToken()
	if err != nil {
		managedError(w, err)
		return
	}
	err = s.change(func(st *managedState) error {
		if !oauthDevicesValid(ids, st.Tenants[tenantID]) {
			return managedFailure{400, "invalid_device", "select at least one device from your space"}
		}
		if st.OAuthCodes == nil {
			st.OAuthCodes = map[string]managedOAuthCode{}
		}
		now := time.Now().Unix()
		for k, v := range st.OAuthCodes {
			if v.Expires <= now {
				delete(st.OAuthCodes, k)
			}
		}
		if len(st.OAuthCodes) >= maxOAuthCodes {
			return managedFailure{429, "authorization_limit", "too many pending codes"}
		}
		st.OAuthCodes[tokenHash(code)] = managedOAuthCode{pending.ClientID, tenantID, pending.RedirectURI, pending.Challenge, grantedScope, append([]string(nil), ids...), time.Now().Add(5 * time.Minute).Unix()}
		return nil
	})
	if err != nil {
		managedError(w, err)
		return
	}
	oauthRedirect(w, r, pending.RedirectURI, pending.State, "code", code)
}
func (s *managedService) oauthToken(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > 8192 || r.ParseForm() != nil {
		oauthError(w, 400, "invalid_request", "invalid form")
		return
	}
	clientID := r.PostForm.Get("client_id")
	if clientID == "" {
		oauthError(w, 401, "invalid_client", "client_id required")
		return
	}
	if resource := r.PostForm.Get("resource"); resource != "" && resource != s.publicURL+"/mcp" {
		oauthError(w, 400, "invalid_target", "invalid resource")
		return
	}
	var access, refresh string
	var scope string
	var err error
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		code := r.PostForm.Get("code")
		verifier := r.PostForm.Get("code_verifier")
		redirect := r.PostForm.Get("redirect_uri")
		if len(code) != 64 || !oauthChallengeValid(verifier) {
			oauthError(w, 400, "invalid_grant", "invalid code or verifier")
			return
		}
		computed := sha256.Sum256([]byte(verifier))
		challenge := base64.RawURLEncoding.EncodeToString(computed[:])
		access, _, err = randomToken()
		if err != nil {
			managedError(w, err)
			return
		}
		refresh, _, err = randomToken()
		if err != nil {
			managedError(w, err)
			return
		}
		err = s.change(func(st *managedState) error {
			entry, ok := st.OAuthCodes[tokenHash(code)]
			if !ok || entry.Expires <= time.Now().Unix() || entry.ClientID != clientID || entry.RedirectURI != redirect || subtle.ConstantTimeCompare([]byte(entry.Challenge), []byte(challenge)) != 1 {
				return managedFailure{400, "invalid_grant", "code is invalid or expired"}
			}
			if !oauthDevicesValid(entry.DeviceIDs, st.Tenants[entry.TenantID]) {
				return managedFailure{400, "invalid_grant", "device authorization changed"}
			}
			delete(st.OAuthCodes, tokenHash(code))
			if st.OAuthGrants == nil {
				st.OAuthGrants = map[string]managedOAuthGrant{}
			}
			now := time.Now().Unix()
			for k, v := range st.OAuthGrants {
				if v.RefreshExpires <= now {
					delete(st.OAuthGrants, k)
				}
			}
			if len(st.OAuthGrants) >= maxOAuthGrants {
				return managedFailure{429, "grant_limit", "too many active grants"}
			}
			id := "grant_" + requestID()[:24]
			scope = entry.Scope
			st.OAuthGrants[id] = managedOAuthGrant{clientID, entry.TenantID, scope, entry.DeviceIDs, tokenHash(access), time.Now().Add(accessLifetime).Unix(), tokenHash(refresh), time.Now().Add(refreshLifetime).Unix()}
			return nil
		})
	case "refresh_token":
		old := r.PostForm.Get("refresh_token")
		if len(old) != 64 {
			oauthError(w, 400, "invalid_grant", "refresh token required")
			return
		}
		access, _, err = randomToken()
		if err != nil {
			managedError(w, err)
			return
		}
		refresh, _, err = randomToken()
		if err != nil {
			managedError(w, err)
			return
		}
		err = s.change(func(st *managedState) error {
			for id, grant := range st.OAuthGrants {
				if grant.RefreshHash != tokenHash(old) {
					continue
				}
				if grant.ClientID != clientID || grant.RefreshExpires <= time.Now().Unix() || !oauthDevicesValid(grant.DeviceIDs, st.Tenants[grant.TenantID]) {
					return managedFailure{400, "invalid_grant", "refresh token is invalid"}
				}
				grant.AccessHash = tokenHash(access)
				grant.AccessExpires = time.Now().Add(accessLifetime).Unix()
				grant.RefreshHash = tokenHash(refresh)
				grant.RefreshExpires = time.Now().Add(refreshLifetime).Unix()
				st.OAuthGrants[id] = grant
				scope = grant.Scope
				return nil
			}
			return managedFailure{400, "invalid_grant", "refresh token is invalid"}
		})
	default:
		oauthError(w, 400, "unsupported_grant_type", "unsupported grant type")
		return
	}
	if err != nil {
		var failure managedFailure
		if errors.As(err, &failure) {
			oauthError(w, failure.status, failure.code, failure.message)
		} else {
			managedError(w, err)
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, 200, map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": int(accessLifetime.Seconds()), "refresh_token": refresh, "scope": scope})
}
func (s *managedService) oauthRevoke(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > 8192 || r.ParseForm() != nil {
		oauthError(w, 400, "invalid_request", "invalid form")
		return
	}
	clientID := r.PostForm.Get("client_id")
	raw := r.PostForm.Get("token")
	if clientID == "" || len(raw) != 64 {
		oauthError(w, 400, "invalid_request", "client and token required")
		return
	}
	err := s.change(func(st *managedState) error {
		for id, g := range st.OAuthGrants {
			if g.ClientID == clientID && (g.AccessHash == tokenHash(raw) || g.RefreshHash == tokenHash(raw)) {
				delete(st.OAuthGrants, id)
				break
			}
		}
		return nil
	})
	if err != nil {
		managedError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, 200, map[string]any{})
}
func (s *managedService) deleteOAuthGrant(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.owner(r)
	if !ok {
		apiError(w, 401, "unauthorized", "owner required")
		return
	}
	id := r.PathValue("id")
	if !validDeviceID.MatchString(id) {
		apiError(w, 404, "grant_not_found", "connection not found")
		return
	}
	err := s.changeOwned(r, tenantID, func(st *managedState) error {
		g, ok := st.OAuthGrants[id]
		if !ok || g.TenantID != tenantID {
			return managedFailure{404, "grant_not_found", "connection not found"}
		}
		delete(st.OAuthGrants, id)
		return nil
	})
	if err != nil {
		managedError(w, err)
		return
	}
	jsonResponse(w, 200, map[string]bool{"revoked": true})
}
