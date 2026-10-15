package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/netpulse/netpulse-backend/internal/domain"
	"github.com/netpulse/netpulse-backend/internal/security"
)

type Repository struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

const nodeColumns = `id, name, target, probe_type, interval_seconds, enabled, status, last_checked_at, last_latency_ms, last_error, created_at, updated_at`

func scanNode(row pgx.Row) (domain.Node, error) {
	var n domain.Node
	err := row.Scan(&n.ID, &n.Name, &n.Target, &n.ProbeType, &n.IntervalSeconds, &n.Enabled, &n.Status, &n.LastCheckedAt, &n.LastLatencyMS, &n.LastError, &n.CreatedAt, &n.UpdatedAt)
	return n, err
}

func (r *Repository) CreateNode(ctx context.Context, in domain.CreateNodeInput) (domain.Node, error) {
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	userID, ok := security.UserID(ctx)
	if !ok {
		return domain.Node{}, security.ErrUnauthorized
	}
	q := `INSERT INTO nodes (id,user_id,name,target,probe_type,interval_seconds,enabled) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING ` + nodeColumns
	return scanNode(r.pool.QueryRow(ctx, q, uuid.New(), userID, strings.TrimSpace(in.Name), strings.TrimSpace(in.Target), in.ProbeType, in.IntervalSeconds, enabled))
}

func (r *Repository) ListNodes(ctx context.Context) ([]domain.Node, error) {
	userID, ok := security.UserID(ctx)
	if !ok {
		return nil, security.ErrUnauthorized
	}
	rows, err := r.pool.Query(ctx, `SELECT `+nodeColumns+` FROM nodes WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Node, 0)
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (r *Repository) GetNode(ctx context.Context, id uuid.UUID) (domain.Node, error) {
	userID, ok := security.UserID(ctx)
	if !ok {
		return domain.Node{}, security.ErrUnauthorized
	}
	n, err := scanNode(r.pool.QueryRow(ctx, `SELECT `+nodeColumns+` FROM nodes WHERE id=$1 AND user_id=$2`, id, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Node{}, domain.ErrNotFound
	}
	return n, err
}

func (r *Repository) DeleteNode(ctx context.Context, id uuid.UUID) error {
	userID, ok := security.UserID(ctx)
	if !ok {
		return security.ErrUnauthorized
	}
	res, err := r.pool.Exec(ctx, `DELETE FROM nodes WHERE id=$1 AND user_id=$2`, id, userID)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *Repository) ListEnabledNodes(ctx context.Context) ([]domain.Node, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+nodeColumns+` FROM nodes WHERE enabled=TRUE ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Node, 0)
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (r *Repository) RecordProbe(ctx context.Context, nodeID uuid.UUID, result domain.ProbeResult) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var latency any = result.LatencyMS
	var probeErr any
	if result.Error != "" {
		probeErr = result.Error
	}
	if _, err = tx.Exec(ctx, `INSERT INTO metric_samples(node_id,checked_at,success,latency_ms,error) VALUES($1,$2,$3,$4,$5)`, nodeID, result.CheckedAt, result.Success, latency, probeErr); err != nil {
		return err
	}
	status := domain.StatusHealthy
	if !result.Success {
		status = domain.StatusUnhealthy
	} else if result.LatencyMS >= 1000 {
		status = domain.StatusDegraded
	}
	var lastLatency any = result.LatencyMS
	var lastErr any
	if result.Error != "" {
		lastErr = result.Error
	}
	if _, err = tx.Exec(ctx, `UPDATE nodes SET status=$2,last_checked_at=$3,last_latency_ms=$4,last_error=$5,updated_at=now() WHERE id=$1`, nodeID, status, result.CheckedAt, lastLatency, lastErr); err != nil {
		return err
	}
	if !result.Success {
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM incidents WHERE node_id=$1 AND status='open')`, nodeID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			if _, err = tx.Exec(ctx, `INSERT INTO incidents(id,node_id,status,severity,title,details) VALUES($1,$2,'open','critical',$3,$4)`, uuid.New(), nodeID, "Node is unreachable", result.Error); err != nil {
				return err
			}
		}
	} else {
		if _, err = tx.Exec(ctx, `UPDATE incidents SET status='resolved',resolved_at=now() WHERE node_id=$1 AND status='open'`, nodeID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *Repository) ListSamples(ctx context.Context, id uuid.UUID, limit int) ([]domain.MetricSample, error) {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `SELECT id,node_id,checked_at,success,latency_ms,error FROM metric_samples WHERE node_id=$1 ORDER BY checked_at DESC LIMIT $2`, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.MetricSample, 0)
	for rows.Next() {
		var m domain.MetricSample
		if err := rows.Scan(&m.ID, &m.NodeID, &m.CheckedAt, &m.Success, &m.LatencyMS, &m.Error); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *Repository) ListIncidents(ctx context.Context, status string, limit int) ([]domain.Incident, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	var rows pgx.Rows
	var err error
	userID, ok := security.UserID(ctx)
	if !ok {
		return nil, security.ErrUnauthorized
	}
	if status == "open" || status == "resolved" {
		rows, err = r.pool.Query(ctx, `SELECT i.id,i.node_id,i.status,i.severity,i.title,i.details,i.opened_at,i.resolved_at FROM incidents i JOIN nodes n ON n.id=i.node_id WHERE n.user_id=$1 AND i.status=$2 ORDER BY i.opened_at DESC LIMIT $3`, userID, status, limit)
	} else {
		rows, err = r.pool.Query(ctx, `SELECT i.id,i.node_id,i.status,i.severity,i.title,i.details,i.opened_at,i.resolved_at FROM incidents i JOIN nodes n ON n.id=i.node_id WHERE n.user_id=$1 ORDER BY i.opened_at DESC LIMIT $2`, userID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Incident, 0)
	for rows.Next() {
		var i domain.Incident
		if err := rows.Scan(&i.ID, &i.NodeID, &i.Status, &i.Severity, &i.Title, &i.Details, &i.OpenedAt, &i.ResolvedAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (r *Repository) CleanupOldSamples(ctx context.Context, days int) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin metrics cleanup: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM metric_samples WHERE checked_at < now() - ($1 * interval '1 day')`, days); err != nil {
		return fmt.Errorf("cleanup probe samples: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM agent_metric_samples WHERE collected_at < now() - ($1 * interval '1 day')`, days); err != nil {
		return fmt.Errorf("cleanup agent samples: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit metrics cleanup: %w", err)
	}
	return nil
}

func (r *Repository) Ping(ctx context.Context) error { return r.pool.Ping(ctx) }
func (r *Repository) Pool() *pgxpool.Pool            { return r.pool }
