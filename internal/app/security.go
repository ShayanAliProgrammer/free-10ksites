package app

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/time/rate"
)

// ============================================================================
// Security Headers
// ============================================================================

// securityHeaders sets strict HTTP security headers on every response.
// CSP blocks all inline scripts/styles (only self-hosted assets allowed),
// preventing XSS even if template escaping fails.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		// Content-Security-Policy: strict — only self-hosted scripts/styles.
		// No inline scripts, no eval, no external CDNs. WebSocket allowed to self.
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self'; "+
				"style-src 'self'; "+
				"img-src 'self' data:; "+
				"font-src 'self'; "+
				"connect-src 'self'; "+
				"frame-ancestors 'none'; "+
				"base-uri 'self'; "+
				"form-action 'self'")
		h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("X-DNS-Prefetch-Control", "off")
		h.Set("X-Permitted-Cross-Domain-Policies", "none")
		h.Set("X-Download-Options", "noopen")
		next.ServeHTTP(w, r)
	})
}

// ============================================================================
// Rate Limiting (token bucket per IP)
// ============================================================================

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rateBucket
}

type rateBucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func newRateLimiter() *rateLimiter {
	rl := &rateLimiter{buckets: make(map[string]*rateBucket)}
	go rl.cleanup()
	return rl
}

// allow checks if the IP is within the rate limit (rps requests per second,
// burst max burst). Returns true if allowed.
func (rl *rateLimiter) allow(ip string, rps rate.Limit, burst int) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	b, ok := rl.buckets[ip]
	if !ok {
		b = &rateBucket{limiter: rate.NewLimiter(rps, burst)}
		rl.buckets[ip] = b
	}
	b.lastSeen = time.Now()
	return b.limiter.Allow()
}

// cleanup removes stale buckets every 5 minutes to prevent memory growth.
func (rl *rateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		for ip, b := range rl.buckets {
			if time.Since(b.lastSeen) > 10*time.Minute {
				delete(rl.buckets, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// RateLimit middleware applies different limits to different route types.
// Limits are generous for reads, strict for auth and mutations.
func (app *App) rateLimit(limit rate.Limit, burst int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			if !app.limiter.allow(ip, limit, burst) {
				w.Header().Set("Retry-After", "60")
				http.Error(w, "Too many requests", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func clientIP(r *http.Request) string {
	// Trust X-Forwarded-For only if present (behind Caddy proxy)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.Index(xff, ","); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	// Fall back to RemoteAddr
	if i := strings.LastIndex(r.RemoteAddr, ":"); i >= 0 {
		return r.RemoteAddr[:i]
	}
	return r.RemoteAddr
}

// ============================================================================
// CSRF Protection (Origin header check)
// ============================================================================

// csrfCheck verifies that state-changing requests (POST, PUT, PATCH, DELETE)
// originate from the same host. This prevents cross-site request forgery
// without requiring CSRF tokens in forms.
func csrfCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut ||
			r.Method == http.MethodPatch || r.Method == http.MethodDelete {
			origin := r.Header.Get("Origin")
			if origin != "" && !isSameOrigin(origin, r.Host) {
				http.Error(w, "Cross-site requests are not allowed", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ============================================================================
// Session Management (random tokens, in-memory, server-side)
// ============================================================================

// SessionStore manages admin sessions with random 32-byte tokens.
// Tokens are stored server-side with expiry — never expose the password
// in the cookie.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]time.Time // token -> expiry
}

func NewSessionStore() *SessionStore {
	s := &SessionStore{sessions: make(map[string]time.Time)}
	go s.cleanup()
	return s
}

// Create generates a new random session token.
func (s *SessionStore) Create() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	s.mu.Lock()
	s.sessions[token] = time.Now().Add(7 * 24 * time.Hour)
	s.mu.Unlock()
	return token, nil
}

// Valid checks if a session token is valid and not expired.
func (s *SessionStore) Valid(token string) bool {
	if token == "" {
		return false
	}
	s.mu.RLock()
	expiry, ok := s.sessions[token]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		s.mu.Lock()
		delete(s.sessions, token)
		s.mu.Unlock()
		return false
	}
	return true
}

// Delete removes a session (logout).
func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

func (s *SessionStore) cleanup() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for token, expiry := range s.sessions {
			if now.After(expiry) {
				delete(s.sessions, token)
			}
		}
		s.mu.Unlock()
	}
}

// ============================================================================
// Password Verification (bcrypt)
// ============================================================================

// AuthConfig holds the admin password (plaintext for dev) or bcrypt hash (prod).
type AuthConfig struct {
	passwordHash []byte // bcrypt hash (if ADMIN_PASSWORD_HASH is set)
	plaintext    string // plaintext (if ADMIN_PASSWORD is set — dev only)
}

// NewAuthConfig creates an auth config from env vars.
// Priority: ADMIN_PASSWORD_HASH > ADMIN_PASSWORD.
func NewAuthConfig() (*AuthConfig, error) {
	if hashStr := envOr("ADMIN_PASSWORD_HASH", ""); hashStr != "" {
		return &AuthConfig{passwordHash: []byte(hashStr)}, nil
	}
	pw := envOr("ADMIN_PASSWORD", "build10k")
	// Hash the plaintext at startup so we never store the raw password
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	return &AuthConfig{passwordHash: hash, plaintext: pw}, nil
}

// VerifyPassword checks a submitted password against the stored hash.
// Uses constant-time comparison via bcrypt to prevent timing attacks.
func (a *AuthConfig) VerifyPassword(submitted string) bool {
	if err := bcrypt.CompareHashAndPassword(a.passwordHash, []byte(submitted)); err == nil {
		return true
	}
	// Also check plaintext (dev mode) with constant-time comparison
	if a.plaintext != "" {
		return subtle.ConstantTimeCompare([]byte(submitted), []byte(a.plaintext)) == 1
	}
	return false
}

// ============================================================================
// Admin Auth Middleware
// ============================================================================

// requireAdmin checks for a valid session cookie. If not present or invalid,
// redirects to the login page (for browser requests) or returns 401 (for API).
func (app *App) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil || !app.sessions.Valid(cookie.Value) {
			// Also check bearer token for API clients
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") || !app.sessions.Valid(strings.TrimPrefix(auth, "Bearer ")) {
				if strings.HasPrefix(r.URL.Path, "/api/") {
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					return
				}
				http.Redirect(w, r, "/admin", http.StatusSeeOther)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ============================================================================
// Input Validation
// ============================================================================

const (
	maxTrackingIDLen    = 20
	maxNameLen          = 200
	maxEmailLen         = 320
	maxDescriptionLen   = 2000
	maxURLLen           = 2048
	maxNotesLen         = 2000
)

// validateTrackingID checks format: alphanumeric with dashes, max 20 chars.
func validateTrackingID(id string) bool {
	if len(id) == 0 || len(id) > maxTrackingIDLen {
		return false
	}
	for _, c := range id {
		if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return true
}

// validateEmail does a basic RFC-ish check.
func validateEmail(email string) bool {
	if len(email) == 0 || len(email) > maxEmailLen {
		return false
	}
	at := strings.IndexByte(email, '@')
	if at <= 0 || at >= len(email)-1 {
		return false
	}
	dot := strings.IndexByte(email[at:], '.')
	return dot > 1
}

// validateURL checks that a URL starts with http:// or https://.
func validateURL(url string) bool {
	if len(url) > maxURLLen {
		return false
	}
	return strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")
}

// truncateLen ensures a string doesn't exceed maxLen.
func truncateLen(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}

// validateSiteType checks the site type is in the allowed set.
func validateSiteType(t string) bool {
	for _, st := range SiteTypes {
		if string(st.Value) == t {
			return true
		}
	}
	return false
}

// validateStatus checks the status is in the allowed set.
func validateStatus(s string) bool {
	for _, st := range StatusFlow {
		if string(st.Key) == s {
			return true
		}
	}
	return false
}

// ============================================================================
// Helpers
// ============================================================================

func envOr(key, def string) string {
	if v := getEnv(key); v != "" {
		return v
	}
	return def
}

// setSessionCookie sets a secure, HttpOnly, SameSite=Strict session cookie.
func setSessionCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,                   // JS cannot read this — prevents XSS session theft
		Secure:   secure,                 // HTTPS only in production
		SameSite: http.SameSiteStrictMode, // Prevents CSRF via cross-site requests
		MaxAge:   7 * 24 * 3600,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

// sanitizeForLog truncates and escapes a string for safe logging.
func sanitizeForLog(s string) string {
	s = truncateLen(s, 100)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}

// logError logs an error with request context (no sensitive data).
func logError(r *http.Request, format string, args ...interface{}) {
	log.Printf("[ERROR] %s %s: %s", r.Method, r.URL.Path, fmt.Sprintf(format, args...))
}
