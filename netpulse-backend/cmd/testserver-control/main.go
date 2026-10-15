package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	listenAddr       = ":8090"
	stressDuration   = 300 * time.Second
	maxAgentSecret   = 512
	minAgentSecret   = 20
)

type controller struct {
	mu         sync.Mutex
	agent      *exec.Cmd
	agentDone  chan struct{}
	stress     *exec.Cmd
	stressDone chan struct{}
	token      string
	wsURL      string
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.LUTC)

	token := strings.TrimSpace(os.Getenv("NETPULSE_TEST_CONTROL_TOKEN"))
	wsURL := strings.TrimSpace(os.Getenv("NETPULSE_WS_URL"))
	if token == "" || wsURL == "" {
		log.Fatal("configuration error: NETPULSE_TEST_CONTROL_TOKEN and NETPULSE_WS_URL are required")
	}

	c := &controller{
		token: token,
		wsURL: wsURL,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	mux.HandleFunc("GET /status", c.authorize(c.status))
	mux.HandleFunc("POST /start", c.authorize(c.start))
	mux.HandleFunc("POST /stress", c.authorize(c.startStress))
	mux.HandleFunc("POST /stop-stress", c.authorize(c.stopStress))
	mux.HandleFunc("POST /stop", c.authorize(c.stop))

	server := &http.Server{
		Addr:              listenAddr,
		Handler:           logRequest(mux),
		ReadHeaderTimeout: 3 * time.Second,
	}

	log.Printf("controller starting: addr=%s", listenAddr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("controller stopped unexpectedly: %v", err)
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

		log.Printf("http request started: method=%s path=%s remote=%s content_length=%d",
			r.Method, r.URL.Path, r.RemoteAddr, r.ContentLength)

		next.ServeHTTP(sw, r)

		log.Printf("http request finished: method=%s path=%s status=%d duration=%s",
			r.Method, r.URL.Path, sw.status, time.Since(started))
	})
}

func (c *controller) authorize(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-NetPulse-Control-Token") != c.token {
			log.Printf("authorization denied: method=%s path=%s remote=%s",
				r.Method, r.URL.Path, r.RemoteAddr)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		log.Printf("authorization accepted: method=%s path=%s", r.Method, r.URL.Path)
		next(w, r)
	}
}

func (c *controller) status(w http.ResponseWriter, _ *http.Request) {
	c.mu.Lock()
	running := alive(c.agent, c.agentDone)
	stressRunning := alive(c.stress, c.stressDone)
	agentPID := processPID(c.agent, c.agentDone)
	stressPID := processPID(c.stress, c.stressDone)
	c.mu.Unlock()

	log.Printf("status requested: agent_running=%t agent_pid=%d stress_running=%t stress_pid=%d",
		running, agentPID, stressRunning, stressPID)

	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": true,
		"running": running,
		"stress":  stressRunning,
	})
}

func (c *controller) start(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AgentSecret string `json:"agent_secret"`
	}

	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
		log.Printf("start agent rejected: invalid JSON: %v", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "valid_agent_secret_required"})
		return
	}
	if len(in.AgentSecret) < minAgentSecret || len(in.AgentSecret) > maxAgentSecret {
		log.Printf("start agent rejected: invalid agent_secret length=%d", len(in.AgentSecret))
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "valid_agent_secret_required"})
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if alive(c.agent, c.agentDone) {
		log.Printf("start agent rejected: agent already running pid=%d", processPID(c.agent, c.agentDone))
		writeJSON(w, http.StatusConflict, map[string]string{"error": "test_server_already_running"})
		return
	}

	cmd := exec.Command("/usr/local/bin/netpulse-agent")
	cmd.Env = append(os.Environ(),
		"NETPULSE_WS_URL="+c.wsURL,
		"NETPULSE_AGENT_SECRET="+in.AgentSecret,
		"NETPULSE_INTERVAL=5s",
	)
	cmd.Dir = "/tmp"
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.Printf("starting agent: executable=%s dir=%s interval=5s", cmd.Path, cmd.Dir)
	if err := cmd.Start(); err != nil {
		log.Printf("agent start failed: error=%v", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "agent_start_failed"})
		return
	}

	done := make(chan struct{})
	c.agent, c.agentDone = cmd, done
	pid := cmd.Process.Pid
	startedAt := time.Now()
	log.Printf("agent started: pid=%d", pid)

	go func() {
		err := cmd.Wait()
		if err != nil {
			log.Printf("agent exited with error: pid=%d duration=%s error=%v", pid, time.Since(startedAt), err)
		} else {
			log.Printf("agent exited successfully: pid=%d duration=%s", pid, time.Since(startedAt))
		}

		close(done)
		c.mu.Lock()
		if c.agent == cmd {
			c.agent = nil
			c.agentDone = nil
		}
		c.mu.Unlock()
	}()

	writeJSON(w, http.StatusCreated, map[string]any{
		"running": true,
		"message": "Test agent started",
	})
}

func (c *controller) startStress(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind string `json:"kind"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in); err != nil {
		log.Printf("start stress rejected: invalid JSON: %v", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}

	var args []string
	switch in.Kind {
	case "cpu":
		args = []string{"--cpu", "2", "--timeout", "300s", "--metrics-brief"}
	case "memory":
		args = []string{"--vm", "1", "--vm-bytes", "256M", "--timeout", "300s", "--metrics-brief"}
	default:
		log.Printf("start stress rejected: invalid kind=%q", in.Kind)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "kind_must_be_cpu_or_memory"})
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if !alive(c.agent, c.agentDone) {
		log.Printf("start stress rejected: agent is not running")
		writeJSON(w, http.StatusConflict, map[string]string{"error": "test_server_not_running"})
		return
	}

	c.stopStressLocked()

	cmd := exec.Command("stress-ng", args...)
	cmd.Dir = "/tmp"
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.Printf("starting stress-ng: kind=%s executable=%s args=%q dir=%s timeout=%s",
		in.Kind, cmd.Path, args, cmd.Dir, stressDuration)

	if err := cmd.Start(); err != nil {
		log.Printf("stress-ng start failed: kind=%s error=%v", in.Kind, err)
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error":   "stress_start_failed",
			"details": err.Error(),
		})
		return
	}

	done := make(chan struct{})
	c.stress, c.stressDone = cmd, done
	pid := cmd.Process.Pid
	startedAt := time.Now()
	log.Printf("stress-ng started: kind=%s pid=%d", in.Kind, pid)

	go func() {
		err := cmd.Wait()
		duration := time.Since(startedAt)
		if err != nil {
			log.Printf("stress-ng exited with error: kind=%s pid=%d duration=%s error=%v",
				in.Kind, pid, duration, err)
		} else {
			log.Printf("stress-ng exited successfully: kind=%s pid=%d duration=%s",
				in.Kind, pid, duration)
		}

		close(done)
		c.mu.Lock()
		if c.stress == cmd {
			c.stress = nil
			c.stressDone = nil
		}
		c.mu.Unlock()
	}()

	writeJSON(w, http.StatusOK, map[string]any{
		"running":          true,
		"kind":             in.Kind,
		"duration_seconds": int(stressDuration.Seconds()),
	})
}

func (c *controller) stopStress(w http.ResponseWriter, _ *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.stopStressLocked(); err != nil {
		log.Printf("stop stress returned error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "stress_stop_failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"stress": false, "message": "Stress workload stopped."})
}

func (c *controller) stop(w http.ResponseWriter, _ *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.stopStressLocked(); err != nil {
		log.Printf("stop server: stress stop failed: %v", err)
	}

	if c.agent != nil && alive(c.agent, c.agentDone) {
		pid := c.agent.Process.Pid
		log.Printf("sending SIGTERM to agent: pid=%d", pid)
		if err := c.agent.Process.Signal(syscall.SIGTERM); err != nil {
			log.Printf("failed to signal agent: pid=%d error=%v", pid, err)
		}
	} else {
		log.Printf("stop server requested: agent is not running")
	}

	writeJSON(w, http.StatusOK, map[string]any{"running": false})
}

func (c *controller) stopStressLocked() error {
	if c.stress == nil || !alive(c.stress, c.stressDone) {
		log.Printf("stop stress: no active process")
		return nil
	}

	cmd := c.stress
	done := c.stressDone
	pid := cmd.Process.Pid

	log.Printf("sending SIGTERM to stress-ng: pid=%d", pid)
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			log.Printf("stress-ng already exited: pid=%d", pid)
			return nil
		}
		return err
	}

	select {
	case <-done:
		log.Printf("stress-ng stopped: pid=%d", pid)
	case <-time.After(3 * time.Second):
		log.Printf("stress-ng did not exit after SIGTERM; sending SIGKILL: pid=%d", pid)
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
		select {
		case <-done:
			log.Printf("stress-ng killed and reaped: pid=%d", pid)
		case <-time.After(2 * time.Second):
			return errors.New("timed out waiting for stress-ng to exit")
		}
	}

	return nil
}

func processPID(cmd *exec.Cmd, done chan struct{}) int {
	if !alive(cmd, done) {
		return 0
	}
	return cmd.Process.Pid
}

func alive(cmd *exec.Cmd, done chan struct{}) bool {
	if cmd == nil || done == nil || cmd.Process == nil {
		return false
	}
	select {
	case <-done:
		return false
	default:
		return true
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("failed to write JSON response: status=%d error=%v", status, err)
	}
}
