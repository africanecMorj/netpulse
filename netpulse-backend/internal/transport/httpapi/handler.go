package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/netpulse/netpulse-backend/internal/domain"
)

type Application interface {
	CreateNode(context.Context, domain.CreateNodeInput) (domain.Node, error)
	ListNodes(context.Context) ([]domain.Node, error)
	GetNode(context.Context, uuid.UUID) (domain.Node, error)
	DeleteNode(context.Context, uuid.UUID) error
	ListSamples(context.Context, uuid.UUID, int) ([]domain.MetricSample, error)
	ListIncidents(context.Context, string, int) ([]domain.Incident, error)
}
type HealthChecker interface{ Ping(context.Context) error }
type Handler struct {
	app      Application
	health   HealthChecker
	logger   *slog.Logger
	security SecurityStore
	tokens   tokenManager
}

func New(app Application, health HealthChecker, store SecurityStore, tokens tokenManager, logger *slog.Logger) *Handler {
	return &Handler{app: app, health: health, security: store, tokens: tokens, logger: logger}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /readyz", h.readyz)
	mux.HandleFunc("POST /api/v1/auth/register", h.register)
	mux.HandleFunc("POST /api/v1/auth/login", h.login)
	mux.HandleFunc("POST /api/v1/auth/refresh", h.refresh)
	mux.HandleFunc("POST /api/v1/auth/logout", h.logout)
	mux.Handle("GET /api/v1/nodes", h.requireAuth(http.HandlerFunc(h.nodes)))
	mux.Handle("POST /api/v1/nodes", h.requireAuth(http.HandlerFunc(h.createNode)))
	mux.Handle("GET /api/v1/nodes/{id}", h.requireAuth(http.HandlerFunc(h.getNode)))
	mux.Handle("DELETE /api/v1/nodes/{id}", h.requireAuth(http.HandlerFunc(h.deleteNode)))
	mux.Handle("GET /api/v1/nodes/{id}/metrics", h.requireAuth(http.HandlerFunc(h.nodeMetrics)))
	mux.Handle("GET /api/v1/incidents", h.requireAuth(http.HandlerFunc(h.incidents)))
	mux.Handle("POST /api/v1/agents", h.requireAuth(http.HandlerFunc(h.createAgent)))
	mux.HandleFunc("POST /api/v1/agents/enroll", h.enrollAgent)
	mux.Handle("GET /api/v1/agents", h.requireAuth(http.HandlerFunc(h.agents)))
	mux.Handle("GET /api/v1/agents/{id}/metrics", h.requireAuth(http.HandlerFunc(h.agentMetrics)))
	mux.Handle("DELETE /api/v1/agents/{id}", h.requireAuth(http.HandlerFunc(h.revokeAgent)))
	mux.HandleFunc("GET /api/v1/agents/ws", h.agentWebSocket)
	mux.Handle("GET /api/v1/dev/test-server", h.requireAuth(h.requireTestControl(http.HandlerFunc(h.testServerStatus))))
	mux.Handle("POST /api/v1/dev/test-server/start", h.requireAuth(h.requireTestControl(http.HandlerFunc(h.startTestServer))))
	mux.Handle("POST /api/v1/dev/test-server/stress", h.requireAuth(h.requireTestControl(http.HandlerFunc(h.stressTestServer))))
	mux.Handle("POST /api/v1/dev/test-server/stop-stress", h.requireAuth(h.requireTestControl(http.HandlerFunc(h.stopTestStress))))
	mux.Handle("POST /api/v1/dev/test-server/stop", h.requireAuth(h.requireTestControl(http.HandlerFunc(h.stopTestServer))))
	return h.recoverPanic(h.requestLog(h.cors(mux)))
}

func (h *Handler) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
func (h *Handler) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := h.health.Ping(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *Handler) nodes(w http.ResponseWriter, r *http.Request) {
	items, err := h.app.ListNodes(r.Context())
	if err != nil {
		h.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "count": len(items)})
}
func (h *Handler) createNode(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var input domain.CreateNodeInput
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, "request_body_must_contain_one_json_object")
		return
	}
	node, err := h.app.CreateNode(r.Context(), input)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": node})
}
func (h *Handler) getNode(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	n, err := h.app.GetNode(r.Context(), id)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": n})
}
func (h *Handler) deleteNode(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := h.app.DeleteNode(r.Context(), id); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) nodeMetrics(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 1000 {
			writeError(w, http.StatusBadRequest, "limit_must_be_between_1_and_1000")
			return
		}
		limit = n
	}
	if _, err := h.app.GetNode(r.Context(), id); err != nil {
		h.handleError(w, err)
		return
	}
	items, err := h.app.ListSamples(r.Context(), id, limit)
	if err != nil {
		h.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "count": len(items)})
}
func (h *Handler) incidents(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status != "" && status != "open" && status != "resolved" {
		writeError(w, http.StatusBadRequest, "status_must_be_open_or_resolved")
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 500 {
			writeError(w, http.StatusBadRequest, "limit_must_be_between_1_and_500")
			return
		}
		limit = n
	}
	items, err := h.app.ListIncidents(r.Context(), status, limit)
	if err != nil {
		h.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "count": len(items)})
}

func parseID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id")
		return uuid.Nil, false
	}
	return id, true
}
func (h *Handler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, domain.ErrInvalidInput):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		h.internalError(w, err)
	}
}
func (h *Handler) internalError(w http.ResponseWriter, err error) {
	h.logger.Error("request failed", "error", err)
	writeError(w, http.StatusInternalServerError, "internal_server_error")
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
func (h *Handler) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		h.logger.Info("http request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start).String())
	})
}
func (h *Handler) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				h.logger.Error("panic recovered", "value", v)
				writeError(w, http.StatusInternalServerError, "internal_server_error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
func (h *Handler) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (strings.HasPrefix(origin, "http://localhost:") || strings.HasPrefix(origin, "http://127.0.0.1:")) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
