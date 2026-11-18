package api

import (
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

const passwordMemory = 19 * 1024

type managedLoginAttempt struct {
	started time.Time
	count   int
}

func validManagedEmail(email string) bool {
	if email == "" || len(email) > 254 || strings.ToLower(email) != email || strings.ContainsAny(email, "\r\n\x00") {
		return false
	}
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email && address.Name == "" && strings.Contains(email, "@")
}

func normalizeManagedEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

func validPassword(password string) bool { return len(password) >= 12 && len(password) <= 128 }

func validPasswordHash(encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != "m=19456,t=2,p=1" {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != 16 {
		return false
	}
	digest, err := base64.RawStdEncoding.DecodeString(parts[5])
	return err == nil && len(digest) == 32
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	digest := argon2.IDKey([]byte(password), salt, 2, passwordMemory, 1, 32)
	return "$argon2id$v=19$m=19456,t=2,p=1$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(digest), nil
}

func matchesPassword(encoded, password string) bool {
	if !validPasswordHash(encoded) {
		return false
	}
	parts := strings.Split(encoded, "$")
	salt, _ := base64.RawStdEncoding.DecodeString(parts[4])
	expected, _ := base64.RawStdEncoding.DecodeString(parts[5])
	actual := argon2.IDKey([]byte(password), salt, 2, passwordMemory, 1, 32)
	return subtle.ConstantTimeCompare(expected, actual) == 1
}

func (s *managedService) emailEnabled() bool {
	return s.publicURL != "" && s.smtpHost != "" && s.smtpPort != "" && s.smtpUser != "" && s.smtpPassword != "" && s.smtpFrom != ""
}

// createOperatorEmailAccount lets the instance operator provision isolated review
// accounts without depending on a mail sender. Nginx must not expose /manage/.
func (s *managedService) createOperatorEmailAccount(w http.ResponseWriter, r *http.Request) {
	if !bearerMatches(r, s.op) {
		apiError(w, 401, "unauthorized", "operator token required")
		return
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		TenantID string `json:"tenant_id"`
	}
	if decodeBody(r, &in) != nil {
		apiError(w, 400, "invalid_request", "email and password required")
		return
	}
	email := normalizeManagedEmail(in.Email)
	if !validManagedEmail(email) || !validPassword(in.Password) || (in.TenantID != "" && !validDeviceID.MatchString(in.TenantID)) {
		apiError(w, 400, "invalid_request", "valid email, 12-128 character password and optional tenant_id required")
		return
	}
	encoded, err := hashPassword(in.Password)
	if err != nil {
		managedError(w, err)
		return
	}
	newTenantID := "em_" + requestID()[:16]
	_, ownerHash, err := randomToken()
	if err != nil {
		managedError(w, err)
		return
	}
	tenantID := in.TenantID
	err = s.change(func(state *managedState) error {
		if _, exists := state.EmailAccounts[email]; exists {
			return managedFailure{409, "email_exists", "email is already registered"}
		}
		if tenantID == "" {
			if len(state.Tenants) >= maxTenants {
				return managedFailure{429, "tenant_limit", "tenant limit reached"}
			}
			if _, exists := state.Tenants[newTenantID]; exists {
				return managedFailure{409, "tenant_exists", "account conflict"}
			}
			tenantID = newTenantID
			state.Tenants[tenantID] = managedTenant{OwnerHash: hex.EncodeToString(ownerHash[:]), Devices: make(map[string]string), Clients: make(map[string]managedClient)}
		} else if _, exists := state.Tenants[tenantID]; !exists {
			return managedFailure{404, "tenant_not_found", "tenant not found"}
		}
		if state.EmailAccounts == nil {
			state.EmailAccounts = make(map[string]managedEmailAccount)
		}
		state.EmailAccounts[email] = managedEmailAccount{TenantID: tenantID, PasswordHash: encoded}
		return nil
	})
	if err != nil {
		managedError(w, err)
		return
	}
	jsonResponse(w, 201, map[string]string{"email": email, "tenant_id": tenantID})
}

func (s *managedService) emailOrigin(r *http.Request) bool {
	return s.publicURL != "" && r.Header.Get("Origin") == s.publicURL
}

func (s *managedService) allowEmailAttempt(key string, limit int, window time.Duration) bool {
	s.authMu.Lock()
	defer s.authMu.Unlock()
	now := time.Now()
	if len(s.loginAttempts) > 10000 {
		for id, attempt := range s.loginAttempts {
			if now.Sub(attempt.started) > time.Hour {
				delete(s.loginAttempts, id)
			}
		}
		if len(s.loginAttempts) > 10000 {
			return false
		}
	}
	attempt := s.loginAttempts[key]
	if attempt.started.IsZero() || now.Sub(attempt.started) > window {
		attempt = managedLoginAttempt{started: now}
	}
	attempt.count++
	s.loginAttempts[key] = attempt
	return attempt.count <= limit
}

func (s *managedService) clearEmailAttempt(key string) {
	s.authMu.Lock()
	delete(s.loginAttempts, key)
	s.authMu.Unlock()
}

func (s *managedService) acquirePasswordSlot() bool {
	select {
	case s.passwordSlots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *managedService) releasePasswordSlot() { <-s.passwordSlots }

func emailClientIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	if ip == "127.0.0.1" || ip == "::1" {
		if forwarded := net.ParseIP(r.Header.Get("X-Real-IP")); forwarded != nil {
			return forwarded.String()
		}
	}
	return ip
}

func (s *managedService) emailLogin(w http.ResponseWriter, r *http.Request) {
	if !s.emailOrigin(r) {
		apiError(w, 403, "invalid_origin", "same-origin request required")
		return
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if decodeBody(r, &in) != nil {
		apiError(w, 400, "invalid_request", "email and password required")
		return
	}
	email := normalizeManagedEmail(in.Email)
	if !validManagedEmail(email) || len(in.Password) > 128 {
		apiError(w, 401, "invalid_credentials", "invalid email or password")
		return
	}
	if !s.allowEmailAttempt("login-email:"+email, 8, 15*time.Minute) ||
		!s.allowEmailAttempt("login-ip:"+emailClientIP(r), 80, 15*time.Minute) {
		apiError(w, 429, "login_throttled", "too many login attempts; try later")
		return
	}
	s.mu.RLock()
	account, exists := s.state.EmailAccounts[email]
	s.mu.RUnlock()
	if !s.acquirePasswordSlot() {
		apiError(w, 429, "login_busy", "login is busy; try again")
		return
	}
	defer s.releasePasswordSlot()
	if !exists || !matchesPassword(account.PasswordHash, in.Password) {
		apiError(w, 401, "invalid_credentials", "invalid email or password")
		return
	}
	s.clearEmailAttempt("login-email:" + email)
	if err := s.startSession(w, account.TenantID, "email:"+email); err != nil {
		managedError(w, err)
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

func (s *managedService) emailRegister(w http.ResponseWriter, r *http.Request) {
	s.emailStart(w, r, "")
}

func (s *managedService) emailLink(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.sessionOwner(r)
	if !ok {
		apiError(w, 401, "unauthorized", "login required")
		return
	}
	s.emailStart(w, r, tenantID)
}

func (s *managedService) emailStart(w http.ResponseWriter, r *http.Request, tenantID string) {
	if !s.emailOrigin(r) {
		apiError(w, 403, "invalid_origin", "same-origin request required")
		return
	}
	if !s.emailEnabled() {
		apiError(w, 503, "email_unavailable", "email registration is not configured")
		return
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if decodeBody(r, &in) != nil {
		apiError(w, 400, "invalid_request", "email and password required")
		return
	}
	email := normalizeManagedEmail(in.Email)
	if !validManagedEmail(email) || !validPassword(in.Password) {
		apiError(w, 400, "invalid_request", "valid email and 12-128 character password required")
		return
	}
	if !s.allowEmailAttempt("register-email:"+email, 3, time.Hour) ||
		!s.allowEmailAttempt("register-ip:"+emailClientIP(r), 30, time.Hour) {
		apiError(w, 429, "registration_throttled", "too many registration attempts; try later")
		return
	}
	s.mu.RLock()
	_, exists := s.state.EmailAccounts[email]
	s.mu.RUnlock()
	if exists {
		if tenantID != "" {
			apiError(w, 409, "email_exists", "email is already registered")
			return
		}
		jsonResponse(w, 202, map[string]bool{"ok": true})
		return
	}
	if !s.acquirePasswordSlot() {
		apiError(w, 429, "registration_busy", "registration is busy; try again")
		return
	}
	encoded, err := hashPassword(in.Password)
	s.releasePasswordSlot()
	if err != nil {
		managedError(w, err)
		return
	}
	token, _, err := randomToken()
	if err != nil {
		managedError(w, err)
		return
	}
	hash := tokenHash(token)
	err = s.change(func(state *managedState) error {
		if len(state.PendingEmails) >= 1000 {
			return managedFailure{429, "registration_busy", "registration is busy"}
		}
		if _, exists := state.EmailAccounts[email]; exists {
			return managedFailure{409, "email_exists", "email is already registered"}
		}
		if tenantID == "" && len(state.Tenants) >= maxTenants {
			return managedFailure{429, "tenant_limit", "registration is currently full"}
		}
		for key, pending := range state.PendingEmails {
			if pending.Email == email || time.Now().Unix() > pending.Expires {
				delete(state.PendingEmails, key)
			}
		}
		if state.PendingEmails == nil {
			state.PendingEmails = make(map[string]managedPendingEmail)
		}
		state.PendingEmails[hash] = managedPendingEmail{Email: email, PasswordHash: encoded, TenantID: tenantID, Expires: time.Now().Add(15 * time.Minute).Unix()}
		return nil
	})
	if err != nil {
		managedError(w, err)
		return
	}
	if err := s.sendVerification(email, token); err != nil {
		_ = s.change(func(state *managedState) error { delete(state.PendingEmails, hash); return nil })
		apiError(w, 503, "email_send_failed", "verification email could not be sent")
		return
	}
	jsonResponse(w, 202, map[string]bool{"ok": true})
}

func (s *managedService) emailVerify(w http.ResponseWriter, r *http.Request) {
	if !s.emailOrigin(r) {
		apiError(w, 403, "invalid_origin", "same-origin request required")
		return
	}
	var in struct {
		Token string `json:"token"`
	}
	if decodeBody(r, &in) != nil || len(in.Token) != 64 {
		apiError(w, 400, "invalid_verification", "verification link is invalid or expired")
		return
	}
	if _, err := hex.DecodeString(in.Token); err != nil {
		apiError(w, 400, "invalid_verification", "verification link is invalid or expired")
		return
	}
	newTenantID := "em_" + requestID()[:16]
	_, ownerHash, err := randomToken()
	if err != nil {
		managedError(w, err)
		return
	}
	var accountEmail, tenantID string
	err = s.change(func(state *managedState) error {
		pending, exists := state.PendingEmails[tokenHash(in.Token)]
		if !exists || time.Now().Unix() > pending.Expires {
			return managedFailure{400, "invalid_verification", "verification link is invalid or expired"}
		}
		if _, exists := state.EmailAccounts[pending.Email]; exists {
			return managedFailure{409, "email_exists", "email is already registered"}
		}
		tenantID = pending.TenantID
		if tenantID == "" {
			if len(state.Tenants) >= maxTenants {
				return managedFailure{429, "tenant_limit", "registration is currently full"}
			}
			if _, exists := state.Tenants[newTenantID]; exists {
				return managedFailure{409, "tenant_exists", "account conflict"}
			}
			tenantID = newTenantID
			state.Tenants[tenantID] = managedTenant{OwnerHash: hex.EncodeToString(ownerHash[:]), Devices: make(map[string]string), Clients: make(map[string]managedClient)}
		}
		if state.EmailAccounts == nil {
			state.EmailAccounts = make(map[string]managedEmailAccount)
		}
		state.EmailAccounts[pending.Email] = managedEmailAccount{TenantID: tenantID, PasswordHash: pending.PasswordHash}
		delete(state.PendingEmails, tokenHash(in.Token))
		accountEmail = pending.Email
		return nil
	})
	if err != nil {
		managedError(w, err)
		return
	}
	if err := s.startSession(w, tenantID, "email:"+accountEmail); err != nil {
		managedError(w, err)
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

func (s *managedService) sendVerification(email, token string) error {
	address := net.JoinHostPort(s.smtpHost, s.smtpPort)
	connection, err := net.DialTimeout("tcp", address, 10*time.Second)
	if err != nil {
		return err
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(20 * time.Second))
	client, err := smtp.NewClient(connection, s.smtpHost)
	if err != nil {
		return err
	}
	defer client.Close()
	if ok, _ := client.Extension("STARTTLS"); !ok {
		return errors.New("SMTP STARTTLS required")
	}
	if err := client.StartTLS(&tls.Config{ServerName: s.smtpHost, MinVersion: tls.VersionTLS12}); err != nil {
		return err
	}
	if err := client.Auth(smtp.PlainAuth("", s.smtpUser, s.smtpPassword, s.smtpHost)); err != nil {
		return err
	}
	if err := client.Mail(s.smtpFrom); err != nil {
		return err
	}
	if err := client.Rcpt(email); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	link := s.publicURL + "/#verify=" + token
	message := "From: " + s.smtpFrom + "\r\nTo: " + email + "\r\nSubject: JellyHub email verification\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nOpen this link within 15 minutes to verify your JellyHub account:\r\n" + link + "\r\n\r\nIf you did not request this, ignore this email.\r\n"
	if _, err := io.WriteString(writer, message); err != nil {
		writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}
