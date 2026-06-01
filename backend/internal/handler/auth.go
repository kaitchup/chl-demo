package handler

import (
	"errors"
	"net/http"
	"strings"

	"chldemo/internal/auth"
	"chldemo/internal/repo"

	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
)

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) Register(c echo.Context) error {
	var in credentials
	if err := c.Bind(&in); err != nil {
		return fail(c, http.StatusBadRequest, "invalid_request", "malformed body")
	}
	in.Email = strings.TrimSpace(strings.ToLower(in.Email))
	if !strings.Contains(in.Email, "@") || len(in.Password) < 6 {
		return fail(c, http.StatusBadRequest, "validation_failed", "invalid email or password too short (min 6)")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return fail(c, http.StatusInternalServerError, "internal", "hash failed")
	}

	id, err := h.repo.CreateUser(c.Request().Context(), in.Email, string(hash))
	if errors.Is(err, repo.ErrConflict) {
		return fail(c, http.StatusConflict, "email_taken", "email already registered")
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, "internal", "create user failed")
	}

	token, exp, _ := auth.Issue(h.cfg.JWTSecret, id, h.cfg.JWTTTL)
	return ok(c, map[string]any{"user_id": id, "token": token, "expires_at": exp})
}

func (h *Handler) Login(c echo.Context) error {
	var in credentials
	if err := c.Bind(&in); err != nil {
		return fail(c, http.StatusBadRequest, "invalid_request", "malformed body")
	}
	in.Email = strings.TrimSpace(strings.ToLower(in.Email))

	u, err := h.repo.GetUserByEmail(c.Request().Context(), in.Email)
	if err != nil {
		return fail(c, http.StatusUnauthorized, "invalid_credentials", "email or password incorrect")
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Password)) != nil {
		return fail(c, http.StatusUnauthorized, "invalid_credentials", "email or password incorrect")
	}

	token, exp, _ := auth.Issue(h.cfg.JWTSecret, u.ID, h.cfg.JWTTTL)
	return ok(c, map[string]any{"token": token, "expires_at": exp})
}
