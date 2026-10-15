package service

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/netpulse/netpulse-backend/internal/domain"
)

type Store interface {
	CreateNode(context.Context, domain.CreateNodeInput) (domain.Node, error)
	ListNodes(context.Context) ([]domain.Node, error)
	GetNode(context.Context, uuid.UUID) (domain.Node, error)
	DeleteNode(context.Context, uuid.UUID) error
	ListEnabledNodes(context.Context) ([]domain.Node, error)
	ListSamples(context.Context, uuid.UUID, int) ([]domain.MetricSample, error)
	ListIncidents(context.Context, string, int) ([]domain.Incident, error)
}

type Service struct{ store Store }

func New(store Store) *Service { return &Service{store: store} }

func (s *Service) CreateNode(ctx context.Context, in domain.CreateNodeInput) (domain.Node, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Target = strings.TrimSpace(in.Target)
	if in.Name == "" || len(in.Name) > 120 {
		return domain.Node{}, fmt.Errorf("name must contain 1-120 characters: %w", domain.ErrInvalidInput)
	}
	if in.IntervalSeconds == 0 {
		in.IntervalSeconds = 10
	}
	if in.IntervalSeconds < 5 || in.IntervalSeconds > 3600 {
		return domain.Node{}, fmt.Errorf("interval_seconds must be between 5 and 3600: %w", domain.ErrInvalidInput)
	}
	switch in.ProbeType {
	case domain.ProbeHTTP:
		u, err := url.ParseRequestURI(in.Target)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return domain.Node{}, fmt.Errorf("target must be an absolute http(s) URL: %w", domain.ErrInvalidInput)
		}
		if isLoopbackOrLocalHost(u.Hostname()) {
			return domain.Node{}, fmt.Errorf("loopback and local targets are not accepted by this API: %w", domain.ErrInvalidInput)
		}
	case domain.ProbeTCP:
		host, port, err := net.SplitHostPort(in.Target)
		if err != nil || host == "" || port == "" {
			return domain.Node{}, fmt.Errorf("TCP target must use host:port format: %w", domain.ErrInvalidInput)
		}
		if isLoopbackOrLocalHost(host) {
			return domain.Node{}, fmt.Errorf("loopback and local targets are not accepted by this API: %w", domain.ErrInvalidInput)
		}
	default:
		return domain.Node{}, fmt.Errorf("probe_type must be http or tcp: %w", domain.ErrInvalidInput)
	}
	return s.store.CreateNode(ctx, in)
}

func isLoopbackOrLocalHost(host string) bool {
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast())
}

func (s *Service) ListNodes(ctx context.Context) ([]domain.Node, error) {
	return s.store.ListNodes(ctx)
}
func (s *Service) GetNode(ctx context.Context, id uuid.UUID) (domain.Node, error) {
	return s.store.GetNode(ctx, id)
}
func (s *Service) DeleteNode(ctx context.Context, id uuid.UUID) error {
	return s.store.DeleteNode(ctx, id)
}
func (s *Service) ListSamples(ctx context.Context, id uuid.UUID, limit int) ([]domain.MetricSample, error) {
	return s.store.ListSamples(ctx, id, limit)
}
func (s *Service) ListIncidents(ctx context.Context, status string, limit int) ([]domain.Incident, error) {
	return s.store.ListIncidents(ctx, status, limit)
}
