package admin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// MerchantAuth verifies merchant-admin JWTs by calling the profile endpoint.
type MerchantAuth struct {
	baseURL string
	http    *http.Client
}

func NewMerchantAuth(baseURL string) *MerchantAuth {
	return &MerchantAuth{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

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

// VerifyToken calls GET /auth/profile with the provided bearer token.
// Returns the merchant profile on success, or an error if the token is invalid.
func (m *MerchantAuth) VerifyToken(token string) (*MerchantProfile, error) {
	req, _ := http.NewRequest(http.MethodGet, m.baseURL+"/auth/profile", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := m.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("merchant auth: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("invalid merchant token")
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("merchant auth %d: %s", resp.StatusCode, raw)
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
