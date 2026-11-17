package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AsCat/jellyhere/server/internal/bridge"
	"github.com/gorilla/websocket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const testOperatorToken = "operator-token-long-enough-for-a-managed-test"

func managedRequest(t *testing.T, srv *httptest.Server, method, path, token string, body any) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, result
}

func mustManagedToken(t *testing.T, status int, body map[string]any, key string) string {
	t.Helper()
	if status != 201 {
		t.Fatalf("status=%d body=%v", status, body)
	}
	token, ok := body[key].(string)
	if !ok || len(token) != 64 {
		t.Fatalf("invalid %s in response", key)
	}
	return token
}

func connectManagedMock(t *testing.T, srv *httptest.Server, token, marker string) (*websocket.Conn, <-chan string, <-chan struct{}) {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/device?id=primary"
	conn, _, err := websocket.DefaultDialer.Dial(url, http.Header{"Authorization": []string{"Bearer " + token}})
	if err != nil {
		t.Fatal(err)
	}
	methods := make(chan string, 4)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			var command bridge.Command
			if conn.ReadJSON(&command) != nil {
				return
			}
			methods <- command.Method
			result := map[string]any{"done": true}
			if command.Method == "get_screen" {
				result = map[string]any{"revision": 1, "width": 100, "height": 200,
					"tree": map[string]any{"path": "0", "text": marker}}
			}
			_ = conn.WriteJSON(bridge.Response{ID: command.ID, OK: true, Result: mustJSON(result)})
		}
	}()
	return conn, methods, done
}

func managedMCPClient(t *testing.T, srv *httptest.Server, token string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "managed-test", Version: "0.1.0"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", DisableStandaloneSSE: true,
		HTTPClient: &http.Client{Transport: tokenTransport{token: token}}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestManagedTenantIsolationAndRevocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managed.json")
	handler, err := NewManagedHandler(ManagedConfig{StatePath: path, OperatorToken: testOperatorToken})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	status, body := managedRequest(t, srv, "POST", "/manage/tenants", testOperatorToken, map[string]any{"tenant_id": "alice"})
	aliceOwner := mustManagedToken(t, status, body, "owner_token")
	status, body = managedRequest(t, srv, "POST", "/manage/tenants", testOperatorToken, map[string]any{"tenant_id": "bob"})
	bobOwner := mustManagedToken(t, status, body, "owner_token")
	status, body = managedRequest(t, srv, "POST", "/tenant/devices", aliceOwner, map[string]any{"device_id": "primary"})
	aliceDevice := mustManagedToken(t, status, body, "device_token")
	status, body = managedRequest(t, srv, "POST", "/tenant/devices", bobOwner, map[string]any{"device_id": "primary"})
	bobDevice := mustManagedToken(t, status, body, "device_token")
	status, body = managedRequest(t, srv, "POST", "/tenant/clients", aliceOwner,
		map[string]any{"client_id": "reader", "scope": "viewer", "device_ids": []string{"primary"}})
	aliceClient := mustManagedToken(t, status, body, "mcp_token")
	status, body = managedRequest(t, srv, "POST", "/tenant/clients", bobOwner,
		map[string]any{"client_id": "writer", "scope": "controller", "device_ids": []string{"primary"}})
	bobClient := mustManagedToken(t, status, body, "mcp_token")

	aliceConn, aliceMethods, _ := connectManagedMock(t, srv, aliceDevice, "alice-screen")
	defer aliceConn.Close()
	bobConn, bobMethods, bobDone := connectManagedMock(t, srv, bobDevice, "bob-screen")
	defer bobConn.Close()
	aliceSession := managedMCPClient(t, srv, aliceClient)
	bobSession := managedMCPClient(t, srv, bobClient)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, test := range []struct {
		session *mcp.ClientSession
		want    string
		methods <-chan string
	}{
		{aliceSession, "alice-screen", aliceMethods},
		{bobSession, "bob-screen", bobMethods},
	} {
		var result *mcp.CallToolResult
		for attempt := 0; attempt < 20; attempt++ {
			result, err = test.session.CallTool(ctx, &mcp.CallToolParams{Name: "get_screen",
				Arguments: map[string]any{"deviceId": "primary", "includeImage": false}})
			if err == nil && !result.IsError {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if err != nil || result.IsError || !strings.Contains(result.Content[0].(*mcp.TextContent).Text, test.want) {
			t.Fatalf("wrong tenant screen: result=%+v err=%v", result, err)
		}
		if got := <-test.methods; got != "get_screen" {
			t.Fatalf("device method=%s", got)
		}
	}
	viewerTools, err := aliceSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range viewerTools.Tools {
		if tool.Name == "tap" {
			t.Fatal("viewer discovered a control tool")
		}
	}
	result, err := aliceSession.CallTool(ctx, &mcp.CallToolParams{Name: "tap",
		Arguments: map[string]any{"deviceId": "primary", "x": 1, "y": 2}})
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("viewer was allowed to tap: result=%+v err=%v", result, err)
	}
	select {
	case got := <-aliceMethods:
		t.Fatalf("viewer sent device command %q", got)
	default:
	}
	result, err = bobSession.CallTool(ctx, &mcp.CallToolParams{Name: "tap",
		Arguments: map[string]any{"deviceId": "primary", "x": 1, "y": 2}})
	if err != nil || result.IsError || <-bobMethods != "tap" {
		t.Fatalf("controller tap failed: result=%+v err=%v", result, err)
	}
	status, _ = managedRequest(t, srv, "DELETE", "/tenant/clients/reader", aliceOwner, nil)
	if status != 200 {
		t.Fatalf("client revoke status=%d", status)
	}
	status, _ = managedRequest(t, srv, "POST", "/mcp", aliceClient,
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	if status != 401 {
		t.Fatalf("revoked client status=%d", status)
	}
	status, body = managedRequest(t, srv, "DELETE", "/tenant/devices/primary", bobOwner, nil)
	if status != 200 || body["revoked_clients"] != float64(1) {
		t.Fatalf("device revoke response=%d %v", status, body)
	}
	select {
	case <-bobDone:
	case <-time.After(time.Second):
		t.Fatal("revoked device socket stayed open")
	}
	status, _ = managedRequest(t, srv, "POST", "/mcp", bobClient,
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	if status != 401 {
		t.Fatalf("revoked device client status=%d", status)
	}
}

func TestManagedStatePersistsOnlyHashes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managed.json")
	handler, err := NewManagedHandler(ManagedConfig{StatePath: path, OperatorToken: testOperatorToken})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	status, body := managedRequest(t, srv, "POST", "/manage/tenants", testOperatorToken, map[string]any{"tenant_id": "alice"})
	owner := mustManagedToken(t, status, body, "owner_token")
	status, body = managedRequest(t, srv, "POST", "/tenant/devices", owner, map[string]any{"device_id": "phone"})
	device := mustManagedToken(t, status, body, "device_token")
	status, body = managedRequest(t, srv, "POST", "/tenant/clients", owner,
		map[string]any{"client_id": "codex", "scope": "viewer", "device_ids": []string{"phone"}})
	client := mustManagedToken(t, status, body, "mcp_token")
	srv.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{owner, device, client} {
		if bytes.Contains(data, []byte(token)) {
			t.Fatal("plaintext credential found in state file")
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("state file mode=%v", info.Mode().Perm())
	}
	reloaded, err := NewManagedHandler(ManagedConfig{StatePath: path, OperatorToken: testOperatorToken})
	if err != nil {
		t.Fatal(err)
	}
	srv = httptest.NewServer(reloaded)
	defer srv.Close()
	status, body = managedRequest(t, srv, "GET", "/tenant", owner, nil)
	if status != 200 || body["tenant_id"] != "alice" {
		t.Fatalf("reloaded owner status=%d body=%v", status, body)
	}
	status, body = managedRequest(t, srv, "POST", "/tenant/owner-token/rotate", owner, nil)
	newOwner, ok := body["owner_token"].(string)
	if status != 200 || !ok || len(newOwner) != 64 {
		t.Fatalf("owner rotation status=%d body=%v", status, body)
	}
	status, _ = managedRequest(t, srv, "GET", "/tenant", owner, nil)
	if status != 401 {
		t.Fatalf("old owner token status=%d", status)
	}
}

func TestManagedCredentialRolesAndTenantScope(t *testing.T) {
	handler, err := NewManagedHandler(ManagedConfig{StatePath: filepath.Join(t.TempDir(), "state.json"),
		OperatorToken: testOperatorToken})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	status, body := managedRequest(t, srv, "POST", "/manage/tenants", testOperatorToken, map[string]any{"tenant_id": "alice"})
	alice := mustManagedToken(t, status, body, "owner_token")
	status, body = managedRequest(t, srv, "POST", "/manage/tenants", testOperatorToken, map[string]any{"tenant_id": "bob"})
	bob := mustManagedToken(t, status, body, "owner_token")
	status, body = managedRequest(t, srv, "POST", "/tenant/devices", alice, map[string]any{"device_id": "mine"})
	device := mustManagedToken(t, status, body, "device_token")
	status, _ = managedRequest(t, srv, "POST", "/tenant/devices", bob, map[string]any{"device_id": "private"})
	if status != 201 {
		t.Fatalf("bob device status=%d", status)
	}
	status, _ = managedRequest(t, srv, "POST", "/tenant/clients", alice,
		map[string]any{"client_id": "bad", "scope": "viewer", "device_ids": []string{"private"}})
	if status != 400 {
		t.Fatalf("cross-tenant client status=%d", status)
	}
	status, body = managedRequest(t, srv, "GET", "/tenant", alice, nil)
	if status != 200 || len(body["devices"].([]any)) != 1 || body["devices"].([]any)[0].(map[string]any)["id"] != "mine" {
		t.Fatalf("tenant listing leaked devices: %d %v", status, body)
	}
	for _, test := range []struct {
		method, path, token string
		body                any
	}{
		{"GET", "/tenant", testOperatorToken, nil},
		{"POST", "/manage/tenants", alice, map[string]any{"tenant_id": "charlie"}},
		{"POST", "/mcp", alice, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}},
		{"POST", "/mcp", device, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}},
	} {
		status, _ = managedRequest(t, srv, test.method, test.path, test.token, test.body)
		if status != 401 {
			t.Fatalf("credential crossed role on %s: %d", test.path, status)
		}
	}
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/device?id=private"
	conn, response, err := websocket.DefaultDialer.Dial(url, http.Header{"Authorization": []string{"Bearer " + device}})
	if conn != nil {
		conn.Close()
	}
	if err == nil || response == nil || response.StatusCode != 401 {
		t.Fatalf("device token crossed tenant: response=%v err=%v", response, err)
	}
	response.Body.Close()
}

func TestManagedPortalServesInviteUIWithoutCredentials(t *testing.T) {
	handler, err := NewManagedHandler(ManagedConfig{StatePath: filepath.Join(t.TempDir(), "state.json"),
		OperatorToken: testOperatorToken})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path, contentType, marker string
	}{
		{"/", "text/html", "id=\"login-form\""},
		{"/managed.js", "text/javascript", "sessionStorage"},
	} {
		record := httptest.NewRecorder()
		handler.ServeHTTP(record, httptest.NewRequest(http.MethodGet, test.path, nil))
		if record.Code != 200 || !strings.Contains(record.Header().Get("Content-Type"), test.contentType) ||
			!strings.Contains(record.Body.String(), test.marker) || record.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("portal %s: status=%d headers=%v", test.path, record.Code, record.Header())
		}
	}
}
