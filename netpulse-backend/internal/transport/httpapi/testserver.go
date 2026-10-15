package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func (h *Handler) testServerEnabled() bool {
	value := os.Getenv("NETPULSE_TEST_SERVER_ENABLED")
	if value == "" {
		value = os.Getenv("NETPULSE_TEST_DOCKER_ENABLED") // backward compatibility for local development
	}
	return strings.EqualFold(strings.TrimSpace(value), "true")
}

func (h *Handler) requireTestControl(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.testServerEnabled() {
			if r.Method == http.MethodGet && r.URL.Path == "/api/v1/dev/test-server" {
				next.ServeHTTP(w, r)
				return
			}
			writeError(w, http.StatusNotFound, "test_server_controls_disabled")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) testServerStatus(w http.ResponseWriter, r *http.Request) {
	if !h.testServerEnabled() {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "running": false, "stress": false})
		return
	}
	var status map[string]any
	if err := h.testControllerRequest(r.Context(), http.MethodGet, "/status", nil, &status); err != nil {
		h.logger.Error("read test server status", "error", err)
		writeError(w, http.StatusBadGateway, "test_server_controller_unavailable")
		return
	}
	status["enabled"] = true
	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) startTestServer(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AgentSecret string `json:"agent_secret"`
	}
	if !decodeJSON(w, r, &in, 4096) {
		return
	}
	if len(in.AgentSecret) < 20 || len(in.AgentSecret) > 512 {
		writeError(w, http.StatusBadRequest, "valid_agent_secret_required")
		return
	}
	var result map[string]any
	if err := h.testControllerRequest(r.Context(), http.MethodPost, "/start", in, &result); err != nil {
		h.logger.Error("start test server", "error", err)
		writeError(w, http.StatusBadGateway, "test_server_start_failed")
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (h *Handler) stressTestServer(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind string `json:"kind"`
	}
	if !decodeJSON(w, r, &in, 1024) {
		return
	}
	if in.Kind != "cpu" && in.Kind != "memory" {
		writeError(w, http.StatusBadRequest, "kind_must_be_cpu_or_memory")
		return
	}
	var result map[string]any
	if err := h.testControllerRequest(r.Context(), http.MethodPost, "/stress", in, &result); err != nil {
		h.logger.Error("start test stress", "error", err)
		if strings.Contains(err.Error(), "409") {
			writeError(w, http.StatusConflict, "test_server_not_running")
		} else {
			writeError(w, http.StatusBadGateway, "stress_start_failed")
		}
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) stopTestStress(w http.ResponseWriter, r *http.Request) {
	var result map[string]any
	if err := h.testControllerRequest(r.Context(), http.MethodPost, "/stop-stress", map[string]any{}, &result); err != nil {
		h.logger.Error("stop test stress", "error", err)
		writeError(w, http.StatusBadGateway, "stress_stop_failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) stopTestServer(w http.ResponseWriter, r *http.Request) {
	var result map[string]any
	if err := h.testControllerRequest(r.Context(), http.MethodPost, "/stop", map[string]any{}, &result); err != nil {
		h.logger.Error("stop test server", "error", err)
		writeError(w, http.StatusBadGateway, "test_server_stop_failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) testControllerRequest(ctx context.Context, method, path string, body any, out *map[string]any) error {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("NETPULSE_TEST_SERVER_URL")), "/")
	if base == "" {
		base = "http://testserver:8090"
	}
	token := strings.TrimSpace(os.Getenv("NETPULSE_TEST_CONTROL_TOKEN"))
	if token == "" {
		return fmt.Errorf("NETPULSE_TEST_CONTROL_TOKEN is not configured")
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("X-NetPulse-Control-Token", token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("test controller returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return err
		}
	}
	return nil
}
