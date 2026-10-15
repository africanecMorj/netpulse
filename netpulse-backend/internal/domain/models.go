package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrNotFound = errors.New("resource not found")
var ErrInvalidInput = errors.New("invalid input")

type ProbeType string

const (
	ProbeHTTP ProbeType = "http"
	ProbeTCP  ProbeType = "tcp"
)

type NodeStatus string

const (
	StatusUnknown   NodeStatus = "unknown"
	StatusHealthy   NodeStatus = "healthy"
	StatusDegraded  NodeStatus = "degraded"
	StatusUnhealthy NodeStatus = "unhealthy"
)

type Node struct {
	ID              uuid.UUID  `json:"id"`
	Name            string     `json:"name"`
	Target          string     `json:"target"`
	ProbeType       ProbeType  `json:"probe_type"`
	IntervalSeconds int        `json:"interval_seconds"`
	Enabled         bool       `json:"enabled"`
	Status          NodeStatus `json:"status"`
	LastCheckedAt   *time.Time `json:"last_checked_at,omitempty"`
	LastLatencyMS   *float64   `json:"last_latency_ms,omitempty"`
	LastError       *string    `json:"last_error,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type CreateNodeInput struct {
	Name            string    `json:"name"`
	Target          string    `json:"target"`
	ProbeType       ProbeType `json:"probe_type"`
	IntervalSeconds int       `json:"interval_seconds"`
	Enabled         *bool     `json:"enabled,omitempty"`
}

type ProbeResult struct {
	Success   bool      `json:"success"`
	LatencyMS float64   `json:"latency_ms"`
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

type MetricSample struct {
	ID        int64     `json:"id"`
	NodeID    uuid.UUID `json:"node_id"`
	CheckedAt time.Time `json:"checked_at"`
	Success   bool      `json:"success"`
	LatencyMS *float64  `json:"latency_ms,omitempty"`
	Error     *string   `json:"error,omitempty"`
}

type Incident struct {
	ID         uuid.UUID  `json:"id"`
	NodeID     uuid.UUID  `json:"node_id"`
	Status     string     `json:"status"`
	Severity   string     `json:"severity"`
	Title      string     `json:"title"`
	Details    string     `json:"details"`
	OpenedAt   time.Time  `json:"opened_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
}
