package gd

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

const sessionCookie = "gdsession"
const sessionTTL = 24 * time.Hour

type Session struct {
	ID      string
	Values  map[string]any
	Expires time.Time
}

func (s *Session) Get(key string) any {
	if s == nil {
		return nil
	}
	return s.Values[key]
}

func (s *Session) Set(key string, v any) {
	if s.Values == nil {
		s.Values = make(map[string]any)
	}
	s.Values[key] = v
}

func (s *Session) Delete(key string) {
	delete(s.Values, key)
}

type SessionStore struct {
	secret   []byte
	mu       sync.Mutex
	sessions map[string]*Session
}

func NewSessionStore(secret string) *SessionStore {
	return &SessionStore{
		secret:   []byte(secret),
		sessions: make(map[string]*Session),
	}
}

func (st *SessionStore) New() *Session {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic("gd: crypto/rand unavailable: " + err.Error())
	}
	s := &Session{
		ID:      hex.EncodeToString(buf),
		Values:  make(map[string]any),
		Expires: time.Now().Add(sessionTTL),
	}
	st.mu.Lock()
	st.gcLocked()
	st.sessions[s.ID] = s
	st.mu.Unlock()
	return s
}

func (st *SessionStore) Get(r *http.Request) *Session {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return nil
	}
	id, sig, ok := splitCookieValue(cookie.Value)
	if !ok || !st.validSignature(id, sig) {
		return nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	s, exists := st.sessions[id]
	if !exists || time.Now().After(s.Expires) {
		return nil
	}
	return s
}

func (st *SessionStore) Save(w http.ResponseWriter, s *Session) {
	st.mu.Lock()
	st.sessions[s.ID] = s
	st.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    s.ID + "." + st.sign(s.ID),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (st *SessionStore) Destroy(w http.ResponseWriter, s *Session) {
	if s != nil {
		st.mu.Lock()
		delete(st.sessions, s.ID)
		st.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (st *SessionStore) gcLocked() {
	now := time.Now()
	for id, s := range st.sessions {
		if now.After(s.Expires) {
			delete(st.sessions, id)
		}
	}
}

func (st *SessionStore) sign(id string) string {
	mac := hmac.New(sha256.New, st.secret)
	mac.Write([]byte(id))
	return hex.EncodeToString(mac.Sum(nil))
}

func (st *SessionStore) validSignature(id, sig string) bool {
	want := st.sign(id)
	return hmac.Equal([]byte(want), []byte(sig))
}

func splitCookieValue(v string) (id, sig string, ok bool) {
	for i := len(v) - 1; i >= 0; i-- {
		if v[i] == '.' {
			return v[:i], v[i+1:], true
		}
	}
	return "", "", false
}
