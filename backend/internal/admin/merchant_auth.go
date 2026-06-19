package admin

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

type MerchantProfile struct {
	MerchantID   string
	MerchantName string
	UserID       string
	Email        string
}

type profileResp struct {
	User struct {
		ID           string `json:"id"`
		Email        string `json:"email"`
		MerchantID   string `json:"merchant_id"`
		MerchantName string `json:"merchant_name"`
	} `json:"user"`
}

// merchantSession holds live credentials and a cached access token for one
// logged-in merchant user. Refreshes automatically; falls back to re-login.
type merchantSession struct {
	mu            sync.Mutex
	baseURL       string
	email         string
	password      string
	profile       *MerchantProfile
	accessToken   string
	tokenExpiry   time.Time
	refreshCookie string
	http          *http.Client
}

func (s *merchantSession) getToken() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Until(s.tokenExpiry) > 5*time.Minute {
		return s.accessToken, nil
	}
	return s.refresh()
}

func (s *merchantSession) refresh() (string, error) {
	if s.refreshCookie != "" {
		if tok, err := s.callRefresh(); err == nil {
			return tok, nil
		}
	}
	return s.login()
}

func (s *merchantSession) callRefresh() (string, error) {
	req, _ := http.NewRequest(http.MethodPost, s.baseURL+"/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "upay_refresh", Value: s.refreshCookie})
	resp, err := s.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("refresh %d", resp.StatusCode)
	}
	var lr loginResp
	if err := json.Unmarshal(raw, &lr); err != nil {
		return "", err
	}
	exp, _ := time.Parse(time.RFC3339Nano, lr.AccessExpiresAt)
	s.accessToken = lr.AccessToken
	s.tokenExpiry = exp
	return lr.AccessToken, nil
}

func (s *merchantSession) login() (string, error) {
	body, _ := json.Marshal(loginReq{Email: s.email, Password: s.password, Audience: "merchant"})
	req, _ := http.NewRequest(http.MethodPost, s.baseURL+"/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("merchant login: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("merchant login %d: %s", resp.StatusCode, raw)
	}
	var lr loginResp
	if err := json.Unmarshal(raw, &lr); err != nil {
		return "", fmt.Errorf("decode merchant login: %w", err)
	}
	exp, _ := time.Parse(time.RFC3339Nano, lr.AccessExpiresAt)
	s.accessToken = lr.AccessToken
	s.tokenExpiry = exp
	for _, ck := range resp.Cookies() {
		if ck.Name == "upay_refresh" {
			s.refreshCookie = ck.Value
			break
		}
	}
	return lr.AccessToken, nil
}

// MerchantAuth manages per-user sandbox sessions. Login returns an opaque
// session key; subsequent calls pass the key to look up the session and get
// a fresh merchant-admin token without re-prompting for credentials.
type MerchantAuth struct {
	mu       sync.Mutex
	baseURL  string
	http     *http.Client
	sessions map[string]*merchantSession
}

func NewMerchantAuth(baseURL string) *MerchantAuth {
	return &MerchantAuth{
		baseURL:  baseURL,
		http:     &http.Client{Timeout: 10 * time.Second},
		sessions: make(map[string]*merchantSession),
	}
}

// Login authenticates with merchant-admin and returns an opaque session key
// plus the merchant profile. The session auto-refreshes its token internally.
func (m *MerchantAuth) Login(email, password string) (string, *MerchantProfile, error) {
	sess := &merchantSession{
		baseURL:  m.baseURL,
		email:    email,
		password: password,
		http:     m.http,
	}
	tok, err := sess.login()
	if err != nil {
		return "", nil, err
	}
	profile, err := m.fetchProfile(tok)
	if err != nil {
		return "", nil, err
	}
	sess.profile = profile

	key := newSessionKey()
	m.mu.Lock()
	m.sessions[key] = sess
	m.mu.Unlock()

	return key, profile, nil
}

// GetProfile returns the cached profile for the given session key.
func (m *MerchantAuth) GetProfile(sessionKey string) (*MerchantProfile, bool) {
	m.mu.Lock()
	sess, ok := m.sessions[sessionKey]
	m.mu.Unlock()
	if !ok {
		return nil, false
	}
	return sess.profile, true
}

func (m *MerchantAuth) fetchProfile(token string) (*MerchantProfile, error) {
	req, _ := http.NewRequest(http.MethodGet, m.baseURL+"/auth/profile", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch profile: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("fetch profile %d: %s", resp.StatusCode, raw)
	}
	var pr profileResp
	if err := json.Unmarshal(raw, &pr); err != nil {
		return nil, fmt.Errorf("decode profile: %w", err)
	}
	if pr.User.MerchantID == "" {
		return nil, fmt.Errorf("merchant_id missing in profile")
	}
	return &MerchantProfile{
		MerchantID:   pr.User.MerchantID,
		MerchantName: pr.User.MerchantName,
		UserID:       pr.User.ID,
		Email:        pr.User.Email,
	}, nil
}

func newSessionKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
