package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/netpulse/netpulse-backend/internal/domain"
	"github.com/netpulse/netpulse-backend/internal/repository/postgres"
	"github.com/netpulse/netpulse-backend/internal/security"
)

type SecurityStore interface {
	CreateUser(context.Context, string, string) (domain.User, error)
	UserByEmail(context.Context, string) (postgres.AuthUser, error)
	CreateRefreshToken(context.Context, uuid.UUID, string, time.Time) error
	RotateRefreshToken(context.Context, string, string, time.Time) (uuid.UUID, error)
	RevokeRefreshToken(context.Context, string) error
	CreateAgentEnrollment(context.Context, uuid.UUID, string, string, time.Time) (domain.Agent, error)
	ConsumeEnrollment(context.Context, string, string, string) (uuid.UUID, error)
	AuthenticateAgent(context.Context, string) (uuid.UUID, error)
	SaveAgentMetrics(context.Context, uuid.UUID, domain.AgentMetrics) error
	ListAgents(context.Context, uuid.UUID) ([]domain.Agent, error)
	ListAgentMetrics(context.Context, uuid.UUID, uuid.UUID, int) ([]domain.AgentMetrics, error)
	RevokeAgent(context.Context, uuid.UUID, uuid.UUID) error
	TouchAgent(context.Context, uuid.UUID, string) error
}

type tokenManager interface {
	NewAccessToken(uuid.UUID) (string, time.Time, error)
	VerifyAccessToken(string) (uuid.UUID, error)
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in, 4096) {
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	addr, err := mail.ParseAddress(in.Email)
	if err != nil || addr.Address != in.Email || len(in.Email) > 254 {
		writeError(w, 400, "invalid_email")
		return
	}
	hash, err := security.HashPassword(in.Password)
	if err != nil {
		writeError(w, 400, "password_must_be_12_to_1024_bytes")
		return
	}
	u, err := h.security.CreateUser(r.Context(), in.Email, hash)
	if errors.Is(err, postgres.ErrEmailTaken) {
		writeError(w, 409, "email_already_registered")
		return
	}
	if err != nil {
		h.internalError(w, err)
		return
	}
	h.issueSession(w, r, u)
}
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in, 4096) {
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	u, err := h.security.UserByEmail(r.Context(), in.Email)
	if err != nil || !security.VerifyPassword(u.PasswordHash, in.Password) {
		writeError(w, http.StatusUnauthorized, "invalid_email_or_password")
		return
	}
	h.issueSession(w, r, domain.User{ID: u.ID, Email: u.Email, CreatedAt: u.CreatedAt})
}
func (h *Handler) issueSession(w http.ResponseWriter, r *http.Request, u domain.User) {
	access, accessExp, err := h.tokens.NewAccessToken(u.ID)
	if err != nil {
		h.internalError(w, err)
		return
	}
	refresh, err := security.RandomToken(32)
	if err != nil {
		h.internalError(w, err)
		return
	}
	if err := h.security.CreateRefreshToken(r.Context(), u.ID, security.HashToken(refresh), time.Now().UTC().Add(30*24*time.Hour)); err != nil {
		h.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u, "access_token": access, "token_type": "Bearer", "access_expires_at": accessExp, "refresh_token": refresh, "refresh_expires_in": int((30 * 24 * time.Hour).Seconds())})
}
func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RefreshToken string `json:"refresh_token"`
	}
	if !decodeJSON(w, r, &in, 4096) || in.RefreshToken == "" {
		writeError(w, 400, "refresh_token_required")
		return
	}
	newRefresh, err := security.RandomToken(32)
	if err != nil {
		h.internalError(w, err)
		return
	}
	userID, err := h.security.RotateRefreshToken(r.Context(), security.HashToken(in.RefreshToken), security.HashToken(newRefresh), time.Now().UTC().Add(30*24*time.Hour))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_or_expired_refresh_token")
		return
	}
	access, exp, err := h.tokens.NewAccessToken(userID)
	if err != nil {
		h.internalError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"access_token": access, "token_type": "Bearer", "access_expires_at": exp, "refresh_token": newRefresh, "refresh_expires_in": int((30 * 24 * time.Hour).Seconds())})
}
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RefreshToken string `json:"refresh_token"`
	}
	if !decodeJSON(w, r, &in, 4096) || in.RefreshToken == "" {
		writeError(w, 400, "refresh_token_required")
		return
	}
	if err := h.security.RevokeRefreshToken(r.Context(), security.HashToken(in.RefreshToken)); err != nil {
		h.internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "bearer_token_required")
			return
		}
		id, err := h.tokens.VerifyAccessToken(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid_or_expired_access_token")
			return
		}
		next.ServeHTTP(w, r.WithContext(security.WithUserID(r.Context(), id)))
	})
}
func decodeJSON(w http.ResponseWriter, r *http.Request, out any, maxBytes int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, "request_body_must_contain_one_json_object")
		return false
	}
	return true
}
