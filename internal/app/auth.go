package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{3,32}$`)

type loginAttempt struct {
	count        int       // failures within the current window
	windowStart  time.Time // start of the current counting window
	blockedUntil time.Time // zero when not banned
	bans         int       // bans served; drives ban escalation
}

const (
	loginMaxAttempts   = 5
	loginAttemptWindow = 10 * time.Minute // quiet IP resets its failure count
	loginMaxBan        = 24 * time.Hour
	loginFailMapCap    = 1024
)

// loginBanDuration escalates per ban: 1m, 2m, 4m, … capped at 24h.

// loginBanDuration escalates per ban: 1m, 2m, 4m, … capped at 24h.
func loginBanDuration(bans int) time.Duration {
	d := time.Minute
	for i := 1; i < bans; i++ {
		d *= 2
		if d >= loginMaxBan {
			return loginMaxBan
		}
	}
	return d
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	if a.loginBlocked(r) {
		http.Error(w, "登录失败次数过多，请稍后再试", http.StatusTooManyRequests)
		return
	}
	if subtle.ConstantTimeCompare([]byte(body.Username), []byte(a.runtime.AdminUser)) != 1 || subtle.ConstantTimeCompare([]byte(body.Password), []byte(a.runtime.AdminPassword)) != 1 {
		a.noteLoginFailure(r)
		http.Error(w, "账号或密码错误", http.StatusUnauthorized)
		return
	}
	a.clearLoginFailures(r)
	value := a.signSession(time.Now().Add(30 * 24 * time.Hour))
	http.SetCookie(w, &http.Cookie{Name: "kotaui_session", Value: value, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil, MaxAge: 30 * 24 * 60 * 60})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) logout(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "kotaui_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func loginClientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (a *App) loginBlocked(r *http.Request) bool {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	attempt, exists := a.loginFails[loginClientKey(r)]
	return exists && time.Now().Before(attempt.blockedUntil)
}

func (a *App) noteLoginFailure(r *http.Request) {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	key := loginClientKey(r)
	now := time.Now()
	attempt := a.loginFails[key]
	if now.Sub(attempt.windowStart) > loginAttemptWindow {
		attempt.count = 0
		attempt.windowStart = now
	}
	attempt.count++
	if attempt.count >= loginMaxAttempts {
		attempt.bans++
		attempt.blockedUntil = now.Add(loginBanDuration(attempt.bans))
		attempt.count = 0
		attempt.windowStart = now
	}
	a.loginFails[key] = attempt
	a.sweepLoginFailsLocked(now)
}

// sweepLoginFailsLocked drops stale entries so a distributed attack can't
// grow the map without bound. Callers must hold loginMu.

// sweepLoginFailsLocked drops stale entries so a distributed attack can't
// grow the map without bound. Callers must hold loginMu.
func (a *App) sweepLoginFailsLocked(now time.Time) {
	if len(a.loginFails) <= loginFailMapCap {
		return
	}
	for key, attempt := range a.loginFails {
		if now.After(attempt.blockedUntil) && now.Sub(attempt.windowStart) > loginAttemptWindow {
			delete(a.loginFails, key)
		}
	}
	for len(a.loginFails) > loginFailMapCap {
		oldestKey := ""
		var oldest time.Time
		first := true
		for key, attempt := range a.loginFails {
			if first || attempt.windowStart.Before(oldest) {
				oldestKey, oldest, first = key, attempt.windowStart, false
			}
		}
		delete(a.loginFails, oldestKey)
	}
}

func (a *App) clearLoginFailures(r *http.Request) {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	delete(a.loginFails, loginClientKey(r))
}

func (a *App) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, e := r.Cookie("kotaui_session")
		if e != nil || !a.verifySession(c.Value) {
			http.Error(w, "请先登录", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (a *App) signSession(expiry time.Time) string {
	payload := strconv.FormatInt(expiry.Unix(), 10)
	mac := hmac.New(sha256.New, a.key)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload + ":" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))))
}

func (a *App) verifySession(value string) bool {
	raw, e := base64.RawURLEncoding.DecodeString(value)
	if e != nil {
		return false
	}
	parts := strings.Split(string(raw), ":")
	if len(parts) != 2 {
		return false
	}
	stamp, e := strconv.ParseInt(parts[0], 10, 64)
	if e != nil || time.Now().Unix() > stamp {
		return false
	}
	expected := a.signSession(time.Unix(stamp, 0))
	return hmac.Equal([]byte(value), []byte(expected))
}
