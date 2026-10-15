package httpapi

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/netpulse/netpulse-backend/internal/domain"
	"github.com/netpulse/netpulse-backend/internal/security"
)

func (h *Handler) createAgent(w http.ResponseWriter, r *http.Request) {
	userID, ok := security.UserID(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized")
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &in, 4096) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if len(in.Name) < 1 || len(in.Name) > 120 {
		writeError(w, 400, "name_must_be_1_to_120_characters")
		return
	}
	token, err := security.RandomToken(32)
	if err != nil {
		h.internalError(w, err)
		return
	}
	a, err := h.security.CreateAgentEnrollment(r.Context(), userID, in.Name, security.HashToken(token), time.Now().UTC().Add(15*time.Minute))
	if err != nil {
		h.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"agent": a, "enrollment_token": token, "expires_at": time.Now().UTC().Add(15 * time.Minute), "instructions": "Exchange this token once at POST /api/v1/agents/enroll. The token will not be shown again."})
}
func (h *Handler) enrollAgent(w http.ResponseWriter, r *http.Request) {
	var in struct {
		EnrollmentToken string `json:"enrollment_token"`
		Hostname        string `json:"hostname"`
	}
	if !decodeJSON(w, r, &in, 4096) {
		return
	}
	if in.EnrollmentToken == "" {
		writeError(w, 400, "enrollment_token_required")
		return
	}
	if len(in.Hostname) > 255 {
		writeError(w, 400, "hostname_too_long")
		return
	}
	secret, err := security.RandomToken(32)
	if err != nil {
		h.internalError(w, err)
		return
	}
	agentID, err := h.security.ConsumeEnrollment(r.Context(), security.HashToken(in.EnrollmentToken), security.HashToken(secret), strings.TrimSpace(in.Hostname))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_or_expired_enrollment_token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"agent_id": agentID, "agent_secret": secret, "websocket_url": "/api/v1/agents/ws", "instructions": "Store agent_secret securely; it is shown only once. Send it as Authorization: Bearer <agent_secret> when opening the WebSocket."})
}
func (h *Handler) agents(w http.ResponseWriter, r *http.Request) {
	userID, ok := security.UserID(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized")
		return
	}
	items, err := h.security.ListAgents(r.Context(), userID)
	if err != nil {
		h.internalError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items, "count": len(items)})
}
func (h *Handler) agentMetrics(w http.ResponseWriter, r *http.Request) {
	userID, ok := security.UserID(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized")
		return
	}
	agentID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, 400, "invalid_id")
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, parseErr := strconv.Atoi(raw)
		if parseErr != nil || n < 1 || n > 1000 {
			writeError(w, 400, "limit_must_be_between_1_and_1000")
			return
		}
		limit = n
	}
	items, err := h.security.ListAgentMetrics(r.Context(), userID, agentID, limit)
	if err != nil {
		h.internalError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items, "count": len(items)})
}
func (h *Handler) revokeAgent(w http.ResponseWriter, r *http.Request) {
	userID, ok := security.UserID(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized")
		return
	}
	agentID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, 400, "invalid_id")
		return
	}
	if err := h.security.RevokeAgent(r.Context(), userID, agentID); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) agentWebSocket(w http.ResponseWriter, r *http.Request) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		writeError(w, 401, "agent_bearer_token_required")
		return
	}
	agentID, err := h.security.AuthenticateAgent(r.Context(), security.HashToken(strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))))
	if err != nil {
		writeError(w, 401, "invalid_agent_secret")
		return
	}
	if err := serveAgentWebSocket(w, r, func(payload []byte) error {
		var envelope struct {
			Type      string    `json:"type"`
			Timestamp time.Time `json:"timestamp"`
			Hostname  string    `json:"hostname"`
			Data      struct {
				CPU         float64 `json:"cpu_percent"`
				MemoryUsed  int64   `json:"memory_used_bytes"`
				MemoryTotal int64   `json:"memory_total_bytes"`
				DiskUsed    int64   `json:"disk_used_bytes"`
				DiskTotal   int64   `json:"disk_total_bytes"`
				RX          int64   `json:"network_rx_bytes_per_sec"`
				TX          int64   `json:"network_tx_bytes_per_sec"`
			} `json:"data"`
		}
		if err := json.Unmarshal(payload, &envelope); err != nil {
			return errors.New("invalid_json")
		}
		switch envelope.Type {
		case "heartbeat":
			return h.security.TouchAgent(r.Context(), agentID, strings.TrimSpace(envelope.Hostname))
		case "metrics":
			m := domain.AgentMetrics{Type: "metrics", Timestamp: envelope.Timestamp, Hostname: strings.TrimSpace(envelope.Hostname), CPUPercent: envelope.Data.CPU, MemoryUsedBytes: envelope.Data.MemoryUsed, MemoryTotalBytes: envelope.Data.MemoryTotal, DiskUsedBytes: envelope.Data.DiskUsed, DiskTotalBytes: envelope.Data.DiskTotal, NetworkRXBytesPerSec: envelope.Data.RX, NetworkTXBytesPerSec: envelope.Data.TX}
			if m.Timestamp.IsZero() {
				m.Timestamp = time.Now().UTC()
			}
			if m.Timestamp.Before(time.Now().Add(-5*time.Minute)) || m.Timestamp.After(time.Now().Add(time.Minute)) {
				return errors.New("timestamp_out_of_range")
			}
			if math.IsNaN(m.CPUPercent) || math.IsInf(m.CPUPercent, 0) || m.CPUPercent < 0 || m.CPUPercent > 100 || m.MemoryUsedBytes < 0 || m.MemoryTotalBytes < 0 || m.DiskUsedBytes < 0 || m.DiskTotalBytes < 0 || m.NetworkRXBytesPerSec < 0 || m.NetworkTXBytesPerSec < 0 || m.MemoryUsedBytes > m.MemoryTotalBytes && m.MemoryTotalBytes > 0 || m.DiskUsedBytes > m.DiskTotalBytes && m.DiskTotalBytes > 0 {
				return errors.New("invalid_metric_values")
			}
			return h.security.SaveAgentMetrics(r.Context(), agentID, m)
		default:
			return errors.New("unsupported_message_type")
		}
	}); err != nil {
		h.logger.Debug("agent websocket closed", "agent_id", agentID, "error", err)
	}
}
