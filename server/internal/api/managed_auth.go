package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type managedSession struct {
	tenantID string
	githubID string
	expires  time.Time
}

const managedSessionCookie = "jellyhere_session"
const managedStateCookie = "jellyhere_oauth_state"

func tokenHash(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func (s *managedService) sessionOwner(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(managedSessionCookie)
	if err != nil || len(cookie.Value) != 64 {
		return "", false
	}
	s.authMu.Lock()
	defer s.authMu.Unlock()
	session, ok := s.sessions[tokenHash(cookie.Value)]
	if !ok || time.Now().After(session.expires) {
		delete(s.sessions, tokenHash(cookie.Value))
		return "", false
	}
	return session.tenantID, true
}

func (s *managedService) ownerMatchesState(r *http.Request, tenantID string, state *managedState) bool {
	if hash, ok := bearerHash(r); ok && state.Tenants[tenantID].OwnerHash == hash {
		return true
	}
	if id, ok := s.sessionOwner(r); ok && id == tenantID {
		for _, mapped := range state.Accounts {
			if mapped == id {
				return true
			}
		}
	}
	return false
}

func (s *managedService) validMutationOrigin(r *http.Request) bool {
	if _, err := r.Cookie(managedSessionCookie); err == nil {
		return s.publicURL != "" && r.Header.Get("Origin") == s.publicURL
	}
	if _, ok := bearerHash(r); ok {
		return true
	}
	return s.publicURL != "" && r.Header.Get("Origin") == s.publicURL
}

func (s *managedService) githubLogin(w http.ResponseWriter, r *http.Request) {
	if s.githubClientID == "" || s.githubClientSecret == "" || s.publicURL == "" {
		apiError(w, 503, "oauth_unavailable", "GitHub login is not configured")
		return
	}
	state, _, err := randomToken()
	if err != nil {
		managedError(w, err)
		return
	}
	s.authMu.Lock()
	for key, expires := range s.oauthStates {
		if time.Now().After(expires) {
			delete(s.oauthStates, key)
		}
	}
	if len(s.oauthStates) >= 1024 {
		s.authMu.Unlock()
		apiError(w, 429, "login_busy", "Too many pending logins; try again later")
		return
	}
	s.oauthStates[tokenHash(state)] = time.Now().Add(5 * time.Minute)
	s.authMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: managedStateCookie, Value: state, Path: "/auth/github", Secure: true,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 300})
	q := url.Values{"client_id": {s.githubClientID}, "redirect_uri": {s.publicURL + "/auth/github/callback"}, "state": {state}}
	http.Redirect(w, r, s.githubOAuthURL+"/login/oauth/authorize?"+q.Encode(), http.StatusFound)
}

func (s *managedService) githubCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	cookie, err := r.Cookie(managedStateCookie)
	if err != nil || len(state) != 64 || cookie.Value != state || r.URL.Query().Get("code") == "" {
		apiError(w, 400, "invalid_oauth_state", "GitHub login expired; try again")
		return
	}
	s.authMu.Lock()
	expires, ok := s.oauthStates[tokenHash(state)]
	delete(s.oauthStates, tokenHash(state))
	s.authMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: managedStateCookie, Path: "/auth/github", Secure: true, HttpOnly: true, MaxAge: -1})
	if !ok || time.Now().After(expires) {
		apiError(w, 400, "invalid_oauth_state", "GitHub login expired; try again")
		return
	}
	form := url.Values{"client_id": {s.githubClientID}, "client_secret": {s.githubClientSecret}, "code": {r.URL.Query().Get("code")},
		"redirect_uri": {s.publicURL + "/auth/github/callback"}}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.githubOAuthURL+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		managedError(w, err)
		return
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		apiError(w, 502, "github_unavailable", "GitHub login failed")
		return
	}
	defer resp.Body.Close()
	var exchange struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&exchange) != nil || exchange.AccessToken == "" {
		apiError(w, 502, "github_unavailable", "GitHub login failed")
		return
	}
	profileReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, s.githubAPIURL+"/user", nil)
	if err != nil {
		managedError(w, err)
		return
	}
	profileReq.Header.Set("Authorization", "Bearer "+exchange.AccessToken)
	profileReq.Header.Set("Accept", "application/vnd.github+json")
	profileReq.Header.Set("User-Agent", "JellyHere")
	profileResp, err := s.httpClient.Do(profileReq)
	if err != nil {
		apiError(w, 502, "github_unavailable", "GitHub profile lookup failed")
		return
	}
	defer profileResp.Body.Close()
	var profile struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}
	if profileResp.StatusCode != 200 || json.NewDecoder(io.LimitReader(profileResp.Body, 4096)).Decode(&profile) != nil || profile.ID <= 0 {
		apiError(w, 502, "github_unavailable", "GitHub profile lookup failed")
		return
	}
	githubID := strconv.FormatInt(profile.ID, 10)
	tenantID := "gh_" + githubID
	_, ownerHash, err := randomToken()
	if err != nil {
		managedError(w, err)
		return
	}
	err = s.change(func(state *managedState) error {
		if state.Accounts == nil {
			state.Accounts = make(map[string]string)
		}
		if id := state.Accounts[githubID]; id != "" {
			tenantID = id
			return nil
		}
		if len(state.Tenants) >= maxTenants {
			return managedFailure{429, "tenant_limit", "registration is currently full"}
		}
		if _, exists := state.Tenants[tenantID]; exists {
			return managedFailure{409, "tenant_exists", "account conflict"}
		}
		state.Tenants[tenantID] = managedTenant{OwnerHash: hex.EncodeToString(ownerHash[:]),
			Devices: make(map[string]string), Clients: make(map[string]managedClient)}
		state.Accounts[githubID] = tenantID
		return nil
	})
	if err != nil {
		managedError(w, err)
		return
	}
	session, _, err := randomToken()
	if err != nil {
		managedError(w, err)
		return
	}
	s.authMu.Lock()
	var oldestKey string
	var oldest time.Time
	count := 0
	for key, existing := range s.sessions {
		if time.Now().After(existing.expires) {
			delete(s.sessions, key)
			continue
		}
		if existing.githubID == githubID {
			count++
			if oldestKey == "" || existing.expires.Before(oldest) {
				oldestKey, oldest = key, existing.expires
			}
		}
	}
	if count >= 5 {
		delete(s.sessions, oldestKey)
	}
	s.sessions[tokenHash(session)] = managedSession{tenantID, githubID, time.Now().Add(7 * 24 * time.Hour)}
	s.authMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: managedSessionCookie, Value: session, Path: "/", Secure: true,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 7 * 24 * 3600})
	http.Redirect(w, r, s.publicURL+"/", http.StatusFound)
}

func (s *managedService) authSession(w http.ResponseWriter, r *http.Request) {
	id, ok := s.sessionOwner(r)
	if !ok {
		apiError(w, 401, "unauthorized", "login required")
		return
	}
	jsonResponse(w, 200, map[string]string{"tenant_id": id})
}

func (s *managedService) authLogout(w http.ResponseWriter, r *http.Request) {
	if !s.validMutationOrigin(r) {
		apiError(w, 403, "invalid_origin", "same-origin request required")
		return
	}
	if cookie, err := r.Cookie(managedSessionCookie); err == nil {
		s.authMu.Lock()
		delete(s.sessions, tokenHash(cookie.Value))
		s.authMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: managedSessionCookie, Path: "/", Secure: true, HttpOnly: true, MaxAge: -1})
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
