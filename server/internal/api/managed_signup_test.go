package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestGitHubSignupAndOneTimeDevicePairing(t *testing.T) {
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			if r.Method != "POST" || r.FormValue("code") != "test-code" {
				t.Errorf("bad exchange: %s", r.URL.Path)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "test-access"})
		case "/user":
			if r.Header.Get("Authorization") != "Bearer test-access" {
				t.Errorf("bad profile authorization")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 12345, "login": "developer"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer github.Close()
	publicURL := "https://jelly.test"
	handler, err := NewManagedHandler(ManagedConfig{StatePath: filepath.Join(t.TempDir(), "state.json"),
		OperatorToken: testOperatorToken, PublicURL: publicURL, GitHubClientID: "client", GitHubClientSecret: "secret",
		GitHubOAuthURL: github.URL, GitHubAPIURL: github.URL})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}

	login, err := client.Get(srv.URL + "/auth/github")
	if err != nil {
		t.Fatal(err)
	}
	login.Body.Close()
	if login.StatusCode != 302 {
		t.Fatalf("login status=%d", login.StatusCode)
	}
	redirect, err := url.Parse(login.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state := redirect.Query().Get("state")
	if len(state) != 64 {
		t.Fatal("missing OAuth state")
	}
	var stateCookie *http.Cookie
	for _, cookie := range login.Cookies() {
		if cookie.Name == managedStateCookie {
			stateCookie = cookie
		}
	}
	if stateCookie == nil || !stateCookie.HttpOnly || !stateCookie.Secure {
		t.Fatal("insecure OAuth state cookie")
	}

	callback, _ := http.NewRequest("GET", srv.URL+"/auth/github/callback?state="+state+"&code=test-code", nil)
	callback.AddCookie(stateCookie)
	finished, err := client.Do(callback)
	if err != nil {
		t.Fatal(err)
	}
	finished.Body.Close()
	if finished.StatusCode != 302 {
		t.Fatalf("callback status=%d", finished.StatusCode)
	}
	var session *http.Cookie
	for _, cookie := range finished.Cookies() {
		if cookie.Name == managedSessionCookie {
			session = cookie
		}
	}
	if session == nil || !session.HttpOnly || !session.Secure {
		t.Fatal("insecure session cookie")
	}

	post := func(path string, payload any, cookie *http.Cookie, origin string) (int, map[string]any) {
		t.Helper()
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", srv.URL+path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, out
	}
	if code, _ := post("/tenant/pairings", map[string]any{}, session, ""); code != 401 {
		t.Fatalf("CSRF guard status=%d", code)
	}
	code, pairing := post("/tenant/pairings", map[string]any{}, session, publicURL)
	if code != 201 {
		t.Fatalf("pairing status=%d body=%v", code, pairing)
	}
	pairURL, _ := url.Parse(pairing["pair_url"].(string))
	if pairURL.RawQuery != "" || !strings.HasPrefix(pairing["qr_data_url"].(string), "data:image/png;base64,") {
		t.Fatal("pair ticket leaked into query or missing QR")
	}
	ticket := strings.TrimPrefix(pairURL.Fragment, "ticket=")
	code, inspected := post("/pair/inspect", map[string]string{"ticket": ticket}, nil, "")
	if code != 200 || inspected["tenant_id"] != "gh_12345" {
		t.Fatalf("inspect status=%d body=%v", code, inspected)
	}
	code, bound := post("/pair/redeem", map[string]string{"ticket": ticket}, nil, "")
	if code != 201 || !strings.HasPrefix(bound["device_url"].(string), "wss://jelly.test/device?id=") || len(bound["device_token"].(string)) != 64 {
		t.Fatalf("redeem status=%d body=%v", code, bound)
	}
	if code, _ := post("/pair/redeem", map[string]string{"ticket": ticket}, nil, ""); code != 404 {
		t.Fatalf("replayed ticket status=%d", code)
	}
	if code, _ := post("/pair/inspect", map[string]string{"ticket": ticket}, nil, ""); code != 404 {
		t.Fatalf("replayed inspect status=%d", code)
	}
	code, clientGrant := post("/tenant/clients", map[string]any{"client_name": "三星开发测试", "scope": "viewer",
		"device_ids": []string{bound["device_id"].(string)}}, session, publicURL)
	if code != 201 || clientGrant["client_name"] != "三星开发测试" ||
		!strings.HasPrefix(clientGrant["client_id"].(string), "client_") || len(clientGrant["mcp_token"].(string)) != 64 {
		t.Fatalf("named client status=%d body=%v", code, clientGrant)
	}
	deviceSocket := "ws" + strings.TrimPrefix(srv.URL, "http") + "/device?id=" + bound["device_id"].(string)
	conn, _, err := websocket.DefaultDialer.Dial(deviceSocket, http.Header{"Authorization": []string{"Bearer " + bound["device_token"].(string)}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	for attempt := 0; attempt < 20; attempt++ {
		req, _ := http.NewRequest("GET", srv.URL+"/tenant", nil)
		req.AddCookie(session)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var tenant map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&tenant); err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode == 200 && len(tenant["devices"].([]any)) == 1 && len(tenant["clients"].([]any)) == 1 &&
			tenant["devices"].([]any)[0].(map[string]any)["online"] == true {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("paired device did not become online in tenant dashboard")
}
