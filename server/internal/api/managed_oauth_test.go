package api

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestManagedWorkBuddyOAuthLifecycle(t *testing.T) {
	const publicURL = "https://jelly.test"
	handler, err := NewManagedHandler(ManagedConfig{StatePath: filepath.Join(t.TempDir(), "state.json"), OperatorToken: testOperatorToken, PublicURL: publicURL})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	status, body := managedRequest(t, srv, "POST", "/manage/tenants", testOperatorToken, map[string]any{"tenant_id": "alice"})
	owner := mustManagedToken(t, status, body, "owner_token")
	status, _ = managedRequest(t, srv, "POST", "/manage/email-accounts", testOperatorToken, map[string]any{"tenant_id": "alice", "email": "alice@example.test", "password": "a-long-test-password"})
	if status != 201 {
		t.Fatalf("email account status=%d", status)
	}
	for _, id := range []string{"first", "second"} {
		status, _ = managedRequest(t, srv, "POST", "/tenant/devices", owner, map[string]any{"device_id": id})
		if status != 201 {
			t.Fatalf("device %s status=%d", id, status)
		}
	}
	status, _ = managedRequest(t, srv, "POST", "/oauth/register", "", map[string]any{"client_name": "WorkBuddy Connector", "redirect_uris": []string{"https://evil.example/callback"}})
	if status != 400 {
		t.Fatalf("unsafe redirect status=%d", status)
	}
	callback := "https://client.workbuddy.cn/mcp/callback"
	status, body = managedRequest(t, srv, "POST", "/oauth/register", "", map[string]any{"client_name": "WorkBuddy Connector", "redirect_uris": []string{callback}, "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}, "token_endpoint_auth_method": "none"})
	if status != 201 {
		t.Fatalf("register status=%d body=%v", status, body)
	}
	clientID := body["client_id"].(string)
	verifier := strings.Repeat("v", 43)
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	q := url.Values{"client_id": {clientID}, "redirect_uri": {callback}, "response_type": {"code"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}, "scope": {oauthControlScope}, "state": {"opaque-state"}, "resource": {publicURL + "/mcp"}}
	authorizePath := "/oauth/authorize?" + q.Encode()
	resp, err := noRedirect.Get(srv.URL + authorizePath)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 302 || !strings.Contains(resp.Header.Get("Location"), "oauth_return=") {
		t.Fatalf("login redirect: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	loginBody := `{"email":"alice@example.test","password":"a-long-test-password"}`
	req, _ := http.NewRequest("POST", srv.URL+"/auth/email/login", strings.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", publicURL)
	resp, err = noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("login status=%d", resp.StatusCode)
	}
	var session *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == managedSessionCookie {
			session = c
		}
	}
	if session == nil {
		t.Fatal("session cookie missing")
	}
	req, _ = http.NewRequest("GET", srv.URL+authorizePath, nil)
	req.AddCookie(session)
	resp, err = noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	page, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(page), "first") || !strings.Contains(string(page), "second") {
		t.Fatalf("consent page status=%d", resp.StatusCode)
	}
	for _, id := range []string{"first", "second"} {
		if !strings.Contains(string(page), `name="device_id" value="`+id+`" checked`) {
			t.Fatalf("device %s was not selected by default", id)
		}
	}
	nonceMatch := regexp.MustCompile(`name="consent" value="([a-f0-9]{64})"`).FindStringSubmatch(string(page))
	if len(nonceMatch) != 2 {
		t.Fatal("consent nonce missing")
	}
	form := url.Values{"consent": {nonceMatch[1]}, "decision": {"allow"}, "device_id": {"first"}, "scope": {oauthReadScope}}
	// A foreign Origin is rejected without consuming the single-use nonce.
	req, _ = http.NewRequest("POST", srv.URL+"/oauth/authorize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://attacker.example")
	req.AddCookie(session)
	resp, err = noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("foreign origin status=%d", resp.StatusCode)
	}
	// WorkBuddy's embedded browser can omit Origin on a form navigation.
	req, _ = http.NewRequest("POST", srv.URL+"/oauth/authorize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(session)
	resp, err = noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 303 {
		t.Fatalf("consent status=%d", resp.StatusCode)
	}
	firstLocation := resp.Header.Get("Location")
	// A repeated click must return the same authorization result, not an expiry error.
	req, _ = http.NewRequest("POST", srv.URL+"/oauth/authorize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(session)
	resp, err = noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 303 || resp.Header.Get("Location") != firstLocation {
		t.Fatalf("duplicate consent status=%d location=%s", resp.StatusCode, resp.Header.Get("Location"))
	}
	redirect, err := url.Parse(firstLocation)
	if err != nil {
		t.Fatal(err)
	}
	if redirect.Query().Get("state") != "opaque-state" {
		t.Fatal("state lost")
	}
	code := redirect.Query().Get("code")
	if len(code) != 64 {
		t.Fatal("code missing")
	}
	postForm := func(path string, v url.Values) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(v.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := noRedirect.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var b map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, b
	}
	exchange := url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {code}, "redirect_uri": {callback}, "code_verifier": {strings.Repeat("x", 43)}, "resource": {publicURL + "/mcp"}}
	status, _ = postForm("/oauth/token", exchange)
	if status != 400 {
		t.Fatalf("wrong PKCE status=%d", status)
	}
	exchange.Set("code_verifier", verifier)
	status, body = postForm("/oauth/token", exchange)
	if status != 200 {
		t.Fatalf("exchange status=%d body=%v", status, body)
	}
	access := body["access_token"].(string)
	refresh := body["refresh_token"].(string)
	if body["scope"] != oauthReadScope {
		t.Fatalf("selected read scope ignored: %v", body["scope"])
	}
	status, _ = postForm("/oauth/token", exchange)
	if status != 400 {
		t.Fatalf("code replay status=%d", status)
	}
	sessionMCP := managedMCPClient(t, srv, access)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tools, err := sessionMCP.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "tap" {
			t.Fatal("read-only grant exposes control")
		}
	}
	result, err := sessionMCP.CallTool(ctx, &mcp.CallToolParams{Name: "get_screen", Arguments: map[string]any{"deviceId": "second", "includeImage": false}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result)
	if !result.IsError || !strings.Contains(string(encoded), "device is not authorized") {
		t.Fatalf("device scope leaked: %s", encoded)
	}
	status, body = postForm("/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {refresh}})
	if status != 200 {
		t.Fatalf("refresh status=%d body=%v", status, body)
	}
	newAccess := body["access_token"].(string)
	newRefresh := body["refresh_token"].(string)
	status, _ = postForm("/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {refresh}})
	if status != 400 {
		t.Fatalf("old refresh status=%d", status)
	}
	for _, token := range []string{access, newAccess} {
		req, _ := http.NewRequest("POST", srv.URL+"/mcp", strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := noRedirect.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if token == access && resp.StatusCode != 401 {
			t.Fatalf("old access status=%d", resp.StatusCode)
		}
		if token == newAccess && resp.StatusCode == 401 {
			t.Fatal("new access rejected")
		}
	}
	status, _ = postForm("/oauth/revoke", url.Values{"client_id": {clientID}, "token": {newRefresh}})
	if status != 200 {
		t.Fatalf("revoke status=%d", status)
	}
	req, _ = http.NewRequest("POST", srv.URL+"/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+newAccess)
	resp, err = noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("revoked access status=%d", resp.StatusCode)
	}
	resp, err = noRedirect.Get(srv.URL + "/.well-known/oauth-protected-resource")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("metadata status=%d", resp.StatusCode)
	}
	// A second, explicit control grant exposes write tools for only the selected device.
	req, _ = http.NewRequest("GET", srv.URL+authorizePath, nil)
	req.AddCookie(session)
	resp, err = noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	page, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	nonceMatch = regexp.MustCompile(`name="consent" value="([a-f0-9]{64})"`).FindStringSubmatch(string(page))
	if len(nonceMatch) != 2 {
		t.Fatal("second consent nonce missing")
	}
	form = url.Values{"consent": {nonceMatch[1]}, "decision": {"allow"}, "device_id": {"first"}, "scope": {oauthControlScope}}
	req, _ = http.NewRequest("POST", srv.URL+"/oauth/authorize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "null")
	req.AddCookie(session)
	resp, err = noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 303 {
		t.Fatalf("control consent status=%d", resp.StatusCode)
	}
	redirect, _ = url.Parse(resp.Header.Get("Location"))
	exchange.Set("code", redirect.Query().Get("code"))
	status, body = postForm("/oauth/token", exchange)
	if status != 200 || body["scope"] != oauthControlScope {
		t.Fatalf("control exchange: %d %v", status, body)
	}
	controlMCP := managedMCPClient(t, srv, body["access_token"].(string))
	tools, err = controlMCP.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	hasTap := false
	for _, tool := range tools.Tools {
		if tool.Name == "tap" {
			hasTap = true
		}
	}
	if !hasTap {
		t.Fatal("control grant lacks tap")
	}
	// Removing a phone revokes every OAuth grant that includes it.
	status, _ = managedRequest(t, srv, "DELETE", "/tenant/devices/first", owner, nil)
	if status != 200 {
		t.Fatalf("device removal status=%d", status)
	}
	req, _ = http.NewRequest("POST", srv.URL+"/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+body["access_token"].(string))
	resp, err = noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("removed device grant status=%d", resp.StatusCode)
	}
}
