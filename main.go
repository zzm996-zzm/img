package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type paidRecord struct {
	Email      string    `json:"email"`
	EventType  string    `json:"event_type"`
	ResourceID string    `json:"resource_id,omitempty"`
	ReceivedAt time.Time `json:"received_at"`
}

var (
	paidMu     sync.RWMutex
	paidEmails = map[string]paidRecord{}
	appDB      *sql.DB
	authMu     sync.Mutex
	authHits   = map[string][]time.Time{}
	secretOnce sync.Once
	secretKey  string
)

const (
	sessionCookieName = "imgtools_session"
	passwordIter      = 120000
	maxJSONBody       = 32 << 10
	authWindow        = 10 * time.Minute
	authMaxHits       = 30
)

func main() {
	db, err := openDB()
	if err != nil {
		log.Fatalf("database init failed: %v", err)
	}
	appDB = db
	defer appDB.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/api/register", handleRegister)
	mux.HandleFunc("/api/login", handleLogin)
	mux.HandleFunc("/api/logout", handleLogout)
	mux.HandleFunc("/api/me", handleMe)
	mux.HandleFunc("/api/paypal-webhook", handlePayPalWebhook)
	mux.HandleFunc("/google6409d0c57bc30ecb.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("google-site-verification: google6409d0c57bc30ecb.html"))
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	for _, fallbackPort := range []string{"8080", "8081"} {
		if fallbackPort == port {
			continue
		}
		go listenAndServe(mux, fallbackPort, false)
	}

	listenAndServe(mux, port, true)
}

func listenAndServe(handler http.Handler, port string, fatal bool) {
	log.Printf("Server listening on 0.0.0.0:%s", port)
	err := http.ListenAndServe(":"+port, handler)
	if fatal {
		log.Fatal(err)
	}
	log.Printf("Could not listen on fallback port %s: %v", port, err)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(indexHTML))
}

func openDB() (*sql.DB, error) {
	path := os.Getenv("DB_PATH")
	if path == "" {
		path = "data/imgtools.db"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;`); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrateDB(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrateDB(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	email TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS licenses (
	email TEXT PRIMARY KEY,
	paid INTEGER NOT NULL DEFAULT 0,
	source TEXT NOT NULL DEFAULT '',
	event_type TEXT NOT NULL DEFAULT '',
	resource_id TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
`)
	return err
}

type authRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type sessionClaims struct {
	UserID int64  `json:"uid"`
	Email  string `json:"email"`
	Iat    int64  `json:"iat"`
	Exp    int64  `json:"exp"`
}

func handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method_not_allowed"})
		return
	}
	if !allowAuthRequest(r) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "error": "too_many_requests"})
		return
	}
	req, ok := readAuthRequest(w, r)
	if !ok {
		return
	}
	email := normalizeEmail(req.Email)
	if email == "" || len(email) > 254 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid_email"})
		return
	}
	if len(req.Password) < 8 || len(req.Password) > 128 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "weak_password"})
		return
	}

	hash, err := hashPassword(req.Password)
	if err != nil {
		log.Printf("password hash error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "server_error"})
		return
	}
	res, err := appDB.Exec(
		`INSERT INTO users (email, password_hash, created_at) VALUES (?, ?, ?)`,
		email, hash, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "constraint") {
			writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "email_exists"})
			return
		}
		log.Printf("register insert error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "server_error"})
		return
	}
	userID, _ := res.LastInsertId()
	setSessionCookie(w, r, userID, email)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "email": email, "paid": isPaidEmail(email)})
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method_not_allowed"})
		return
	}
	if !allowAuthRequest(r) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "error": "too_many_requests"})
		return
	}
	req, ok := readAuthRequest(w, r)
	if !ok {
		return
	}
	email := normalizeEmail(req.Email)
	if email == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "invalid_credentials"})
		return
	}

	var userID int64
	var storedHash string
	err := appDB.QueryRow(`SELECT id, password_hash FROM users WHERE email = ?`, email).Scan(&userID, &storedHash)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "invalid_credentials"})
		return
	}
	if err != nil {
		log.Printf("login query error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "server_error"})
		return
	}
	if !verifyPassword(req.Password, storedHash) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "invalid_credentials"})
		return
	}

	setSessionCookie(w, r, userID, email)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "email": email, "paid": isPaidEmail(email)})
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method_not_allowed"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method_not_allowed"})
		return
	}
	claims, ok := currentSession(r)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "authenticated": false, "paid": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"authenticated": true,
		"email":         claims.Email,
		"paid":          isPaidEmail(claims.Email),
	})
}

func readAuthRequest(w http.ResponseWriter, r *http.Request) (authRequest, bool) {
	defer r.Body.Close()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxJSONBody))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid_body"})
		return authRequest{}, false
	}
	var req authRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid_json"})
		return authRequest{}, false
	}
	return req, true
}

func allowAuthRequest(r *http.Request) bool {
	key := clientIP(r)
	now := time.Now()
	cutoff := now.Add(-authWindow)

	authMu.Lock()
	defer authMu.Unlock()

	hits := authHits[key]
	kept := hits[:0]
	for _, hit := range hits {
		if hit.After(cutoff) {
			kept = append(kept, hit)
		}
	}
	if len(kept) >= authMaxHits {
		authHits[key] = kept
		return false
	}
	authHits[key] = append(kept, now)
	return true
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		first := strings.TrimSpace(strings.Split(forwarded, ",")[0])
		if first != "" {
			return first
		}
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	host := r.RemoteAddr
	if idx := strings.LastIndex(host, ":"); idx > -1 {
		return host[:idx]
	}
	return host
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := pbkdf2SHA256([]byte(password), salt, passwordIter, 32)
	return fmt.Sprintf(
		"pbkdf2_sha256$%d$%s$%s",
		passwordIter,
		base64.RawURLEncoding.EncodeToString(salt),
		base64.RawURLEncoding.EncodeToString(hash),
	), nil
}

func verifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2_sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 10000 {
		return false
	}
	salt, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	expected, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	actual := pbkdf2SHA256([]byte(password), salt, iter, len(expected))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func pbkdf2SHA256(password, salt []byte, iter, keyLen int) []byte {
	hashLen := sha256.Size
	blocks := (keyLen + hashLen - 1) / hashLen
	out := make([]byte, 0, blocks*hashLen)
	for block := 1; block <= blocks; block++ {
		mac := hmac.New(sha256.New, password)
		mac.Write(salt)
		mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := make([]byte, len(u))
		copy(t, u)
		for i := 1; i < iter; i++ {
			mac = hmac.New(sha256.New, password)
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen]
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, userID int64, email string) {
	token, err := signSession(sessionClaims{
		UserID: userID,
		Email:  email,
		Iat:    time.Now().Unix(),
		Exp:    time.Now().Add(180 * 24 * time.Hour).Unix(),
	})
	if err != nil {
		log.Printf("session sign error: %v", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   180 * 24 * 60 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
	})
}

func currentSession(r *http.Request) (sessionClaims, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return sessionClaims{}, false
	}
	claims, err := verifySession(cookie.Value)
	if err != nil || claims.Exp < time.Now().Unix() || normalizeEmail(claims.Email) == "" {
		return sessionClaims{}, false
	}
	return claims, true
}

func signSession(claims sessionClaims) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signature := signValue(encodedPayload)
	return encodedPayload + "." + signature, nil
}

func verifySession(token string) (sessionClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return sessionClaims{}, errors.New("invalid token")
	}
	expected := signValue(parts[0])
	if subtle.ConstantTimeCompare([]byte(expected), []byte(parts[1])) != 1 {
		return sessionClaims{}, errors.New("invalid signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return sessionClaims{}, err
	}
	var claims sessionClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return sessionClaims{}, err
	}
	return claims, nil
}

func signValue(value string) string {
	mac := hmac.New(sha256.New, []byte(sessionSecret()))
	mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func sessionSecret() string {
	if secret := os.Getenv("SESSION_SECRET"); secret != "" {
		return secret
	}
	if secret := os.Getenv("JWT_SECRET"); secret != "" {
		return secret
	}
	secretOnce.Do(func() {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			log.Printf("session secret fallback random error: %v", err)
			secretKey = "temporary-dev-session-secret"
			return
		}
		secretKey = base64.RawURLEncoding.EncodeToString(buf)
		log.Print("SESSION_SECRET/JWT_SECRET not set; generated temporary session secret for this process")
	})
	return secretKey
}

func isSecureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func isPaidEmail(email string) bool {
	email = normalizeEmail(email)
	if email == "" {
		return false
	}
	if envPaidEmail(email) {
		return true
	}
	if appDB == nil {
		paidMu.RLock()
		_, ok := paidEmails[email]
		paidMu.RUnlock()
		return ok
	}
	var paid int
	err := appDB.QueryRow(`SELECT paid FROM licenses WHERE email = ?`, email).Scan(&paid)
	if err != nil {
		return false
	}
	return paid == 1
}

func envPaidEmail(email string) bool {
	for _, raw := range strings.Split(os.Getenv("PAID_EMAILS"), ",") {
		if normalizeEmail(raw) == email {
			return true
		}
	}
	return false
}

func setLicense(email, eventType, resourceID string, paid bool) {
	email = normalizeEmail(email)
	if email == "" || appDB == nil {
		return
	}
	paidValue := 0
	if paid {
		paidValue = 1
	}
	_, err := appDB.Exec(
		`INSERT INTO licenses (email, paid, source, event_type, resource_id, updated_at)
		 VALUES (?, ?, 'paypal', ?, ?, ?)
		 ON CONFLICT(email) DO UPDATE SET
		 paid = excluded.paid,
		 source = excluded.source,
		 event_type = excluded.event_type,
		 resource_id = excluded.resource_id,
		 updated_at = excluded.updated_at`,
		email,
		paidValue,
		eventType,
		resourceID,
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		log.Printf("license upsert error email=%q event=%q: %v", email, eventType, err)
	}
}

func handlePayPalWebhook(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"message": "paypal webhook endpoint ready",
			"path":    "/api/paypal-webhook",
		})
		return
	}

	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method_not_allowed"})
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		log.Printf("paypal webhook read error: %v", err)
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid_body"})
		return
	}
	defer r.Body.Close()

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		log.Printf("paypal webhook invalid json: %v body=%s", err, truncateForLog(string(body), 1200))
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid_json"})
		return
	}

	eventType, _ := payload["event_type"].(string)
	resourceID := findResourceID(payload)
	emails := extractEmails(payload)
	grantPaid := eventType == "PAYMENT.CAPTURE.COMPLETED"
	revokePaid := eventType == "PAYMENT.CAPTURE.REFUNDED" || eventType == "PAYMENT.CAPTURE.REVERSED" || eventType == "PAYMENT.CAPTURE.DENIED"

	for _, email := range emails {
		if grantPaid {
			paidMu.Lock()
			paidEmails[email] = paidRecord{
				Email:      email,
				EventType:  eventType,
				ResourceID: resourceID,
				ReceivedAt: time.Now().UTC(),
			}
			paidMu.Unlock()
			setLicense(email, eventType, resourceID, true)
		}
		if revokePaid {
			paidMu.Lock()
			delete(paidEmails, email)
			paidMu.Unlock()
			setLicense(email, eventType, resourceID, false)
		}
	}

	log.Printf(
		"paypal webhook received event=%q resource_id=%q emails=%v transmission_id=%q",
		eventType,
		resourceID,
		emails,
		r.Header.Get("Paypal-Transmission-Id"),
	)

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"event_type":  eventType,
		"resource_id": resourceID,
		"emails":      emails,
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("json response error: %v", err)
	}
}

func extractEmails(value any) []string {
	seen := map[string]bool{}
	var emails []string

	var walk func(any)
	walk = func(v any) {
		switch typed := v.(type) {
		case map[string]any:
			for key, item := range typed {
				if strings.Contains(strings.ToLower(key), "email") {
					if raw, ok := item.(string); ok {
						if email := normalizeEmail(raw); email != "" && !seen[email] {
							seen[email] = true
							emails = append(emails, email)
						}
					}
				}
				walk(item)
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		}
	}

	walk(value)
	sort.Strings(emails)
	return emails
}

func normalizeEmail(value string) string {
	email := strings.ToLower(strings.TrimSpace(value))
	if !strings.Contains(email, "@") || strings.ContainsAny(email, " \t\r\n") {
		return ""
	}
	return email
}

func findResourceID(payload map[string]any) string {
	if resource, ok := payload["resource"]; ok {
		return findStringByKey(resource, "id")
	}
	return findStringByKey(payload, "id")
}

func findStringByKey(value any, key string) string {
	switch typed := value.(type) {
	case map[string]any:
		if raw, ok := typed[key].(string); ok {
			return raw
		}
		for _, item := range typed {
			if found := findStringByKey(item, key); found != "" {
				return found
			}
		}
	case []any:
		for _, item := range typed {
			if found := findStringByKey(item, key); found != "" {
				return found
			}
		}
	}
	return ""
}

func truncateForLog(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "...(truncated)"
}

const indexHTML = `<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>免费在线工具集 | 图片压缩、格式转换、尺寸转换、二维码与数据工具</title>
<meta name="description" content="免费在线浏览器工具集，支持图片压缩、格式转换、尺寸转换、批量压缩、背景移除、二维码生成、社交媒体卡片、CSV转JSON和Markdown转PDF。核心处理在浏览器本地完成。">
<link href="https://fonts.googleapis.com/css2?family=Syne:wght@400;700;800&family=DM+Sans:wght@400;500&display=swap" rel="stylesheet">
<script async src="https://pagead2.googlesyndication.com/pagead/js/adsbygoogle.js?client=ca-pub-1902780696242483" crossorigin="anonymous"></script>
<script src="https://unpkg.com/qrcode-generator@1.4.4/qrcode.js"></script>
<style>
*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
:root{
  --bg:#0e0e11;--surface:#18181d;--surface2:#222228;
  --border:rgba(255,255,255,0.07);--accent:#d4ff57;
  --accent-dim:rgba(212,255,87,0.1);--text:#f0f0f0;
  --muted:#777;--success:#57ff9a;--danger:#ff6b6b;
}
body{font-family:'DM Sans',sans-serif;background:var(--bg);color:var(--text);min-height:100vh;overflow-x:hidden}
body::before{content:'';position:fixed;inset:0;background-image:linear-gradient(rgba(255,255,255,0.02) 1px,transparent 1px),linear-gradient(90deg,rgba(255,255,255,0.02) 1px,transparent 1px);background-size:48px 48px;pointer-events:none;z-index:0}
.wrap{position:relative;z-index:1;max-width:700px;margin:0 auto;padding:64px 24px 80px}
.badge{display:inline-flex;align-items:center;gap:6px;background:var(--accent-dim);border:1px solid rgba(212,255,87,0.25);color:var(--accent);font-size:11px;font-weight:500;letter-spacing:.1em;text-transform:uppercase;padding:5px 12px;border-radius:20px;margin-bottom:20px}
.badge-dot{width:6px;height:6px;border-radius:50%;background:var(--accent);animation:pulse 2s ease-in-out infinite}
@keyframes pulse{0%,100%{opacity:1;transform:scale(1)}50%{opacity:.4;transform:scale(.7)}}
h1{font-family:'Syne',sans-serif;font-size:clamp(34px,6vw,54px);font-weight:800;line-height:1.05;letter-spacing:-.02em;margin-bottom:14px}
h1 em{color:var(--accent);font-style:normal}
.desc{color:var(--muted);font-size:15px;line-height:1.65;max-width:460px;margin-bottom:48px}

/* 免费次数提示 */
.quota-bar{display:flex;align-items:center;justify-content:space-between;background:var(--surface2);border:1px solid var(--border);border-radius:12px;padding:12px 16px;margin-bottom:20px;gap:12px}
.quota-left{font-size:13px;color:var(--muted)}
.quota-left strong{color:var(--text);font-family:'Syne',sans-serif}
.quota-track{flex:1;height:4px;background:rgba(255,255,255,0.06);border-radius:2px;overflow:hidden}
.quota-fill{height:100%;background:var(--accent);border-radius:2px;transition:width .4s}
.quota-fill.low{background:var(--danger)}
.quota-unlock{background:transparent;border:1px solid rgba(212,255,87,0.35);color:var(--accent);border-radius:8px;padding:7px 12px;font-size:12px;font-weight:700;cursor:pointer;white-space:nowrap}
.quota-unlock:hover{background:var(--accent-dim)}

.card{background:var(--surface);border:1px solid var(--border);border-radius:20px;padding:32px;margin-bottom:20px}
.tool-tabs{display:grid;grid-template-columns:repeat(4,1fr);gap:8px;background:var(--surface2);border:1px solid var(--border);border-radius:12px;padding:6px;margin-bottom:24px}
.tool-tab{border:0;background:transparent;color:var(--muted);border-radius:8px;padding:11px 8px;font-size:13px;font-weight:700;cursor:pointer;transition:all .18s;white-space:nowrap}
.tool-tab:hover{color:var(--text);background:rgba(255,255,255,0.04)}
.tool-tab.on{background:var(--accent);color:#0e0e11}
.tool-tab .pro{display:inline-block;margin-left:5px;font-size:10px;color:inherit;opacity:.72}
.tool-box.hidden,.tool-controls.hidden{display:none}
@media(max-width:620px){.tool-tabs{grid-template-columns:repeat(2,1fr)}}
.drop-zone{border:1.5px dashed rgba(255,255,255,0.12);border-radius:14px;padding:52px 24px;text-align:center;cursor:pointer;transition:all .2s;background:var(--surface2);margin-bottom:24px}
.drop-zone:hover,.drop-zone.over{border-color:var(--accent);background:var(--accent-dim)}
.drop-zone.over{transform:scale(1.01)}
.drop-zone input{display:none}
.drop-icon{width:52px;height:52px;background:var(--surface);border:1px solid var(--border);border-radius:12px;display:flex;align-items:center;justify-content:center;margin:0 auto 14px;font-size:22px;transition:all .2s}
.drop-zone:hover .drop-icon{background:var(--accent-dim);border-color:rgba(212,255,87,0.3)}
.drop-title{font-family:'Syne',sans-serif;font-size:15px;font-weight:700;margin-bottom:5px}
.drop-title span{color:var(--accent)}
.drop-hint{font-size:12px;color:var(--muted)}
.preview{display:none;background:var(--surface2);border:1px solid var(--border);border-radius:12px;padding:14px 16px;margin-bottom:24px;align-items:center;gap:14px}
.preview.show{display:flex}
.prev-img{width:56px;height:56px;border-radius:8px;object-fit:cover;border:1px solid var(--border);flex-shrink:0}
.prev-info{flex:1;min-width:0}
.prev-name{font-size:13px;font-weight:500;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;margin-bottom:3px}
.prev-size{font-size:12px;color:var(--muted)}
.prev-change{font-size:12px;color:var(--muted);cursor:pointer;padding:6px 12px;border:1px solid var(--border);border-radius:8px;transition:all .15s;flex-shrink:0;white-space:nowrap}
.prev-change:hover{border-color:var(--accent);color:var(--accent)}
.field-label{font-size:11px;font-weight:500;color:var(--muted);text-transform:uppercase;letter-spacing:.08em;margin-bottom:10px}
.size-row{display:flex;align-items:center;gap:10px;margin-bottom:12px}
.size-input{flex:1;background:var(--surface2);border:1.5px solid var(--border);border-radius:10px;padding:12px 16px;font-size:26px;font-family:'Syne',sans-serif;font-weight:700;color:var(--text);outline:none;transition:border-color .2s;-moz-appearance:textfield}
.size-input::-webkit-outer-spin-button,.size-input::-webkit-inner-spin-button{-webkit-appearance:none}
.size-input:focus{border-color:var(--accent)}
.size-unit{font-family:'Syne',sans-serif;font-size:26px;font-weight:700;color:var(--muted)}
.presets{display:flex;gap:8px;flex-wrap:wrap;margin-bottom:28px}
.preset{padding:6px 14px;background:var(--surface2);border:1px solid var(--border);border-radius:8px;color:var(--muted);font-size:13px;font-weight:500;cursor:pointer;transition:all .15s}
.preset:hover,.preset.on{border-color:var(--accent);color:var(--accent);background:var(--accent-dim)}
.format-grid{display:grid;grid-template-columns:repeat(3,1fr);gap:8px;margin-bottom:28px}
.format-option{padding:13px 10px;background:var(--surface2);border:1px solid var(--border);border-radius:10px;color:var(--muted);font-size:13px;font-weight:700;cursor:pointer;transition:all .15s}
.format-option:hover,.format-option.on{border-color:var(--accent);color:var(--accent);background:var(--accent-dim)}
.dimension-grid{display:grid;grid-template-columns:1fr 1fr;gap:10px;margin-bottom:12px}
.dim-input{width:100%;background:var(--surface2);border:1.5px solid var(--border);border-radius:10px;padding:12px 14px;font-size:20px;font-family:'Syne',sans-serif;font-weight:700;color:var(--text);outline:none;-moz-appearance:textfield}
.dim-input::-webkit-outer-spin-button,.dim-input::-webkit-inner-spin-button{-webkit-appearance:none}
.dim-input:focus{border-color:var(--accent)}
.resize-presets{display:flex;gap:8px;flex-wrap:wrap;margin-bottom:16px}
.resize-preset{padding:6px 12px;background:var(--surface2);border:1px solid var(--border);border-radius:8px;color:var(--muted);font-size:12px;font-weight:700;cursor:pointer;transition:all .15s}
.resize-preset:hover,.resize-preset.on{border-color:var(--accent);color:var(--accent);background:var(--accent-dim)}
.mode-grid{display:grid;grid-template-columns:repeat(3,1fr);gap:8px;margin-bottom:28px}
.pro-panel{display:none;background:var(--surface2);border:1px solid rgba(212,255,87,0.24);border-radius:14px;padding:28px;text-align:left}
.pro-panel.show{display:block}
.pro-kicker{display:inline-flex;align-items:center;background:var(--accent-dim);border:1px solid rgba(212,255,87,0.25);color:var(--accent);border-radius:999px;padding:5px 10px;font-size:11px;font-weight:800;margin-bottom:16px}
.pro-title{font-family:'Syne',sans-serif;font-size:24px;font-weight:800;margin-bottom:10px}
.pro-desc{font-size:14px;color:var(--muted);line-height:1.65;margin-bottom:18px}
.pro-list{display:grid;gap:8px;margin-bottom:22px}
.pro-item{font-size:13px;color:var(--text);display:flex;gap:10px;align-items:center}
.pro-item::before{content:'✓';color:var(--accent);font-weight:800}
.btn{width:100%;padding:16px;background:var(--accent);color:#0e0e11;border:none;border-radius:12px;font-size:16px;font-family:'Syne',sans-serif;font-weight:700;cursor:pointer;transition:all .2s}
.btn:hover:not(:disabled){background:#c8f53f;transform:translateY(-1px)}
.btn:active:not(:disabled){transform:translateY(0)}
.btn:disabled{background:var(--surface2);color:var(--muted);cursor:not-allowed;border:1px solid var(--border)}
.btn.pay{background:linear-gradient(135deg,#f59e0b,#ef4444);color:#fff}
.btn.pay:hover:not(:disabled){background:linear-gradient(135deg,#d97706,#dc2626);transform:translateY(-1px)}
.status{display:none;margin-top:14px;padding:13px 16px;border-radius:10px;font-size:13px;font-weight:500;align-items:center;gap:10px}
.status.show{display:flex}
.status.loading{background:rgba(255,255,255,0.03);border:1px solid var(--border);color:var(--muted)}
.status.ok{background:rgba(87,255,154,0.07);border:1px solid rgba(87,255,154,0.25);color:var(--success)}
.status.err{background:rgba(255,107,107,0.07);border:1px solid rgba(255,107,107,0.25);color:var(--danger)}
.sdot{width:7px;height:7px;border-radius:50%;flex-shrink:0}
.loading .sdot{background:var(--muted);animation:pulse 1s ease-in-out infinite}
.ok .sdot{background:var(--success)}
.err .sdot{background:var(--danger)}
@keyframes spin{to{transform:rotate(360deg)}}
.spinner{width:14px;height:14px;border:2px solid rgba(255,255,255,0.08);border-top-color:var(--muted);border-radius:50%;animation:spin .7s linear infinite;flex-shrink:0}

/* paywall modal */
.overlay{display:none;position:fixed;inset:0;background:rgba(0,0,0,0.8);z-index:100;align-items:center;justify-content:center;padding:24px}
.overlay.show{display:flex}
.modal{background:var(--surface);border:1px solid var(--border);border-radius:20px;padding:40px 32px;max-width:420px;width:100%;text-align:center}
.modal-icon{font-size:48px;margin-bottom:16px}
.modal-title{font-family:'Syne',sans-serif;font-size:24px;font-weight:800;margin-bottom:8px}
.modal-desc{color:var(--muted);font-size:14px;line-height:1.6;margin-bottom:28px}
.modal-price{font-family:'Syne',sans-serif;font-size:42px;font-weight:800;color:var(--accent);margin-bottom:4px}
.modal-price-desc{font-size:13px;color:var(--muted);margin-bottom:28px}
.modal-features{text-align:left;background:var(--surface2);border-radius:12px;padding:16px 20px;margin-bottom:24px}
.modal-feature{font-size:13px;color:var(--text);padding:5px 0;display:flex;align-items:center;gap:10px}
.modal-feature::before{content:'✓';color:var(--accent);font-weight:700;flex-shrink:0}
.modal-close{margin-top:16px;font-size:13px;color:var(--muted);cursor:pointer;text-decoration:underline}
.modal-close:hover{color:var(--text)}

.features{display:grid;grid-template-columns:repeat(4,1fr);gap:12px}
@media(max-width:650px){.features{grid-template-columns:repeat(2,1fr)}}
@media(max-width:500px){.features{grid-template-columns:1fr}}
.feat{background:var(--surface);border:1px solid var(--border);border-radius:14px;padding:20px}
.feat-icon{font-size:20px;margin-bottom:10px}
.feat-title{font-family:'Syne',sans-serif;font-size:13px;font-weight:700;margin-bottom:4px}
.feat-desc{font-size:12px;color:var(--muted);line-height:1.55}
.section{margin-top:52px}
.section-head{display:flex;align-items:flex-end;justify-content:space-between;gap:18px;margin-bottom:18px}
.section-title{font-family:'Syne',sans-serif;font-size:24px;font-weight:800;letter-spacing:-.01em}
.section-desc{font-size:13px;color:var(--muted);line-height:1.6;max-width:360px}
.tool-matrix{display:grid;grid-template-columns:repeat(3,1fr);gap:12px}
@media(max-width:720px){.tool-matrix{grid-template-columns:1fr}.section-head{display:block}.section-desc{margin-top:8px}}
.tool-card{background:var(--surface);border:1px solid var(--border);border-radius:14px;padding:20px;min-height:156px;display:flex;flex-direction:column;gap:10px}
.tool-card.pro{border-color:rgba(212,255,87,0.28)}
.tool-card.coming{opacity:.78}
.tool-top{display:flex;align-items:center;justify-content:space-between;gap:10px}
.tool-icon{font-size:22px}
.tool-tag{font-size:10px;font-weight:800;letter-spacing:.06em;color:var(--accent);background:var(--accent-dim);border:1px solid rgba(212,255,87,0.2);border-radius:999px;padding:4px 8px;white-space:nowrap}
.tool-name{font-family:'Syne',sans-serif;font-size:16px;font-weight:800}
.tool-copy{font-size:12px;color:var(--muted);line-height:1.55;flex:1}
.tool-action{align-self:flex-start;border:1px solid var(--border);background:transparent;color:var(--text);border-radius:8px;padding:8px 11px;font-size:12px;font-weight:800;cursor:pointer;transition:all .15s}
.tool-action:hover{border-color:var(--accent);color:var(--accent);background:var(--accent-dim)}
.pro-strip{background:linear-gradient(135deg,rgba(212,255,87,0.13),rgba(255,255,255,0.03));border:1px solid rgba(212,255,87,0.25);border-radius:18px;padding:28px;margin-top:52px}
.pro-strip-title{font-family:'Syne',sans-serif;font-size:28px;font-weight:800;margin-bottom:8px}
.pro-strip-copy{color:var(--muted);font-size:14px;line-height:1.65;margin-bottom:18px;max-width:560px}
.pro-points{display:grid;grid-template-columns:repeat(3,1fr);gap:10px;margin-bottom:20px}
@media(max-width:620px){.pro-points{grid-template-columns:1fr}}
.pro-point{background:rgba(255,255,255,0.04);border:1px solid var(--border);border-radius:10px;padding:11px 12px;font-size:12px;color:var(--text);font-weight:700}
.utility-lab{background:var(--surface);border:1px solid var(--border);border-radius:20px;padding:28px;margin-top:52px}
.utility-tabs{display:flex;gap:8px;flex-wrap:wrap;margin-bottom:20px}
.utility-tab{background:var(--surface2);border:1px solid var(--border);color:var(--muted);border-radius:8px;padding:9px 12px;font-size:12px;font-weight:800;cursor:pointer;transition:all .15s}
.utility-tab:hover,.utility-tab.on{border-color:var(--accent);color:var(--accent);background:var(--accent-dim)}
.utility-panel{display:none}
.utility-panel.show{display:block}
.utility-grid{display:grid;grid-template-columns:1fr 1fr;gap:18px;align-items:start}
@media(max-width:720px){.utility-grid{grid-template-columns:1fr}}
.utility-field{display:grid;gap:8px;margin-bottom:12px}
.utility-label{font-size:11px;color:var(--muted);font-weight:800;letter-spacing:.08em;text-transform:uppercase}
.utility-input,.utility-textarea,.utility-select{width:100%;background:var(--surface2);border:1px solid var(--border);border-radius:10px;color:var(--text);padding:12px 13px;font:inherit;outline:none}
.utility-input:focus,.utility-textarea:focus,.utility-select:focus{border-color:var(--accent)}
.utility-textarea{min-height:150px;resize:vertical;line-height:1.55}
.utility-output{background:var(--surface2);border:1px solid var(--border);border-radius:12px;min-height:180px;padding:16px;overflow:auto}
.qr-box{display:flex;align-items:center;justify-content:center;min-height:240px}
.gradient-preview{height:190px;border-radius:12px;border:1px solid var(--border);margin-bottom:12px}
.swatch-row{display:flex;gap:8px;flex-wrap:wrap}
.swatch{height:34px;min-width:86px;border-radius:8px;border:1px solid var(--border)}
.social-canvas{width:100%;max-width:420px;border:1px solid var(--border);border-radius:12px;background:#111}
.json-output{white-space:pre-wrap;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px;line-height:1.55}
.markdown-preview h1,.markdown-preview h2,.markdown-preview h3{font-family:'Syne',sans-serif;margin:0 0 10px}
.markdown-preview p,.markdown-preview li{color:var(--text);line-height:1.65;margin-bottom:8px}
.markdown-preview code{background:rgba(255,255,255,0.08);border-radius:5px;padding:2px 5px}
.faq{margin-top:48px}
.faq-title{font-family:'Syne',sans-serif;font-size:22px;font-weight:800;margin-bottom:24px;letter-spacing:-.01em}
.faq-item{border-bottom:1px solid var(--border);padding:20px 0;cursor:pointer}
.faq-item:last-child{border-bottom:none}
.faq-q{font-family:'Syne',sans-serif;font-size:15px;font-weight:700;display:flex;justify-content:space-between;align-items:center;gap:16px}
.faq-arrow{color:var(--muted);font-size:18px;flex-shrink:0;transition:transform .2s}
.faq-item.open .faq-arrow{transform:rotate(45deg)}
.faq-a{font-size:14px;color:var(--muted);line-height:1.75;max-height:0;overflow:hidden;transition:max-height .3s ease,padding .3s}
.faq-item.open .faq-a{max-height:300px;padding-top:12px}
@keyframes up{from{opacity:0;transform:translateY(18px)}to{opacity:1;transform:translateY(0)}}
.header{animation:up .5s ease both}
.card{animation:up .5s ease .08s both}
.features{animation:up .5s ease .16s both}
.account-bar{display:flex;justify-content:flex-end;margin-bottom:20px}
.account-pill{display:inline-flex;align-items:center;gap:8px;background:var(--surface);border:1px solid var(--border);border-radius:999px;color:var(--text);padding:8px 12px;font-size:12px;font-weight:800;cursor:pointer;max-width:100%;transition:all .15s}
.account-pill:hover{border-color:rgba(212,255,87,0.35);color:var(--accent);background:var(--accent-dim)}
.account-pill.pro{border-color:rgba(212,255,87,0.35);color:var(--accent)}
.account-email{max-width:220px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.auth-switch{display:grid;grid-template-columns:1fr 1fr;gap:8px;background:var(--surface2);border:1px solid var(--border);border-radius:12px;padding:6px;margin-bottom:18px}
.auth-switch button{border:0;background:transparent;color:var(--muted);border-radius:8px;padding:10px;font-size:13px;font-weight:800;cursor:pointer}
.auth-switch button.on{background:var(--accent);color:#0e0e11}
.auth-form{display:grid;gap:10px;text-align:left}
.auth-input{width:100%;background:var(--surface2);border:1px solid var(--border);border-radius:10px;color:var(--text);padding:13px 14px;font:inherit;outline:none}
.auth-input:focus{border-color:var(--accent)}
.auth-hint{font-size:12px;color:var(--muted);line-height:1.55;text-align:left;margin-top:12px}
.auth-msg{min-height:18px;font-size:12px;color:var(--muted);text-align:left}
.auth-msg.err{color:var(--danger)}
.auth-msg.ok{color:var(--success)}
</style>
</head>
<body>
<div class="wrap">
  <div class="header">
    <div class="account-bar">
      <button class="account-pill" id="accountPill" onclick="showAuthModal()">登录 / 注册</button>
    </div>
    <div class="badge"><span class="badge-dot"></span>Browser Tool Suite</div>
    <h1>免费在线工具集<br><em>图片、创作与数据处理</em></h1>
    <p class="desc">压缩图片、转换格式、调整尺寸，并逐步加入二维码、社交媒体卡片、CSV转JSON和Markdown转PDF。核心处理在浏览器本地完成，尽量不占服务器算力。</p>
  </div>

  <!-- 免费次数进度条 -->
  <div class="quota-bar" id="quotaBar">
    <div class="quota-left">免费次数：<strong id="quotaText">100 / 100</strong></div>
    <div class="quota-track"><div class="quota-fill" id="quotaFill" style="width:100%"></div></div>
    <button class="quota-unlock" onclick="showPaywall()">PayPal 解锁</button>
  </div>

  <div class="card">
    <div class="tool-tabs">
      <button class="tool-tab on" id="tab-compress" onclick="switchTool('compress')">单张压缩</button>
      <button class="tool-tab" id="tab-convert" onclick="switchTool('convert')">格式转换</button>
      <button class="tool-tab" id="tab-resize" onclick="switchTool('resize')">尺寸转换</button>
      <button class="tool-tab" id="tab-batch" onclick="switchTool('batch')">批量压缩 <span class="pro">PRO</span></button>
    </div>

    <div class="tool-box" id="singleTool">
      <div class="drop-zone" id="dz">
        <input type="file" id="fi" accept="image/jpeg,image/png,image/webp">
        <div class="drop-icon">🖼</div>
        <div class="drop-title">拖拽图片到这里，或<span>点击选择</span></div>
        <div class="drop-hint" id="dropHint">JPG · PNG · WebP &nbsp;·&nbsp; 本地处理，不上传服务器</div>
      </div>

      <div class="preview" id="prev">
        <img class="prev-img" id="pimg" src="" alt="">
        <div class="prev-info">
          <div class="prev-name" id="pname"></div>
          <div class="prev-size" id="psize"></div>
        </div>
        <div class="prev-change" onclick="document.getElementById('fi').click()">换一张</div>
      </div>

      <div class="tool-controls" id="compressControls">
        <div class="field-label">目标文件大小</div>
        <div class="size-row">
          <input class="size-input" type="number" id="tgt" value="200" min="10" max="20000">
          <span class="size-unit">KB</span>
        </div>
        <div class="presets">
          <button class="preset" onclick="setTarget(100)">100 KB</button>
          <button class="preset on" onclick="setTarget(200)">200 KB</button>
          <button class="preset" onclick="setTarget(500)">500 KB</button>
          <button class="preset" onclick="setTarget(1024)">1 MB</button>
          <button class="preset" onclick="setTarget(2048)">2 MB</button>
        </div>
      </div>

      <div class="tool-controls hidden" id="convertControls">
        <div class="field-label">输出格式</div>
        <div class="format-grid">
          <button class="format-option on" onclick="setFormat('image/jpeg', 'JPG')">JPG</button>
          <button class="format-option" onclick="setFormat('image/png', 'PNG')">PNG</button>
          <button class="format-option" onclick="setFormat('image/webp', 'WebP')">WebP</button>
        </div>
      </div>

      <div class="tool-controls hidden" id="resizeControls">
        <div class="field-label">输出尺寸</div>
        <div class="dimension-grid">
          <input class="dim-input" type="number" id="resizeW" value="200" min="1" max="12000" aria-label="宽度">
          <input class="dim-input" type="number" id="resizeH" value="200" min="1" max="12000" aria-label="高度">
        </div>
        <div class="resize-presets">
          <button class="resize-preset on" onclick="setResizePreset(200, 200)">200x200</button>
          <button class="resize-preset" onclick="setResizePreset(300, 300)">300x300</button>
          <button class="resize-preset" onclick="setResizePreset(512, 512)">512x512</button>
          <button class="resize-preset" onclick="setResizePreset(1080, 1080)">1080x1080</button>
          <button class="resize-preset" onclick="setResizePreset(1920, 1080)">1920x1080</button>
        </div>
        <div class="field-label">适配方式</div>
        <div class="mode-grid">
          <button class="format-option on" onclick="setResizeMode('contain', this)">留白适配</button>
          <button class="format-option" onclick="setResizeMode('cover', this)">裁剪填满</button>
          <button class="format-option" onclick="setResizeMode('stretch', this)">拉伸</button>
        </div>
      </div>

      <button class="btn" id="btn" onclick="runTool()" disabled>选择图片后开始</button>
      <div class="status" id="st"></div>
    </div>

    <div class="pro-panel" id="batchPanel">
      <div class="pro-kicker">PRO TOOL</div>
      <div class="pro-title">批量图片压缩</div>
      <div class="pro-desc">一次选择多张图片，统一压缩到指定大小，适合电商图、证件资料、社媒素材和表单上传前的批处理。</div>
      <div class="pro-list">
        <div class="pro-item">批量处理 JPG / PNG / WebP</div>
        <div class="pro-item">统一目标大小，减少重复操作</div>
        <div class="pro-item">浏览器本地处理，图片不上传服务器</div>
      </div>
      <button class="btn pay" onclick="showPaywall()">使用 PayPal 解锁批量压缩 →</button>
    </div>
  </div>

  <div class="features">
    <div class="feat"><div class="feat-icon">⚡</div><div class="feat-title">浏览器本地处理</div><div class="feat-desc">图片不上传服务器，速度快，完全私密</div></div>
    <div class="feat"><div class="feat-icon">🎯</div><div class="feat-title">精确压缩</div><div class="feat-desc">二分法算法，紧贴目标大小，不会超限</div></div>
    <div class="feat"><div class="feat-icon">📐</div><div class="feat-title">尺寸转换</div><div class="feat-desc">快速生成200x200、头像和平台上传尺寸</div></div>
    <div class="feat"><div class="feat-icon">🆓</div><div class="feat-title">免费格式转换</div><div class="feat-desc">JPG、PNG、WebP互转，免费使用</div></div>
  </div>

  <section class="section">
    <div class="section-head">
      <div class="section-title">Image Tools</div>
      <p class="section-desc">围绕上传限制、头像尺寸、电商素材和批量处理设计，免费工具引流，Pro 工具节省重复操作。</p>
    </div>
    <div class="tool-matrix">
      <div class="tool-card">
        <div class="tool-top"><span class="tool-icon">🎯</span><span class="tool-tag">FREE</span></div>
        <div class="tool-name">图片压缩到指定KB</div>
        <div class="tool-copy">把 JPG、PNG、WebP 压缩到 200KB、500KB、1MB 等目标大小，适合表单、招聘网站和证件材料上传。</div>
        <button class="tool-action" onclick="jumpTool('compress')">开始压缩</button>
      </div>
      <div class="tool-card">
        <div class="tool-top"><span class="tool-icon">🔁</span><span class="tool-tag">FREE</span></div>
        <div class="tool-name">图片格式转换</div>
        <div class="tool-copy">免费将图片转换为 JPG、PNG 或 WebP，全部在浏览器中完成，不需要上传服务器。</div>
        <button class="tool-action" onclick="jumpTool('convert')">转换格式</button>
      </div>
      <div class="tool-card">
        <div class="tool-top"><span class="tool-icon">📐</span><span class="tool-tag">FREE</span></div>
        <div class="tool-name">图片尺寸转换</div>
        <div class="tool-copy">自定义宽高，支持留白适配、裁剪填满和拉伸，快速生成头像、社媒和平台上传尺寸。</div>
        <button class="tool-action" onclick="jumpTool('resize')">调整尺寸</button>
      </div>
      <div class="tool-card pro">
        <div class="tool-top"><span class="tool-icon">📦</span><span class="tool-tag">PRO</span></div>
        <div class="tool-name">批量图片压缩</div>
        <div class="tool-copy">一次处理多张图片，统一压缩设置，适合电商图、资料图和社媒素材批处理。</div>
        <button class="tool-action" onclick="jumpTool('batch')">解锁批量</button>
      </div>
      <div class="tool-card pro coming">
        <div class="tool-top"><span class="tool-icon">✂️</span><span class="tool-tag">PRO SOON</span></div>
        <div class="tool-name">背景移除</div>
        <div class="tool-copy">面向头像、电商图和社媒配图，计划支持本地模型移除背景，Pro 输出高清无水印。</div>
        <button class="tool-action" onclick="showPaywall()">查看 Pro</button>
      </div>
      <div class="tool-card pro coming">
        <div class="tool-top"><span class="tool-icon">🧰</span><span class="tool-tag">PRO</span></div>
        <div class="tool-name">批量尺寸与格式处理</div>
        <div class="tool-copy">把批量压缩、尺寸转换和格式转换组合成一个流水线，后续作为 Pro 核心能力。</div>
        <button class="tool-action" onclick="showPaywall()">解锁 Pro</button>
      </div>
    </div>
  </section>

  <section class="section">
    <div class="section-head">
      <div class="section-title">Creator Tools</div>
      <p class="section-desc">为运营、博主和小商家准备的轻量创作工具，基础功能免费，高级样式和高清导出进入 Pro。</p>
    </div>
    <div class="tool-matrix">
      <div class="tool-card">
        <div class="tool-top"><span class="tool-icon">▦</span><span class="tool-tag">FREE</span></div>
        <div class="tool-name">二维码生成器</div>
        <div class="tool-copy">生成基础二维码，可自定义前景色和背景色，适合链接、活动页和社媒运营。</div>
        <button class="tool-action" onclick="openUtility('qr')">生成二维码</button>
      </div>
      <div class="tool-card">
        <div class="tool-top"><span class="tool-icon">🖼</span><span class="tool-tag">FREE</span></div>
        <div class="tool-name">社交媒体卡片制作</div>
        <div class="tool-copy">输入标题和副标题，本地生成一张适合社媒分享的卡片图，后续 Pro 解锁更多模板。</div>
        <button class="tool-action" onclick="openUtility('social')">制作卡片</button>
      </div>
      <div class="tool-card">
        <div class="tool-top"><span class="tool-icon">🎨</span><span class="tool-tag">FREE</span></div>
        <div class="tool-name">配色与渐变生成器</div>
        <div class="tool-copy">随机生成配色和 CSS 渐变，快速复制到设计稿、落地页或社媒素材里。</div>
        <button class="tool-action" onclick="openUtility('gradient')">生成配色</button>
      </div>
    </div>
  </section>

  <section class="section">
    <div class="section-head">
      <div class="section-title">Data Tools</div>
      <p class="section-desc">面向开发者、运营和数据整理场景，主打浏览器本地解析，减少上传敏感文件的顾虑。</p>
    </div>
    <div class="tool-matrix">
      <div class="tool-card">
        <div class="tool-top"><span class="tool-icon">{} </span><span class="tool-tag">FREE</span></div>
        <div class="tool-name">CSV / Excel 转 JSON</div>
        <div class="tool-copy">粘贴 CSV 内容，在浏览器本地转换成 JSON。Pro 方向是字段映射、批量文件和保存转换规则。</div>
        <button class="tool-action" onclick="openUtility('csv')">转换 JSON</button>
      </div>
      <div class="tool-card">
        <div class="tool-top"><span class="tool-icon">📄</span><span class="tool-tag">FREE</span></div>
        <div class="tool-name">Markdown 导出 PDF</div>
        <div class="tool-copy">把 Markdown 转成可打印预览，直接使用浏览器打印为 PDF。后续 Pro 支持高级模板。</div>
        <button class="tool-action" onclick="openUtility('markdown')">打开编辑器</button>
      </div>
      <div class="tool-card pro coming">
        <div class="tool-top"><span class="tool-icon">⚙️</span><span class="tool-tag">PRO SOON</span></div>
        <div class="tool-name">批量数据转换规则</div>
        <div class="tool-copy">为重复导入、清洗和转换任务保存规则，作为 Data Tools 的付费增强点。</div>
        <button class="tool-action" onclick="showPaywall()">查看 Pro</button>
      </div>
    </div>
  </section>

  <section class="utility-lab" id="utilityLab">
    <div class="section-head">
      <div class="section-title">Live Browser Tools</div>
      <p class="section-desc">这些工具已经可以直接使用，处理过程在浏览器本地完成，适合作为 SEO 引流和 AdSense 展示入口。</p>
    </div>
    <div class="utility-tabs">
      <button class="utility-tab on" id="util-tab-qr" onclick="openUtility('qr')">二维码</button>
      <button class="utility-tab" id="util-tab-social" onclick="openUtility('social')">社交卡片</button>
      <button class="utility-tab" id="util-tab-gradient" onclick="openUtility('gradient')">配色渐变</button>
      <button class="utility-tab" id="util-tab-csv" onclick="openUtility('csv')">CSV转JSON</button>
      <button class="utility-tab" id="util-tab-markdown" onclick="openUtility('markdown')">Markdown转PDF</button>
    </div>

    <div class="utility-panel show" id="util-qr">
      <div class="utility-grid">
        <div>
          <div class="utility-field">
            <label class="utility-label">二维码内容</label>
            <textarea class="utility-textarea" id="qrText">https://img-production-b10c.up.railway.app</textarea>
          </div>
          <div class="dimension-grid">
            <div class="utility-field">
              <label class="utility-label">前景色</label>
              <input class="utility-input" id="qrDark" type="color" value="#0e0e11">
            </div>
            <div class="utility-field">
              <label class="utility-label">背景色</label>
              <input class="utility-input" id="qrLight" type="color" value="#ffffff">
            </div>
          </div>
          <button class="btn" onclick="generateQR()">生成二维码</button>
          <button class="tool-action" onclick="downloadCanvas('qrCanvas', 'qrcode.png')">下载 PNG</button>
        </div>
        <div class="utility-output qr-box">
          <canvas id="qrCanvas" width="260" height="260"></canvas>
        </div>
      </div>
    </div>

    <div class="utility-panel" id="util-social">
      <div class="utility-grid">
        <div>
          <div class="utility-field">
            <label class="utility-label">标题</label>
            <input class="utility-input" id="cardTitle" value="Browser Tool Suite">
          </div>
          <div class="utility-field">
            <label class="utility-label">副标题</label>
            <input class="utility-input" id="cardSubtitle" value="Compress, convert, resize and create useful assets locally.">
          </div>
          <div class="dimension-grid">
            <div class="utility-field">
              <label class="utility-label">强调色</label>
              <input class="utility-input" id="cardAccent" type="color" value="#d4ff57">
            </div>
            <div class="utility-field">
              <label class="utility-label">版式</label>
              <select class="utility-select" id="cardTemplate">
                <option value="dark">Dark tool card</option>
                <option value="light">Clean light card</option>
              </select>
            </div>
          </div>
          <button class="btn" onclick="renderSocialCard()">生成并下载卡片</button>
        </div>
        <div class="utility-output">
          <canvas class="social-canvas" id="socialCanvas" width="1200" height="630"></canvas>
        </div>
      </div>
    </div>

    <div class="utility-panel" id="util-gradient">
      <div class="utility-grid">
        <div>
          <div class="utility-field">
            <label class="utility-label">方向</label>
            <select class="utility-select" id="gradientDirection">
              <option value="135deg">Diagonal</option>
              <option value="90deg">Horizontal</option>
              <option value="180deg">Vertical</option>
              <option value="45deg">Soft angle</option>
            </select>
          </div>
          <button class="btn" onclick="generateGradient()">随机生成配色</button>
        </div>
        <div class="utility-output">
          <div class="gradient-preview" id="gradientPreview"></div>
          <div class="swatch-row" id="swatchRow"></div>
          <pre class="json-output" id="gradientCode"></pre>
        </div>
      </div>
    </div>

    <div class="utility-panel" id="util-csv">
      <div class="utility-grid">
        <div>
          <div class="utility-field">
            <label class="utility-label">CSV内容</label>
            <textarea class="utility-textarea" id="csvInput">name,email,plan
Alice,alice@example.com,free
Bob,bob@example.com,pro</textarea>
          </div>
          <button class="btn" onclick="convertCSV()">转换为 JSON</button>
        </div>
        <div class="utility-output">
          <pre class="json-output" id="jsonOutput"></pre>
        </div>
      </div>
    </div>

    <div class="utility-panel" id="util-markdown">
      <div class="utility-grid">
        <div>
          <div class="utility-field">
            <label class="utility-label">Markdown</label>
            <textarea class="utility-textarea" id="markdownInput"># Project Notes

## Tools
- Image compression
- Format conversion
- CSV to JSON

**Export this page with browser print.**</textarea>
          </div>
          <button class="btn" onclick="renderMarkdown(true)">预览并打印PDF</button>
        </div>
        <div class="utility-output markdown-preview" id="markdownPreview"></div>
      </div>
    </div>
  </section>

  <section class="pro-strip">
    <div class="pro-strip-title">Unlock Pro Tools — $10 one-time</div>
    <p class="pro-strip-copy">Pro 不是单独买一个按钮，而是解锁整个工具包里的高级能力：批量图片处理、背景移除、高清无水印导出、更多模板，以及未来新增的 Pro 工具。</p>
    <div class="pro-points">
      <div class="pro-point">Batch image compression</div>
      <div class="pro-point">Background remover access</div>
      <div class="pro-point">Future Pro tools included</div>
    </div>
    <button class="btn pay" onclick="showPaywall()">使用 PayPal 解锁 $10 →</button>
  </section>

  <div class="faq">
    <div class="faq-title">常见问题</div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">如何把图片压缩到200KB以内？<span class="faq-arrow">+</span></div>
      <div class="faq-a">上传图片后，在目标大小输入框填写"200"，点击"开始压缩"，工具会自动将图片压缩到200KB以内并下载。适合政府表单、招聘网站等对图片大小有严格限制的场景。</div>
    </div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">图片压缩后会不会很模糊？<span class="faq-arrow">+</span></div>
      <div class="faq-a">我们使用二分法算法，在满足目标大小的前提下尽量保留最高画质。目标大小设置越接近原图大小，画质损失越小。</div>
    </div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">支持压缩到1MB、500KB、100KB吗？<span class="faq-arrow">+</span></div>
      <div class="faq-a">支持任意目标大小，可以直接点击预设按钮，也可以手动输入任意数值。无论压缩到1MB还是50KB都可以处理。</div>
    </div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">图片会上传到服务器保存吗？<span class="faq-arrow">+</span></div>
      <div class="faq-a">不会。所有压缩操作完全在你的浏览器本地完成，图片数据从不离开你的设备，完全私密安全。</div>
    </div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">支持哪些图片格式？PNG能压缩吗？<span class="faq-arrow">+</span></div>
      <div class="faq-a">支持 JPG、PNG、WebP 格式。PNG 图片会自动转换为 JPG 格式进行压缩，压缩效果更好，文件体积更小。</div>
    </div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">微信发图片太模糊，怎么办？<span class="faq-arrow">+</span></div>
      <div class="faq-a">微信会对超过5MB的图片进行二次压缩。建议将图片压缩到4MB以内再发送，在目标大小输入"4096"即可有效避免微信自动压缩。</div>
    </div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">免费次数用完了怎么办？<span class="faq-arrow">+</span></div>
      <div class="faq-a">免费额度用完后，支付$10即可解锁无限次使用，一次付费永久有效，不收月费。</div>
    </div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">这个网站只有图片压缩工具吗？<span class="faq-arrow">+</span></div>
      <div class="faq-a">不止。现在已经支持图片压缩、格式转换和尺寸转换，后续会逐步加入批量图片处理、背景移除、二维码生成、社交媒体卡片、CSV转JSON和Markdown转PDF等浏览器本地工具。</div>
    </div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">尺寸转换支持自定义宽高吗？<span class="faq-arrow">+</span></div>
      <div class="faq-a">支持。你可以输入任意宽度和高度，也可以使用200x200、300x300、512x512、1080x1080等预设。适配方式支持留白适配、裁剪填满和拉伸。</div>
    </div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">哪些功能是免费的，哪些是Pro？<span class="faq-arrow">+</span></div>
      <div class="faq-a">单张图片压缩、格式转换和尺寸转换是免费入口。批量压缩、背景移除、批量尺寸和格式处理、高清无水印导出以及未来高级模板会放入Pro工具包。</div>
    </div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">CSV转JSON和Markdown转PDF会上传文件吗？<span class="faq-arrow">+</span></div>
      <div class="faq-a">规划方向是浏览器本地解析和导出，尽量不把文件上传到服务器。这样既节省服务器成本，也更适合处理个人资料、运营表格和技术文档。</div>
    </div>
  </div>
</div>

<!-- Paywall Modal -->
<div class="overlay" id="overlay">
  <div class="modal">
    <div class="modal-icon">🎉</div>
    <div class="modal-title">解锁 Pro 工具</div>
    <div class="modal-desc">解锁批量图片压缩和无限次单张压缩。支付一次，永久使用。</div>
    <div class="modal-price">$10</div>
    <div class="modal-price-desc">一次付费 · 永久有效 · 无月费</div>
    <div class="modal-features">
      <div class="modal-feature">批量图片压缩</div>
      <div class="modal-feature">背景移除与未来Pro工具</div>
      <div class="modal-feature">高清无水印导出</div>
      <div class="modal-feature">一次付费，解锁工具包</div>
    </div>
    <button class="btn pay" onclick="goPay()">使用 PayPal 解锁 $10 →</button>
    <div class="auth-hint">已经付款？用付款时的 PayPal 邮箱登录或注册，系统会自动识别 Pro 状态。</div>
    <button class="quota-unlock" style="margin-top:14px" onclick="showAuthModal()">登录 / 注册激活</button>
    <div class="modal-close" onclick="closeModal()">暂时不需要</div>
  </div>
</div>

<!-- Auth Modal -->
<div class="overlay" id="authOverlay">
  <div class="modal">
    <div class="modal-title" id="authTitle">登录</div>
    <div class="modal-desc">只用邮箱和密码，服务器只负责账号与 Pro 状态。</div>
    <div class="auth-switch">
      <button id="authLoginTab" class="on" onclick="setAuthMode('login')">登录</button>
      <button id="authRegisterTab" onclick="setAuthMode('register')">注册</button>
    </div>
    <div class="auth-form">
      <input class="auth-input" id="authEmail" type="email" autocomplete="email" placeholder="PayPal 邮箱">
      <input class="auth-input" id="authPassword" type="password" autocomplete="current-password" placeholder="密码，至少 8 位">
      <button class="btn" id="authSubmit" onclick="submitAuth()">继续</button>
      <div class="auth-msg" id="authMsg"></div>
    </div>
    <div class="auth-hint">付款后请使用 PayPal 付款邮箱登录。没有付款也可以先注册，免费工具照常使用。</div>
    <div class="modal-close" onclick="closeAuthModal()">关闭</div>
  </div>
</div>

<script>
const FREE_LIMIT = 100;
const STORAGE_KEY = 'img_compress_count';
const PAYPAL_PAYMENT_LINK = 'https://www.paypal.com/ncp/payment/LN55VSJYNE252';

let f = null;
let usedCount = parseInt(localStorage.getItem(STORAGE_KEY) || '0');
let account = { authenticated: false, paid: false, email: '' };
let authMode = 'login';
let activeTool = 'compress';
let outputType = 'image/jpeg';
let outputLabel = 'JPG';
let resizeMode = 'contain';

const dz = document.getElementById('dz');
const fi = document.getElementById('fi');
const prev = document.getElementById('prev');
const pimg = document.getElementById('pimg');
const pname = document.getElementById('pname');
const psize = document.getElementById('psize');
const btn = document.getElementById('btn');
const st = document.getElementById('st');
const dropHint = document.getElementById('dropHint');
const singleTool = document.getElementById('singleTool');
const batchPanel = document.getElementById('batchPanel');
const compressControls = document.getElementById('compressControls');
const convertControls = document.getElementById('convertControls');
const resizeControls = document.getElementById('resizeControls');
const resizeW = document.getElementById('resizeW');
const resizeH = document.getElementById('resizeH');

initAccount();
updateQuota();

dz.onclick = () => fi.click();
dz.ondragover = e => { e.preventDefault(); dz.classList.add('over'); };
dz.ondragleave = () => dz.classList.remove('over');
dz.ondrop = e => { e.preventDefault(); dz.classList.remove('over'); if (e.dataTransfer.files[0]) load(e.dataTransfer.files[0]); };
fi.onchange = e => { if (e.target.files[0]) load(e.target.files[0]); };

function switchTool(tool) {
  activeTool = tool;
  document.querySelectorAll('.tool-tab').forEach(tab => tab.classList.remove('on'));
  document.getElementById('tab-' + tool).classList.add('on');
  singleTool.classList.toggle('hidden', tool === 'batch');
  batchPanel.classList.toggle('show', tool === 'batch');

  if (tool === 'batch') {
    st.className = 'status';
    return;
  }

  compressControls.classList.toggle('hidden', tool !== 'compress');
  convertControls.classList.toggle('hidden', tool !== 'convert');
  resizeControls.classList.toggle('hidden', tool !== 'resize');
  if (tool === 'convert') {
    dropHint.textContent = '免费转换 JPG · PNG · WebP · 图片不上传服务器';
  } else if (tool === 'resize') {
    dropHint.textContent = '自定义宽高 · 头像尺寸 · 平台上传尺寸 · 本地处理';
  } else {
    dropHint.textContent = 'JPG · PNG · WebP · 本地处理，不上传服务器';
  }
  st.className = 'status';
  updateButtonText();
}

function runTool() {
  if (activeTool === 'convert') {
    convertImage();
    return;
  }
  if (activeTool === 'resize') {
    resizeImage();
    return;
  }
  compress();
}

function load(file) {
  if (!file.type.startsWith('image/')) { showStatus('err', '请选择图片文件'); return; }
  f = file;
  const r = new FileReader();
  r.onload = e => {
    pimg.src = e.target.result;
    pname.textContent = file.name;
    psize.textContent = '原始大小：' + (file.size / 1024).toFixed(1) + ' KB';
    prev.classList.add('show');
    dz.style.display = 'none';
  };
  r.readAsDataURL(file);
  btn.disabled = false;
  updateButtonText();
  st.className = 'status';
}

function updateButtonText() {
  if (!f) {
    btn.textContent = '选择图片后开始';
    btn.disabled = true;
    return;
  }
  btn.disabled = false;
  if (activeTool === 'convert') {
    btn.textContent = '免费转换为 ' + outputLabel + ' →';
  } else if (activeTool === 'resize') {
    btn.textContent = '转换尺寸 →';
  } else {
    btn.textContent = '开始压缩 →';
  }
}

function setTarget(kb) {
  document.getElementById('tgt').value = kb;
  document.querySelectorAll('.preset').forEach(b => {
    const val = parseInt(b.textContent);
    b.classList.toggle('on', val === kb || (kb === 1024 && b.textContent.includes('1 MB')) || (kb === 2048 && b.textContent.includes('2 MB')));
  });
}

function setFormat(type, label) {
  outputType = type;
  outputLabel = label;
  document.querySelectorAll('.format-option').forEach(b => {
    b.classList.toggle('on', b.textContent.trim() === label);
  });
  updateButtonText();
}

function setResizePreset(w, h) {
  resizeW.value = w;
  resizeH.value = h;
  document.querySelectorAll('.resize-preset').forEach(b => {
    b.classList.toggle('on', b.textContent.trim() === w + 'x' + h);
  });
}

function setResizeMode(mode, el) {
  resizeMode = mode;
  document.querySelectorAll('.mode-grid .format-option').forEach(b => b.classList.remove('on'));
  el.classList.add('on');
}

function updateQuota() {
  if (isPaid()) {
    document.getElementById('quotaText').textContent = 'PRO';
    const fill = document.getElementById('quotaFill');
    fill.style.width = '100%';
    fill.className = 'quota-fill';
    return;
  }
  const remaining = Math.max(0, FREE_LIMIT - usedCount);
  const pct = (remaining / FREE_LIMIT) * 100;
  document.getElementById('quotaText').textContent = remaining + ' / ' + FREE_LIMIT;
  const fill = document.getElementById('quotaFill');
  fill.style.width = pct + '%';
  fill.className = 'quota-fill' + (remaining <= 10 ? ' low' : '');
}

async function compress() {
  if (!f) return;

  // 检查免费次数
  if (!isPaid() && usedCount >= FREE_LIMIT) {
    showPaywall();
    return;
  }

  const targetKB = parseFloat(document.getElementById('tgt').value);
  if (!targetKB || targetKB <= 0) { showStatus('err', '请输入有效的目标大小'); return; }
  const targetBytes = targetKB * 1024;

  btn.disabled = true;
  btn.textContent = '压缩中...';
  showStatus('loading', '正在压缩，请稍候...');

  try {
    const result = await compressInBrowser(f, targetBytes);
    const origKB = (f.size / 1024).toFixed(1);
    const resultKB = (result.size / 1024).toFixed(1);
    const saved = (((f.size - result.size) / f.size) * 100).toFixed(0);

    // 下载
    const url = URL.createObjectURL(result);
    const a = document.createElement('a');
    const ext = result.type === 'image/png' ? '.png' : '.jpg';
    const base = f.name.replace(/\.[^.]+$/, '');
    a.download = 'compressed_' + base + ext;
    a.href = url;
    a.click();
    URL.revokeObjectURL(url);

    // 记录次数
    if (!isPaid()) {
      usedCount++;
      localStorage.setItem(STORAGE_KEY, usedCount);
      updateQuota();
    }

    showStatus('ok', origKB + ' KB → ' + resultKB + ' KB · 减少 ' + saved + '% · 已自动下载');
    psize.textContent = '原始：' + origKB + ' KB → 压缩后：' + resultKB + ' KB';
  } catch (e) {
    showStatus('err', '压缩失败，请重试');
  } finally {
    btn.disabled = false;
    btn.textContent = '再次压缩 →';
  }
}

async function convertImage() {
  if (!f) return;

  btn.disabled = true;
  btn.textContent = '转换中...';
  showStatus('loading', '正在转换格式，请稍候...');

  try {
    const result = await convertInBrowser(f, outputType);
    const resultKB = (result.size / 1024).toFixed(1);
    const ext = outputType === 'image/png' ? '.png' : outputType === 'image/webp' ? '.webp' : '.jpg';
    const base = f.name.replace(/\.[^.]+$/, '');
    downloadBlob(result, 'converted_' + base + ext);
    showStatus('ok', '已转换为 ' + outputLabel + ' · ' + resultKB + ' KB · 已自动下载');
    psize.textContent = '已转换为：' + outputLabel + ' · ' + resultKB + ' KB';
  } catch (e) {
    showStatus('err', '转换失败，请重试');
  } finally {
    btn.disabled = false;
    updateButtonText();
  }
}

async function resizeImage() {
  if (!f) return;

  const width = parseInt(resizeW.value, 10);
  const height = parseInt(resizeH.value, 10);
  if (!width || !height || width <= 0 || height <= 0) {
    showStatus('err', '请输入有效的宽度和高度');
    return;
  }
  if (width > 12000 || height > 12000) {
    showStatus('err', '尺寸过大，请输入12000以内的宽高');
    return;
  }

  btn.disabled = true;
  btn.textContent = '处理中...';
  showStatus('loading', '正在转换图片尺寸...');

  try {
    const result = await resizeInBrowser(f, width, height, resizeMode);
    const resultKB = (result.size / 1024).toFixed(1);
    const base = f.name.replace(/\.[^.]+$/, '');
    downloadBlob(result, 'resized_' + base + '_' + width + 'x' + height + '.jpg');
    showStatus('ok', '已转换为 ' + width + 'x' + height + ' · ' + resultKB + ' KB · 已自动下载');
    psize.textContent = '已转换尺寸：' + width + 'x' + height + ' · ' + resultKB + ' KB';
  } catch (e) {
    showStatus('err', '尺寸转换失败，请重试');
  } finally {
    btn.disabled = false;
    updateButtonText();
  }
}

// 浏览器端二分法压缩
function compressInBrowser(file, targetBytes) {
  return new Promise((resolve, reject) => {
    const img = new Image();
    const url = URL.createObjectURL(file);
    img.onload = () => {
      URL.revokeObjectURL(url);
      const canvas = document.createElement('canvas');
      canvas.width = img.naturalWidth;
      canvas.height = img.naturalHeight;
      const ctx = canvas.getContext('2d');
      ctx.drawImage(img, 0, 0);

      // 如果原图已经小于目标，直接返回
      if (file.size <= targetBytes) {
        canvas.toBlob(blob => resolve(blob), 'image/jpeg', 0.95);
        return;
      }

      // 二分法找最优 quality
      let low = 0.01, high = 0.95, bestBlob = null, attempts = 0;

      function tryQuality(q) {
        canvas.toBlob(blob => {
          attempts++;
          if (!blob) { reject(new Error('Failed')); return; }

          if (blob.size <= targetBytes) {
            bestBlob = blob;
            low = q;
          } else {
            high = q;
          }

          if (attempts >= 10 || (high - low) < 0.01) {
            if (bestBlob) {
              resolve(bestBlob);
            } else {
              // 还是太大，用最低质量
              canvas.toBlob(b => resolve(b), 'image/jpeg', 0.01);
            }
            return;
          }
          tryQuality((low + high) / 2);
        }, 'image/jpeg', q);
      }

      tryQuality((low + high) / 2);
    };
    img.onerror = reject;
    img.src = url;
  });
}

function convertInBrowser(file, type) {
  return new Promise((resolve, reject) => {
    const img = new Image();
    const url = URL.createObjectURL(file);
    img.onload = () => {
      URL.revokeObjectURL(url);
      const canvas = document.createElement('canvas');
      canvas.width = img.naturalWidth;
      canvas.height = img.naturalHeight;
      const ctx = canvas.getContext('2d');
      if (type === 'image/jpeg') {
        ctx.fillStyle = '#ffffff';
        ctx.fillRect(0, 0, canvas.width, canvas.height);
      }
      ctx.drawImage(img, 0, 0);
      canvas.toBlob(blob => {
        if (!blob) { reject(new Error('Failed')); return; }
        resolve(blob);
      }, type, 0.92);
    };
    img.onerror = reject;
    img.src = url;
  });
}

function resizeInBrowser(file, width, height, mode) {
  return new Promise((resolve, reject) => {
    const img = new Image();
    const url = URL.createObjectURL(file);
    img.onload = () => {
      URL.revokeObjectURL(url);
      const canvas = document.createElement('canvas');
      canvas.width = width;
      canvas.height = height;
      const ctx = canvas.getContext('2d');
      ctx.fillStyle = '#ffffff';
      ctx.fillRect(0, 0, width, height);

      let sx = 0, sy = 0, sw = img.naturalWidth, sh = img.naturalHeight;
      let dx = 0, dy = 0, dw = width, dh = height;

      if (mode === 'contain') {
        const scale = Math.min(width / img.naturalWidth, height / img.naturalHeight);
        dw = img.naturalWidth * scale;
        dh = img.naturalHeight * scale;
        dx = (width - dw) / 2;
        dy = (height - dh) / 2;
      } else if (mode === 'cover') {
        const targetRatio = width / height;
        const imageRatio = img.naturalWidth / img.naturalHeight;
        if (imageRatio > targetRatio) {
          sw = img.naturalHeight * targetRatio;
          sx = (img.naturalWidth - sw) / 2;
        } else {
          sh = img.naturalWidth / targetRatio;
          sy = (img.naturalHeight - sh) / 2;
        }
      }

      ctx.drawImage(img, sx, sy, sw, sh, dx, dy, dw, dh);
      canvas.toBlob(blob => {
        if (!blob) { reject(new Error('Failed')); return; }
        resolve(blob);
      }, 'image/jpeg', 0.92);
    };
    img.onerror = reject;
    img.src = url;
  });
}

function downloadBlob(blob, filename) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.download = filename;
  a.href = url;
  a.click();
  URL.revokeObjectURL(url);
}

function openUtility(tool) {
  document.querySelectorAll('.utility-tab').forEach(tab => tab.classList.remove('on'));
  document.querySelectorAll('.utility-panel').forEach(panel => panel.classList.remove('show'));
  document.getElementById('util-tab-' + tool).classList.add('on');
  document.getElementById('util-' + tool).classList.add('show');
  document.getElementById('utilityLab').scrollIntoView({ behavior: 'smooth', block: 'start' });
  if (tool === 'qr') generateQR();
  if (tool === 'gradient') generateGradient();
  if (tool === 'social') renderSocialCard(false);
  if (tool === 'csv') convertCSV();
  if (tool === 'markdown') renderMarkdown(false);
}

function generateQR() {
  const text = document.getElementById('qrText').value.trim();
  const canvas = document.getElementById('qrCanvas');
  if (!text) return;
  if (!window.qrcode) {
    const ctx = canvas.getContext('2d');
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    ctx.fillStyle = '#ffffff';
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    ctx.fillStyle = '#0e0e11';
    ctx.font = '14px sans-serif';
    ctx.fillText('二维码库加载失败', 72, 128);
    return;
  }
  const qr = qrcode(0, 'M');
  qr.addData(text);
  qr.make();
  const ctx = canvas.getContext('2d');
  const count = qr.getModuleCount();
  const margin = 16;
  const cell = Math.floor((canvas.width - margin * 2) / count);
  const size = cell * count;
  const offset = Math.floor((canvas.width - size) / 2);
  const dark = document.getElementById('qrDark').value;
  const light = document.getElementById('qrLight').value;
  ctx.fillStyle = light;
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  ctx.fillStyle = dark;
  for (let row = 0; row < count; row++) {
    for (let col = 0; col < count; col++) {
      if (qr.isDark(row, col)) {
        ctx.fillRect(offset + col * cell, offset + row * cell, cell, cell);
      }
    }
  }
}

function downloadCanvas(id, filename) {
  const canvas = document.getElementById(id);
  canvas.toBlob(blob => {
    if (blob) downloadBlob(blob, filename);
  }, 'image/png');
}

function renderSocialCard(shouldDownload = true) {
  const canvas = document.getElementById('socialCanvas');
  const ctx = canvas.getContext('2d');
  const title = document.getElementById('cardTitle').value || 'Browser Tool Suite';
  const subtitle = document.getElementById('cardSubtitle').value || 'Useful tools that run locally in your browser.';
  const accent = document.getElementById('cardAccent').value || '#d4ff57';
  const template = document.getElementById('cardTemplate').value;
  const light = template === 'light';
  ctx.fillStyle = light ? '#f7f7f2' : '#0e0e11';
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  ctx.fillStyle = accent;
  ctx.fillRect(0, 0, 26, canvas.height);
  ctx.globalAlpha = 0.15;
  for (let x = 90; x < canvas.width; x += 90) {
    ctx.fillRect(x, 0, 1, canvas.height);
  }
  for (let y = 90; y < canvas.height; y += 90) {
    ctx.fillRect(0, y, canvas.width, 1);
  }
  ctx.globalAlpha = 1;
  ctx.fillStyle = light ? '#111111' : '#ffffff';
  ctx.font = '800 72px Syne, sans-serif';
  wrapCanvasText(ctx, title, 95, 230, 960, 82);
  ctx.fillStyle = light ? '#4a4a4a' : '#b7b7b7';
  ctx.font = '400 30px DM Sans, sans-serif';
  wrapCanvasText(ctx, subtitle, 100, 400, 920, 42);
  ctx.fillStyle = accent;
  ctx.font = '800 24px Syne, sans-serif';
  ctx.fillText('IMGTOOLS · BROWSER LOCAL', 100, 545);
  if (shouldDownload) downloadCanvas('socialCanvas', 'social-card.png');
}

function wrapCanvasText(ctx, text, x, y, maxWidth, lineHeight) {
  const words = text.split(/\s+/);
  let line = '';
  for (const word of words) {
    const test = line ? line + ' ' + word : word;
    if (ctx.measureText(test).width > maxWidth && line) {
      ctx.fillText(line, x, y);
      line = word;
      y += lineHeight;
    } else {
      line = test;
    }
  }
  if (line) ctx.fillText(line, x, y);
}

function randomColor() {
  return '#' + Math.floor(Math.random() * 16777215).toString(16).padStart(6, '0');
}

function generateGradient() {
  const colors = [randomColor(), randomColor(), randomColor()];
  const direction = document.getElementById('gradientDirection').value;
  const css = 'linear-gradient(' + direction + ', ' + colors.join(', ') + ')';
  document.getElementById('gradientPreview').style.background = css;
  document.getElementById('swatchRow').innerHTML = colors.map(c => '<div class="swatch" title="' + c + '" style="background:' + c + '"></div>').join('');
  document.getElementById('gradientCode').textContent = 'background: ' + css + ';';
}

function parseCSV(text) {
  const rows = [];
  let row = [], cell = '', quote = false;
  for (let i = 0; i < text.length; i++) {
    const ch = text[i];
    const next = text[i + 1];
    if (ch === '"' && quote && next === '"') {
      cell += '"';
      i++;
    } else if (ch === '"') {
      quote = !quote;
    } else if (ch === ',' && !quote) {
      row.push(cell.trim());
      cell = '';
    } else if ((ch === '\n' || ch === '\r') && !quote) {
      if (ch === '\r' && next === '\n') i++;
      row.push(cell.trim());
      if (row.some(v => v !== '')) rows.push(row);
      row = [];
      cell = '';
    } else {
      cell += ch;
    }
  }
  row.push(cell.trim());
  if (row.some(v => v !== '')) rows.push(row);
  return rows;
}

function convertCSV() {
  const rows = parseCSV(document.getElementById('csvInput').value);
  if (rows.length < 2) {
    document.getElementById('jsonOutput').textContent = '请粘贴带表头的CSV内容。';
    return;
  }
  const headers = rows[0];
  const data = rows.slice(1).map(row => {
    const item = {};
    headers.forEach((header, index) => {
      item[header || 'field_' + index] = row[index] || '';
    });
    return item;
  });
  document.getElementById('jsonOutput').textContent = JSON.stringify(data, null, 2);
}

function escapeHTML(value) {
  return value.replace(/[&<>"']/g, ch => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[ch]));
}

function markdownToHTML(markdown) {
  const lines = markdown.split(/\r?\n/);
  let html = '', inList = false;
  for (const raw of lines) {
    const line = raw.trim();
    if (!line) {
      if (inList) { html += '</ul>'; inList = false; }
      continue;
    }
    if (line.startsWith('# ')) {
      if (inList) { html += '</ul>'; inList = false; }
      html += '<h1>' + inlineMarkdown(line.slice(2)) + '</h1>';
    } else if (line.startsWith('## ')) {
      if (inList) { html += '</ul>'; inList = false; }
      html += '<h2>' + inlineMarkdown(line.slice(3)) + '</h2>';
    } else if (line.startsWith('### ')) {
      if (inList) { html += '</ul>'; inList = false; }
      html += '<h3>' + inlineMarkdown(line.slice(4)) + '</h3>';
    } else if (line.startsWith('- ')) {
      if (!inList) { html += '<ul>'; inList = true; }
      html += '<li>' + inlineMarkdown(line.slice(2)) + '</li>';
    } else {
      if (inList) { html += '</ul>'; inList = false; }
      html += '<p>' + inlineMarkdown(line) + '</p>';
    }
  }
  if (inList) html += '</ul>';
  return html;
}

function inlineMarkdown(text) {
  return escapeHTML(text)
    .replace(/\*\*(.*?)\*\*/g, '<strong>$1</strong>');
}

function renderMarkdown(shouldPrint) {
  const html = markdownToHTML(document.getElementById('markdownInput').value);
  document.getElementById('markdownPreview').innerHTML = html;
  if (shouldPrint) {
    const w = window.open('', '_blank');
    w.document.write('<!doctype html><html><head><title>Markdown PDF</title><style>body{font-family:Arial,sans-serif;max-width:760px;margin:40px auto;line-height:1.65;color:#111}code{background:#eee;padding:2px 5px;border-radius:4px}</style></head><body>' + html + '<script>window.print()<\/script></body></html>');
    w.document.close();
  }
}

function jumpTool(tool) {
  switchTool(tool);
  document.querySelector('.card').scrollIntoView({ behavior: 'smooth', block: 'start' });
}

function showSoon(name) {
  closeModal();
  st.className = 'status show loading';
  st.innerHTML = '<div class="sdot"></div><span>' + name + ' 即将上线，先试试当前可用工具。</span>';
  document.querySelector('.card').scrollIntoView({ behavior: 'smooth', block: 'start' });
}

function showPaywall() {
  document.getElementById('overlay').classList.add('show');
}

function closeModal() {
  document.getElementById('overlay').classList.remove('show');
}

function isPaid() {
  return account && account.paid === true;
}

async function initAccount() {
  try {
    const res = await fetch('/api/me', { credentials: 'same-origin' });
    const data = await res.json();
    if (data && data.ok) {
      account = {
        authenticated: data.authenticated === true,
        paid: data.paid === true,
        email: data.email || ''
      };
      renderAccount();
      updateQuota();
    }
  } catch (e) {
    renderAccount();
  }
}

function renderAccount() {
  const pill = document.getElementById('accountPill');
  if (!pill) return;
  if (!account.authenticated) {
    pill.className = 'account-pill';
    pill.innerHTML = '登录 / 注册';
    pill.onclick = showAuthModal;
    return;
  }
  pill.className = 'account-pill' + (account.paid ? ' pro' : '');
  pill.innerHTML = '<span class="account-email">' + escapeHTML(account.email) + '</span><span>' + (account.paid ? 'PRO' : 'FREE') + '</span>';
  pill.onclick = showAccountMenu;
}

function showAccountMenu() {
  if (!account.authenticated) {
    showAuthModal();
    return;
  }
  const action = confirm((account.paid ? 'Pro 已激活' : '当前是免费账号') + '\n\n点击确定退出登录，取消保留登录。');
  if (action) logout();
}

function showAuthModal(mode) {
  closeModal();
  setAuthMode(mode || authMode || 'login');
  document.getElementById('authOverlay').classList.add('show');
  setTimeout(() => document.getElementById('authEmail').focus(), 50);
}

function closeAuthModal() {
  document.getElementById('authOverlay').classList.remove('show');
}

function setAuthMode(mode) {
  authMode = mode === 'register' ? 'register' : 'login';
  document.getElementById('authTitle').textContent = authMode === 'register' ? '注册' : '登录';
  document.getElementById('authLoginTab').classList.toggle('on', authMode === 'login');
  document.getElementById('authRegisterTab').classList.toggle('on', authMode === 'register');
  document.getElementById('authSubmit').textContent = authMode === 'register' ? '注册并登录' : '登录';
  document.getElementById('authMsg').textContent = '';
  document.getElementById('authMsg').className = 'auth-msg';
}

async function submitAuth() {
  const email = document.getElementById('authEmail').value.trim();
  const password = document.getElementById('authPassword').value;
  const msg = document.getElementById('authMsg');
  const submit = document.getElementById('authSubmit');
  msg.className = 'auth-msg';
  msg.textContent = '';
  if (!email || !email.includes('@')) {
    msg.className = 'auth-msg err';
    msg.textContent = '请输入有效邮箱。';
    return;
  }
  if (password.length < 8) {
    msg.className = 'auth-msg err';
    msg.textContent = '密码至少 8 位。';
    return;
  }
  submit.disabled = true;
  submit.textContent = '处理中...';
  try {
    const res = await fetch('/api/' + authMode, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email, password })
    });
    const data = await res.json();
    if (!res.ok || !data.ok) {
      msg.className = 'auth-msg err';
      msg.textContent = authErrorText(data.error);
      return;
    }
    account = { authenticated: true, paid: data.paid === true, email: data.email || email };
    renderAccount();
    updateQuota();
    msg.className = 'auth-msg ok';
    msg.textContent = account.paid ? '已登录，Pro 已激活。' : '已登录，当前是免费账号。';
    setTimeout(closeAuthModal, 700);
  } catch (e) {
    msg.className = 'auth-msg err';
    msg.textContent = '网络异常，请稍后重试。';
  } finally {
    submit.disabled = false;
    submit.textContent = authMode === 'register' ? '注册并登录' : '登录';
  }
}

function authErrorText(code) {
  if (code === 'email_exists') return '这个邮箱已经注册，请切换到登录。';
  if (code === 'weak_password') return '密码至少 8 位。';
  if (code === 'invalid_credentials') return '邮箱或密码不正确。';
  if (code === 'invalid_email') return '邮箱格式不正确。';
  return '操作失败，请稍后重试。';
}

async function logout() {
  try {
    await fetch('/api/logout', { method: 'POST', credentials: 'same-origin' });
  } catch (e) {}
  account = { authenticated: false, paid: false, email: '' };
  renderAccount();
  updateQuota();
}

function goPay() {
  window.location.href = PAYPAL_PAYMENT_LINK;
}

function showStatus(type, msg) {
  st.className = 'status show ' + type;
  if (type === 'loading') {
    st.innerHTML = '<div class="spinner"></div><span>' + msg + '</span>';
  } else {
    st.innerHTML = '<div class="sdot"></div><span>' + msg + '</span>';
  }
}

generateQR();
renderSocialCard(false);
generateGradient();
convertCSV();
renderMarkdown(false);
</script>
</body>
</html>`
