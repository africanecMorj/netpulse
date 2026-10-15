package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/netpulse/netpulse-backend/internal/domain"
)

var ErrEmailTaken = errors.New("email already registered")
var ErrInvalidCredentials = errors.New("invalid credentials")
var ErrTokenInvalid = errors.New("token invalid or expired")

type AuthUser struct {
	ID           uuid.UUID
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

func (r *Repository) CreateUser(ctx context.Context, email, passwordHash string) (domain.User, error) {
	var u domain.User
	err := r.pool.QueryRow(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,$2,$3) RETURNING id,email,created_at`, uuid.New(), email, passwordHash).Scan(&u.ID, &u.Email, &u.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return domain.User{}, ErrEmailTaken
	}
	return u, err
}
func (r *Repository) UserByEmail(ctx context.Context, email string) (AuthUser, error) {
	var u AuthUser
	err := r.pool.QueryRow(ctx, `SELECT id,email,password_hash,created_at FROM users WHERE email=$1`, email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthUser{}, ErrInvalidCredentials
	}
	return u, err
}
func (r *Repository) CreateRefreshToken(ctx context.Context, userID uuid.UUID, hash string, expires time.Time) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO refresh_tokens(token_hash,user_id,expires_at) VALUES($1,$2,$3)`, hash, userID, expires)
	return err
}
func (r *Repository) RotateRefreshToken(ctx context.Context, oldHash, newHash string, expires time.Time) (uuid.UUID, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)
	var userID uuid.UUID
	err = tx.QueryRow(ctx, `UPDATE refresh_tokens SET used_at=now() WHERE token_hash=$1 AND used_at IS NULL AND expires_at>now() RETURNING user_id`, oldHash).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrTokenInvalid
	}
	if err != nil {
		return uuid.Nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO refresh_tokens(token_hash,user_id,expires_at) VALUES($1,$2,$3)`, newHash, userID, expires); err != nil {
		return uuid.Nil, err
	}
	return userID, tx.Commit(ctx)
}
func (r *Repository) RevokeRefreshToken(ctx context.Context, hash string) error {
	_, err := r.pool.Exec(ctx, `UPDATE refresh_tokens SET used_at=COALESCE(used_at,now()) WHERE token_hash=$1`, hash)
	return err
}

func (r *Repository) CreateAgentEnrollment(ctx context.Context, userID uuid.UUID, name, tokenHash string, expires time.Time) (domain.Agent, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Agent{}, err
	}
	defer tx.Rollback(ctx)
	var a domain.Agent
	err = tx.QueryRow(ctx, `INSERT INTO agents(id,user_id,name) VALUES($1,$2,$3) RETURNING id,name,hostname,status,last_seen_at,created_at`, uuid.New(), userID, name).Scan(&a.ID, &a.Name, &a.Hostname, &a.Status, &a.LastSeenAt, &a.CreatedAt)
	if err != nil {
		return domain.Agent{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO agent_enrollments(id,agent_id,token_hash,expires_at) VALUES($1,$2,$3,$4)`, uuid.New(), a.ID, tokenHash, expires); err != nil {
		return domain.Agent{}, err
	}
	return a, tx.Commit(ctx)
}
func (r *Repository) ConsumeEnrollment(ctx context.Context, tokenHash, secretHash, hostname string) (uuid.UUID, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)
	var agentID uuid.UUID
	err = tx.QueryRow(ctx, `UPDATE agent_enrollments SET used_at=now() WHERE token_hash=$1 AND used_at IS NULL AND expires_at>now() RETURNING agent_id`, tokenHash).Scan(&agentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrTokenInvalid
	}
	if err != nil {
		return uuid.Nil, err
	}
	res, err := tx.Exec(ctx, `UPDATE agents SET secret_hash=$2,hostname=$3,status='offline',updated_at=now() WHERE id=$1 AND secret_hash IS NULL AND status='pending'`, agentID, secretHash, hostname)
	if err != nil {
		return uuid.Nil, err
	}
	if res.RowsAffected() != 1 {
		return uuid.Nil, ErrTokenInvalid
	}
	return agentID, tx.Commit(ctx)
}
func (r *Repository) AuthenticateAgent(ctx context.Context, secretHash string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `SELECT id FROM agents WHERE secret_hash=$1 AND secret_revoked_at IS NULL AND status<>'revoked'`, secretHash).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrTokenInvalid
	}
	return id, err
}
func (r *Repository) SaveAgentMetrics(ctx context.Context, agentID uuid.UUID, m domain.AgentMetrics) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var activeID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM agents WHERE id=$1 AND secret_revoked_at IS NULL AND status<>'revoked' FOR UPDATE`, agentID).Scan(&activeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTokenInvalid
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO agent_metric_samples(agent_id,collected_at,cpu_percent,memory_used_bytes,memory_total_bytes,disk_used_bytes,disk_total_bytes,network_rx_bytes_per_sec,network_tx_bytes_per_sec) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, agentID, m.Timestamp, m.CPUPercent, m.MemoryUsedBytes, m.MemoryTotalBytes, m.DiskUsedBytes, m.DiskTotalBytes, m.NetworkRXBytesPerSec, m.NetworkTXBytesPerSec); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE agents SET status='online',last_seen_at=now(),hostname=CASE WHEN $2='' THEN hostname ELSE $2 END,updated_at=now() WHERE id=$1 AND secret_revoked_at IS NULL`, agentID, m.Hostname); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *Repository) ListAgents(ctx context.Context, userID uuid.UUID) ([]domain.Agent, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,name,hostname,status,last_seen_at,created_at FROM agents WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Agent, 0)
	for rows.Next() {
		var a domain.Agent
		if err := rows.Scan(&a.ID, &a.Name, &a.Hostname, &a.Status, &a.LastSeenAt, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (r *Repository) ListAgentMetrics(ctx context.Context, userID, agentID uuid.UUID, limit int) ([]domain.AgentMetrics, error) {
	rows, err := r.pool.Query(ctx, `SELECT m.collected_at,m.cpu_percent,m.memory_used_bytes,m.memory_total_bytes,m.disk_used_bytes,m.disk_total_bytes,m.network_rx_bytes_per_sec,m.network_tx_bytes_per_sec FROM agent_metric_samples m JOIN agents a ON a.id=m.agent_id WHERE a.id=$1 AND a.user_id=$2 ORDER BY m.collected_at DESC LIMIT $3`, agentID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.AgentMetrics, 0)
	for rows.Next() {
		var m domain.AgentMetrics
		if err := rows.Scan(&m.Timestamp, &m.CPUPercent, &m.MemoryUsedBytes, &m.MemoryTotalBytes, &m.DiskUsedBytes, &m.DiskTotalBytes, &m.NetworkRXBytesPerSec, &m.NetworkTXBytesPerSec); err != nil {
			return nil, err
		}
		m.Type = "metrics"
		out = append(out, m)
	}
	return out, rows.Err()
}
func (r *Repository) RevokeAgent(ctx context.Context, userID, agentID uuid.UUID) error {
	res, err := r.pool.Exec(ctx, `UPDATE agents SET status='revoked',secret_revoked_at=now(),updated_at=now() WHERE id=$1 AND user_id=$2 AND status<>'revoked'`, agentID, userID)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *Repository) TouchAgent(ctx context.Context, agentID uuid.UUID, hostname string) error {
	res, err := r.pool.Exec(ctx, `UPDATE agents SET status='online',last_seen_at=now(),hostname=CASE WHEN $2='' THEN hostname ELSE $2 END,updated_at=now() WHERE id=$1 AND secret_revoked_at IS NULL AND status<>'revoked'`, agentID, hostname)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrTokenInvalid
	}
	return nil
}

func (r *Repository) MarkStaleAgentsOffline(ctx context.Context, timeout time.Duration) error {
	_, err := r.pool.Exec(ctx, `UPDATE agents SET status='offline',updated_at=now() WHERE status='online' AND last_seen_at < now() - $1::interval`, timeout.String())
	return err
}
