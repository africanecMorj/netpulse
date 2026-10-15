package probes

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/netpulse/netpulse-backend/internal/domain"
)

type Runner struct {
	client  *http.Client
	timeout time.Duration
}

func New(timeout time.Duration) *Runner {
	return &Runner{client: &http.Client{Timeout: timeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, timeout: timeout}
}

func (r *Runner) Check(ctx context.Context, node domain.Node) domain.ProbeResult {
	started := time.Now()
	checkCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var err error
	switch node.ProbeType {
	case domain.ProbeHTTP:
		err = r.checkHTTP(checkCtx, node.Target)
	case domain.ProbeTCP:
		err = r.checkTCP(checkCtx, node.Target)
	default:
		err = fmt.Errorf("unsupported probe type %q", node.ProbeType)
	}
	result := domain.ProbeResult{Success: err == nil, LatencyMS: float64(time.Since(started).Microseconds()) / 1000, CheckedAt: time.Now().UTC()}
	if err != nil {
		result.Error = err.Error()
	}
	return result
}

func (r *Runner) checkHTTP(ctx context.Context, target string) error {
	u, err := url.ParseRequestURI(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("target must be an absolute http(s) URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP probe failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return fmt.Errorf("unexpected HTTP status: %d", resp.StatusCode)
	}
	return nil
}

func (r *Runner) checkTCP(ctx context.Context, target string) error {
	if strings.TrimSpace(target) == "" {
		return fmt.Errorf("TCP target is required")
	}
	dialer := net.Dialer{Timeout: r.timeout}
	conn, err := dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		return fmt.Errorf("TCP probe failed: %w", err)
	}
	return conn.Close()
}
