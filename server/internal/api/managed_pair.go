package api

import (
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

type managedPairing struct {
	tenantID string
	expires  time.Time
}

func (s *managedService) createPairing(w http.ResponseWriter, r *http.Request) {
	id, ok := s.owner(r)
	if !ok || !s.validMutationOrigin(r) {
		apiError(w, 401, "unauthorized", "login required")
		return
	}
	if s.publicURL == "" {
		apiError(w, 503, "pairing_unavailable", "public URL is not configured")
		return
	}
	ticket, _, err := randomToken()
	if err != nil {
		managedError(w, err)
		return
	}
	s.pairMu.Lock()
	for key, pair := range s.pairings {
		if time.Now().After(pair.expires) || pair.tenantID == id {
			delete(s.pairings, key)
		}
	}
	expires := time.Now().Add(5 * time.Minute)
	s.pairings[tokenHash(ticket)] = managedPairing{id, expires}
	s.pairMu.Unlock()
	// The secret lives in the URL fragment, so ordinary HTTP access logs never receive it.
	content := s.publicURL + "/pair#ticket=" + ticket
	png, err := qrcode.Encode(content, qrcode.Medium, 256)
	if err != nil {
		managedError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, 201, map[string]string{"qr_data_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
		"expires_at": expires.UTC().Format(time.RFC3339), "pair_url": content})
}

func (s *managedService) pairingFromRequest(r *http.Request) (managedPairing, string, bool) {
	var in struct {
		Ticket string `json:"ticket"`
	}
	if decodeBody(r, &in) != nil || len(in.Ticket) != 64 {
		return managedPairing{}, "", false
	}
	if _, err := hex.DecodeString(in.Ticket); err != nil {
		return managedPairing{}, "", false
	}
	key := tokenHash(in.Ticket)
	s.pairMu.Lock()
	defer s.pairMu.Unlock()
	pair, ok := s.pairings[key]
	if !ok || time.Now().After(pair.expires) {
		delete(s.pairings, key)
		return managedPairing{}, "", false
	}
	return pair, key, true
}

func (s *managedService) pairInfo(w http.ResponseWriter, r *http.Request) {
	pair, _, ok := s.pairingFromRequest(r)
	if !ok {
		apiError(w, 404, "pairing_expired", "pairing code expired")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, 200, map[string]string{"tenant_id": pair.tenantID, "server": s.publicURL,
		"expires_at": pair.expires.UTC().Format(time.RFC3339)})
}

func (s *managedService) redeemPairing(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Ticket string `json:"ticket"`
	}
	if decodeBody(r, &in) != nil || len(in.Ticket) != 64 {
		apiError(w, 400, "invalid_request", "ticket required")
		return
	}
	if _, err := hex.DecodeString(in.Ticket); err != nil {
		apiError(w, 400, "invalid_request", "ticket required")
		return
	}
	key := tokenHash(in.Ticket)
	s.pairMu.Lock()
	defer s.pairMu.Unlock()
	pair, ok := s.pairings[key]
	if !ok || time.Now().After(pair.expires) {
		delete(s.pairings, key)
		apiError(w, 404, "pairing_expired", "pairing code expired")
		return
	}
	seed, _, err := randomToken()
	if err != nil {
		managedError(w, err)
		return
	}
	id := "phone_" + seed[:16]
	token, hash, err := randomToken()
	if err != nil {
		managedError(w, err)
		return
	}
	err = s.change(func(state *managedState) error {
		tenant, exists := state.Tenants[pair.tenantID]
		if !exists {
			return managedFailure{404, "tenant_not_found", "account not found"}
		}
		if len(tenant.Devices) >= maxTenantDevices {
			return managedFailure{429, "device_limit", "device limit reached"}
		}
		if _, exists := tenant.Devices[id]; exists {
			return managedFailure{409, "device_exists", "try scanning again"}
		}
		tenant.Devices[id] = hex.EncodeToString(hash[:])
		state.Tenants[pair.tenantID] = tenant
		return nil
	})
	if err != nil {
		managedError(w, err)
		return
	}
	delete(s.pairings, key)
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, 201, map[string]string{"device_id": id, "device_token": token,
		"device_url": "wss://" + strings.TrimPrefix(s.publicURL, "https://") + "/device?id=" + id})
}
