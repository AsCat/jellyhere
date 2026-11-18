package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManagedEmailVerificationAndLogin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	password := "long-test-password-123"
	hash, err := hashPassword(password)
	if err != nil || !validPasswordHash(hash) || !matchesPassword(hash, password) || matchesPassword(hash, "incorrect") {
		t.Fatal("password hashing failed")
	}
	token := strings.Repeat("a", 64)
	state := managedState{Version: managedStateVersion, Tenants: map[string]managedTenant{},
		PendingEmails: map[string]managedPendingEmail{tokenHash(token): {Email: "test@example.com", PasswordHash: hash,
			Expires: time.Now().Add(time.Minute).Unix()}}}
	if err := writeManagedState(path, state); err != nil {
		t.Fatal(err)
	}
	handler, err := NewManagedHandler(ManagedConfig{StatePath: path, OperatorToken: testOperatorToken, PublicURL: "https://jelly.test"})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	post := func(path string, body any, origin string) (*http.Response, map[string]any) {
		t.Helper()
		encoded, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", srv.URL+path, bytes.NewReader(encoded))
		req.Header.Set("Content-Type", "application/json")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return resp, out
	}
	if resp, _ := post("/auth/email/verify", map[string]string{"token": token}, ""); resp.StatusCode != 403 {
		t.Fatalf("missing origin status=%d", resp.StatusCode)
	}
	resp, out := post("/auth/email/verify", map[string]string{"token": token}, "https://jelly.test")
	if resp.StatusCode != 200 || out["ok"] != true || len(resp.Cookies()) == 0 {
		t.Fatalf("verification status=%d body=%v", resp.StatusCode, out)
	}
	if resp, _ := post("/auth/email/verify", map[string]string{"token": token}, "https://jelly.test"); resp.StatusCode != 400 {
		t.Fatalf("replayed verification status=%d", resp.StatusCode)
	}
	if resp, _ := post("/auth/email/login", map[string]string{"email": "test@example.com", "password": "wrong"}, "https://jelly.test"); resp.StatusCode != 401 {
		t.Fatalf("wrong password status=%d", resp.StatusCode)
	}
	resp, out = post("/auth/email/login", map[string]string{"email": "test@example.com", "password": password}, "https://jelly.test")
	if resp.StatusCode != 200 || out["ok"] != true {
		t.Fatalf("login status=%d body=%v", resp.StatusCode, out)
	}
	var session *http.Cookie
	for _, cookie := range resp.Cookies() {
		if cookie.Name == managedSessionCookie {
			session = cookie
		}
	}
	if session == nil || !session.HttpOnly || !session.Secure {
		t.Fatal("email login session cookie missing or insecure")
	}
	req, _ := http.NewRequest("GET", srv.URL+"/tenant", nil)
	req.AddCookie(session)
	result, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	result.Body.Close()
	if result.StatusCode != 200 {
		t.Fatalf("email session cannot access tenant: %d", result.StatusCode)
	}
}

func TestManagedClientScopeCanBeEditedWithoutRotatingToken(t *testing.T) {
	handler, err := NewManagedHandler(ManagedConfig{StatePath: filepath.Join(t.TempDir(), "state.json"), OperatorToken: testOperatorToken})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	status, body := managedRequest(t, srv, "POST", "/manage/tenants", testOperatorToken, map[string]any{"tenant_id": "owner"})
	owner := mustManagedToken(t, status, body, "owner_token")
	for _, id := range []string{"xiaomi", "samsung"} {
		status, body = managedRequest(t, srv, "POST", "/tenant/devices", owner, map[string]any{"device_id": id})
		mustManagedToken(t, status, body, "device_token")
	}
	status, body = managedRequest(t, srv, "POST", "/tenant/clients", owner, map[string]any{"client_id": "codex", "scope": "controller", "device_ids": []string{"xiaomi"}})
	token := mustManagedToken(t, status, body, "mcp_token")
	if status, _ := managedRequest(t, srv, "PATCH", "/tenant/clients/codex", owner, map[string]any{"device_ids": []string{"samsung", "samsung"}}); status != 400 {
		t.Fatalf("duplicate device status=%d", status)
	}
	status, body = managedRequest(t, srv, "PATCH", "/tenant/clients/codex", owner, map[string]any{"device_ids": []string{"xiaomi", "samsung"}})
	if status != 200 {
		t.Fatalf("edit status=%d body=%v", status, body)
	}
	status, body = managedRequest(t, srv, "GET", "/tenant", owner, nil)
	client := body["clients"].([]any)[0].(map[string]any)
	if status != 200 || len(client["device_ids"].([]any)) != 2 || client["scope"] != "controller" {
		t.Fatalf("updated client=%v", client)
	}
	clientSession := managedMCPClient(t, srv, token)
	if clientSession == nil {
		t.Fatal("editing device scope revoked existing token")
	}
}

func TestOperatorProvisionsEmailAccountWithoutSMTP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	handler, err := NewManagedHandler(ManagedConfig{StatePath: path, OperatorToken: testOperatorToken, PublicURL: "https://jelly.test"})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	in := map[string]any{"email": "reviewer@example.com", "password": "reviewer-test-password-123"}
	if status, _ := managedRequest(t, srv, "POST", "/manage/email-accounts", "", in); status != 401 {
		t.Fatalf("operator endpoint is public: %d", status)
	}
	status, body := managedRequest(t, srv, "POST", "/manage/email-accounts", testOperatorToken, in)
	if status != 201 || !strings.HasPrefix(body["tenant_id"].(string), "em_") {
		t.Fatalf("create status=%d body=%v", status, body)
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("reviewer-test-password-123")) {
		t.Fatal("plaintext password persisted")
	}
	loginBody, _ := json.Marshal(in)
	req, _ := http.NewRequest("POST", srv.URL+"/auth/email/login", bytes.NewReader(loginBody))
	req.Header.Set("Origin", "https://jelly.test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("login status=%d", resp.StatusCode)
	}
}
