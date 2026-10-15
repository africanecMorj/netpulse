package domain

import (
	"github.com/google/uuid"
	"time"
)

type User struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

type Agent struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Hostname   string     `json:"hostname"`
	Status     string     `json:"status"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type AgentMetrics struct {
	Type                 string    `json:"type"`
	Timestamp            time.Time `json:"timestamp"`
	Hostname             string    `json:"hostname,omitempty"`
	CPUPercent           float64   `json:"cpu_percent"`
	MemoryUsedBytes      int64     `json:"memory_used_bytes"`
	MemoryTotalBytes     int64     `json:"memory_total_bytes"`
	DiskUsedBytes        int64     `json:"disk_used_bytes"`
	DiskTotalBytes       int64     `json:"disk_total_bytes"`
	NetworkRXBytesPerSec int64     `json:"network_rx_bytes_per_sec"`
	NetworkTXBytesPerSec int64     `json:"network_tx_bytes_per_sec"`
}
