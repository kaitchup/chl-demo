package auth

import (
	"errors"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Issue returns a signed HS256 token whose subject is the user id.
func Issue(secret string, userID int64, ttl time.Duration) (string, time.Time, error) {
	exp := time.Now().Add(ttl)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": strconv.FormatInt(userID, 10),
		"exp": exp.Unix(),
		"iat": time.Now().Unix(),
	})
	s, err := token.SignedString([]byte(secret))
	return s, exp, err
}

// Parse validates the token and returns the user id from its subject.
func Parse(secret, tokenStr string) (int64, error) {
	tok, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil || !tok.Valid {
		return 0, errors.New("invalid token")
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return 0, errors.New("invalid claims")
	}
	sub, _ := claims["sub"].(string)
	id, err := strconv.ParseInt(sub, 10, 64)
	if err != nil {
		return 0, errors.New("invalid subject")
	}
	return id, nil
}
