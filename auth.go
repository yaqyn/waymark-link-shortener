package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const UserContextKey contextKey = "user"
const sessionCookie = "waymark_session"
const demoHash = "$2a$10$B6O/n6teuCzpuh66jrUAdeaJ3WvXcxRkzpN0x7H.di9G9e/NGb9Me"

type session struct {
	Username string
	Expires  time.Time
}
type authState struct {
	mu           sync.Mutex
	sessions     map[[32]byte]session
	window       time.Time
	attempts     int
	username     string
	passwordHash string
	secure       bool
	now          func() time.Time
}

func newAuth(username, hash string, secure bool) *authState {
	return &authState{sessions: make(map[[32]byte]session), username: username, passwordHash: hash, secure: secure, now: time.Now}
}
func (a *authState) allowLogin() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	if now.Sub(a.window) >= time.Minute {
		a.window = now
		a.attempts = 0
	}
	a.attempts++
	return a.attempts <= 30
}
func (a *authState) authenticate(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err == nil {
		key := sha256.Sum256([]byte(cookie.Value))
		a.mu.Lock()
		ss, ok := a.sessions[key]
		if ok && !ss.Expires.After(time.Now()) {
			delete(a.sessions, key)
			ok = false
		}
		a.mu.Unlock()
		if ok {
			return ss.Username, true
		}
	}
	return "", false
}
func (s *server) validatePassword(ctx context.Context, password, stored string) (bool, error) {
	_, span := tracer.Start(ctx, "auth.validate_password")
	defer span.End()
	err := bcrypt.CompareHashAndPassword([]byte(stored), []byte(password))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return false, nil
	}
	return err == nil, err
}
func (s *server) checkCredentials(ctx context.Context, user, password string) (bool, error) {
	if len(password) == 0 || len(password) > 72 {
		return false, nil
	}
	// Compare the password even for an unknown username to avoid a fast user oracle.
	ok, err := s.validatePassword(ctx, password, s.auth.passwordHash)
	return ok && subtle.ConstantTimeCompare([]byte(user), []byte(s.auth.username)) == 1, err
}
func (s *server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.auth.authenticate(r)
		if !ok {
			username, password, basic := r.BasicAuth()
			if basic {
				if !s.auth.allowLogin() {
					w.Header().Set("Retry-After", "60")
					httpError(r.Context(), w, 429, errors.New("too many login attempts; try again in a minute"))
					return
				}
				var err error
				ok, err = s.checkCredentials(r.Context(), username, password)
				if err != nil {
					httpError(r.Context(), w, 500, err)
					return
				}
				if ok {
					user = username
				}
			}
		}
		if !ok {
			httpError(r.Context(), w, 401, errors.New("unauthorized"))
			return
		}
		if logCtx, _ := r.Context().Value(LogContextKey).(*LogContext); logCtx != nil {
			logCtx.Username = user
		}
		r = r.WithContext(context.WithValue(r.Context(), UserContextKey, user))
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (s *server) handlerLogin(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "handler.login")
	defer span.End()
	if !s.auth.allowLogin() {
		w.Header().Set("Retry-After", "60")
		httpError(ctx, w, 429, errors.New("too many login attempts; try again in a minute"))
		return
	}
	user, password, basic := r.BasicAuth()
	if !basic {
		var input struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := decodeJSON(w, r, &input); err != nil {
			httpError(ctx, w, 400, errors.New("enter a username and password"))
			return
		}
		user, password = input.Username, input.Password
	}
	ok, err := s.checkCredentials(ctx, user, password)
	if err != nil {
		httpError(ctx, w, 500, err)
		return
	}
	if !ok {
		httpError(ctx, w, 401, errors.New("unauthorized"))
		return
	}
	if lc, _ := ctx.Value(LogContextKey).(*LogContext); lc != nil {
		lc.Username = user
	}
	token := rand.Text() + rand.Text()
	key := sha256.Sum256([]byte(token))
	expires := time.Now().Add(12 * time.Hour)
	s.auth.mu.Lock()
	if cookie, e := r.Cookie(sessionCookie); e == nil {
		delete(s.auth.sessions, sha256.Sum256([]byte(cookie.Value)))
	}
	for k, v := range s.auth.sessions {
		if !v.Expires.After(time.Now()) {
			delete(s.auth.sessions, k)
		}
	}
	if len(s.auth.sessions) >= 1000 {
		s.auth.mu.Unlock()
		httpError(ctx, w, 503, errors.New("session capacity reached"))
		return
	}
	s.auth.sessions[key] = session{Username: user, Expires: expires}
	s.auth.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: s.auth.secure, SameSite: http.SameSiteStrictMode, MaxAge: 12 * 60 * 60, Expires: expires})
	writeJSON(w, 200, map[string]string{"username": user})
}
func (s *server) handlerLogout(w http.ResponseWriter, r *http.Request) {
	_, span := tracer.Start(r.Context(), "handler.logout")
	defer span.End()
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		s.auth.mu.Lock()
		delete(s.auth.sessions, sha256.Sum256([]byte(cookie.Value)))
		s.auth.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: s.auth.secure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}
func (s *server) handlerSession(w http.ResponseWriter, r *http.Request) {
	_, span := tracer.Start(r.Context(), "handler.session")
	defer span.End()
	writeJSON(w, 200, map[string]string{"username": r.Context().Value(UserContextKey).(string)})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("expected one JSON object")
	}
	return nil
}

// Reject cross-site mutations even on a local deployment using demo credentials.
func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			origin := r.Header.Get("Origin")
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" || origin != "" && !sameOriginHost(origin, r.Host) {
				httpError(r.Context(), w, 403, errors.New("cross-site request rejected"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func sameOriginHost(origin, host string) bool {
	u, err := url.Parse(origin)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && strings.EqualFold(u.Host, host)
}
