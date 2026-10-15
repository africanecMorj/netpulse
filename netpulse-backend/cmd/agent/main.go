//go:build linux

package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const maxFrameSize = 64 << 10

type metricsMessage struct {
	Type      string     `json:"type"`
	Timestamp time.Time  `json:"timestamp"`
	Hostname  string     `json:"hostname"`
	Data      metricData `json:"data"`
}
type metricData struct {
	CPUPercent           float64 `json:"cpu_percent"`
	MemoryUsedBytes      int64   `json:"memory_used_bytes"`
	MemoryTotalBytes     int64   `json:"memory_total_bytes"`
	DiskUsedBytes        int64   `json:"disk_used_bytes"`
	DiskTotalBytes       int64   `json:"disk_total_bytes"`
	NetworkRXBytesPerSec int64   `json:"network_rx_bytes_per_sec"`
	NetworkTXBytesPerSec int64   `json:"network_tx_bytes_per_sec"`
}
type cpuCounters struct{ total, idle uint64 }
type netCounters struct{ rx, tx uint64 }
type snapshot struct {
	cpu                                          cpuCounters
	memoryUsed, memoryTotal, diskUsed, diskTotal uint64
	network                                      netCounters
	at                                           time.Time
}
type wsClient struct {
	conn   net.Conn
	reader *bufio.Reader
	writer *bufio.Writer
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	endpoint := strings.TrimSpace(os.Getenv("NETPULSE_WS_URL"))
	secret := strings.TrimSpace(os.Getenv("NETPULSE_AGENT_SECRET"))
	if endpoint == "" || secret == "" {
		logger.Error("NETPULSE_WS_URL and NETPULSE_AGENT_SECRET are required")
		os.Exit(2)
	}
	interval := 10 * time.Second
	if raw := os.Getenv("NETPULSE_INTERVAL"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed < 5*time.Second {
			logger.Error("NETPULSE_INTERVAL must be a duration of at least 5s")
			os.Exit(2)
		}
		interval = parsed
	}
	hostname, err := os.Hostname()
	if err != nil {
		logger.Error("read hostname", "error", err)
		os.Exit(1)
	}
	ctx, cancel := signalContext()
	defer cancel()
	var previous snapshot
	var client *wsClient
	defer func() {
		if client != nil {
			_ = client.conn.Close()
		}
	}()
	for {
		current, err := collectSnapshot()
		if err != nil {
			logger.Error("collect metrics", "error", err)
		} else if !previous.at.IsZero() {
			message := buildMessage(hostname, previous, current)
			if client == nil {
				client, err = dialWebSocket(ctx, endpoint, secret)
				if err != nil {
					logger.Error("connect to NetPulse", "error", err)
				}
			}
			if client != nil {
				if err := sendMetrics(client, message); err != nil {
					logger.Error("send metrics", "error", err)
					_ = client.conn.Close()
					client = nil
				} else {
					logger.Info("metrics sent", "cpu_percent", message.Data.CPUPercent, "memory_used_bytes", message.Data.MemoryUsedBytes)
				}
			}
		}
		if err == nil {
			previous = current
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func collectSnapshot() (snapshot, error) {
	cpu, err := readCPU()
	if err != nil {
		return snapshot{}, err
	}
	memUsed, memTotal, err := readMemory()
	if err != nil {
		return snapshot{}, err
	}
	var fs syscall.Statfs_t
	if err := syscall.Statfs("/", &fs); err != nil {
		return snapshot{}, err
	}
	blockSize := uint64(fs.Bsize)
	diskTotal := fs.Blocks * blockSize
	diskUsed := (fs.Blocks - fs.Bfree) * blockSize
	network, err := readNetwork()
	if err != nil {
		return snapshot{}, err
	}
	return snapshot{cpu: cpu, memoryUsed: memUsed, memoryTotal: memTotal, diskUsed: diskUsed, diskTotal: diskTotal, network: network, at: time.Now().UTC()}, nil
}
func readCPU() (cpuCounters, error) {
	// Prefer cgroup v2 counters when running in a container so the reported
	// CPU percentage reflects this container's quota, not the entire host.
	if usageData, usageErr := os.ReadFile("/sys/fs/cgroup/cpu.stat"); usageErr == nil {
		var usageUsec uint64
		for _, line := range strings.Split(string(usageData), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "usage_usec" {
				usageUsec, usageErr = strconv.ParseUint(fields[1], 10, 64)
				break
			}
		}
		if usageErr == nil {
			if maxData, maxErr := os.ReadFile("/sys/fs/cgroup/cpu.max"); maxErr == nil {
				fields := strings.Fields(string(maxData))
				if len(fields) == 2 && fields[0] != "max" {
					quota, qErr := strconv.ParseUint(fields[0], 10, 64)
					period, pErr := strconv.ParseUint(fields[1], 10, 64)
					if qErr == nil && pErr == nil && quota > 0 && period > 0 {
						cores := float64(quota) / float64(period)
						total := uint64(float64(time.Now().UnixMicro()) * cores)
						if usageUsec <= total {
							return cpuCounters{total: total, idle: total - usageUsec}, nil
						}
					}
				}
			}
		}
	}
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuCounters{}, err
	}
	line := strings.SplitN(string(b), "\n", 2)[0]
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuCounters{}, errors.New("invalid /proc/stat CPU line")
	}
	var values []uint64
	for _, field := range fields[1:] {
		n, e := strconv.ParseUint(field, 10, 64)
		if e != nil {
			return cpuCounters{}, e
		}
		values = append(values, n)
	}
	var total, idle uint64
	for i, v := range values {
		if i < 8 {
			total += v
		}
		if i == 3 || i == 4 {
			idle += v
		}
	}
	return cpuCounters{total: total, idle: idle}, nil
}
func readMemory() (uint64, uint64, error) {
	// Prefer cgroup v2 memory limits inside containers; /proc/meminfo may
	// otherwise describe the host and hide the test container's memory usage.
	if usedData, usedErr := os.ReadFile("/sys/fs/cgroup/memory.current"); usedErr == nil {
		if totalData, totalErr := os.ReadFile("/sys/fs/cgroup/memory.max"); totalErr == nil {
			used, uErr := strconv.ParseUint(strings.TrimSpace(string(usedData)), 10, 64)
			total, tErr := strconv.ParseUint(strings.TrimSpace(string(totalData)), 10, 64)
			if uErr == nil && tErr == nil && total > 0 && used <= total {
				return used, total, nil
			}
		}
	}
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, err
	}
	values := map[string]uint64{}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && (fields[0] == "MemTotal:" || fields[0] == "MemAvailable:") {
			n, e := strconv.ParseUint(fields[1], 10, 64)
			if e != nil {
				return 0, 0, e
			}
			values[strings.TrimSuffix(fields[0], ":")] = n * 1024
		}
	}
	total, ok1 := values["MemTotal"]
	available, ok2 := values["MemAvailable"]
	if !ok1 || !ok2 || available > total {
		return 0, 0, errors.New("missing or invalid memory values in /proc/meminfo")
	}
	return total - available, total, nil
}
func readNetwork() (netCounters, error) {
	b, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return netCounters{}, err
	}
	var out netCounters
	for _, line := range strings.Split(string(b), "\n")[2:] {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		iface := strings.TrimSpace(parts[0])
		if iface == "lo" {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 9 {
			continue
		}
		rx, e1 := strconv.ParseUint(fields[0], 10, 64)
		tx, e2 := strconv.ParseUint(fields[8], 10, 64)
		if e1 != nil || e2 != nil {
			return netCounters{}, errors.New("invalid /proc/net/dev counter")
		}
		out.rx += rx
		out.tx += tx
	}
	return out, nil
}
func buildMessage(host string, prev, cur snapshot) metricsMessage {
	var cpu float64
	deltaTotal := cur.cpu.total - prev.cpu.total
	deltaIdle := cur.cpu.idle - prev.cpu.idle
	if deltaTotal > 0 && deltaIdle <= deltaTotal {
		cpu = 100 * float64(deltaTotal-deltaIdle) / float64(deltaTotal)
	}
	seconds := cur.at.Sub(prev.at).Seconds()
	var rx, tx int64
	if seconds > 0 && cur.network.rx >= prev.network.rx && cur.network.tx >= prev.network.tx {
		rx = int64(float64(cur.network.rx-prev.network.rx) / seconds)
		tx = int64(float64(cur.network.tx-prev.network.tx) / seconds)
	}
	if math.IsNaN(cpu) || math.IsInf(cpu, 0) {
		cpu = 0
	}
	return metricsMessage{Type: "metrics", Timestamp: cur.at, Hostname: host, Data: metricData{CPUPercent: math.Round(cpu*100) / 100, MemoryUsedBytes: int64(cur.memoryUsed), MemoryTotalBytes: int64(cur.memoryTotal), DiskUsedBytes: int64(cur.diskUsed), DiskTotalBytes: int64(cur.diskTotal), NetworkRXBytesPerSec: rx, NetworkTXBytesPerSec: tx}}
}

func sendMetrics(client *wsClient, message metricsMessage) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if err := client.writeFrame(1, payload); err != nil {
		return err
	}
	_ = client.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	opcode, response, err := client.readFrame()
	if err != nil {
		return err
	}
	if opcode != 1 {
		return fmt.Errorf("unexpected websocket response opcode %d", opcode)
	}
	var ack struct {
		Type   string `json:"type"`
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(response, &ack); err != nil {
		return err
	}
	if ack.Type == "error" {
		return fmt.Errorf("server rejected metrics: %s", ack.Error)
	}
	if ack.Status != "accepted" {
		return fmt.Errorf("unexpected server response: %s", response)
	}
	return nil
}

func dialWebSocket(ctx context.Context, rawURL, secret string) (*wsClient, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return nil, errors.New("NETPULSE_WS_URL scheme must be ws or wss")
	}
	host := u.Host
	if !strings.Contains(host, ":") {
		if u.Scheme == "wss" {
			host = net.JoinHostPort(host, "443")
		} else {
			host = net.JoinHostPort(host, "80")
		}
	}
	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "wss" {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, err
		}
		conn = tlsConn
	}
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		conn.Close()
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	path := u.RequestURI()
	if path == "" {
		path = "/"
	}
	if _, err := fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\nAuthorization: Bearer %s\r\n\r\n", path, u.Host, key, secret); err != nil {
		conn.Close()
		return nil, err
	}
	reader := bufio.NewReader(conn)
	req := &http.Request{Method: "GET"}
	response, err := http.ReadResponse(reader, req)
	if err != nil {
		conn.Close()
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		return nil, fmt.Errorf("websocket handshake failed: %s", response.Status)
	}
	expectedSum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	expected := base64.StdEncoding.EncodeToString(expectedSum[:])
	if response.Header.Get("Sec-WebSocket-Accept") != expected {
		conn.Close()
		return nil, errors.New("invalid Sec-WebSocket-Accept")
	}
	return &wsClient{conn: conn, reader: reader, writer: bufio.NewWriter(conn)}, nil
}
func (c *wsClient) writeFrame(opcode byte, payload []byte) error {
	if len(payload) > maxFrameSize {
		return errors.New("payload exceeds 64 KiB")
	}
	if err := c.writer.WriteByte(0x80 | opcode); err != nil {
		return err
	}
	switch n := len(payload); {
	case n < 126:
		if err := c.writer.WriteByte(0x80 | byte(n)); err != nil {
			return err
		}
	case n <= 65535:
		if err := c.writer.WriteByte(0x80 | 126); err != nil {
			return err
		}
		var b [2]byte
		binary.BigEndian.PutUint16(b[:], uint16(n))
		if _, err := c.writer.Write(b[:]); err != nil {
			return err
		}
	default:
		if err := c.writer.WriteByte(0x80 | 127); err != nil {
			return err
		}
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], uint64(n))
		if _, err := c.writer.Write(b[:]); err != nil {
			return err
		}
	}
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	if _, err := c.writer.Write(mask[:]); err != nil {
		return err
	}
	masked := append([]byte(nil), payload...)
	for i := range masked {
		masked[i] ^= mask[i%4]
	}
	if _, err := c.writer.Write(masked); err != nil {
		return err
	}
	return c.writer.Flush()
}
func (c *wsClient) readFrame() (byte, []byte, error) {
	var h [2]byte
	if _, err := io.ReadFull(c.reader, h[:]); err != nil {
		return 0, nil, err
	}
	if h[0]&0x80 == 0 {
		return 0, nil, errors.New("fragmented server frame is unsupported")
	}
	opcode := h[0] & 0x0f
	length := uint64(h[1] & 0x7f)
	if length == 126 {
		var b [2]byte
		if _, err := io.ReadFull(c.reader, b[:]); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(b[:]))
	} else if length == 127 {
		var b [8]byte
		if _, err := io.ReadFull(c.reader, b[:]); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(b[:])
	}
	if length > maxFrameSize {
		return 0, nil, errors.New("server frame exceeds 64 KiB")
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(c.reader, payload); err != nil {
		return 0, nil, err
	}
	if opcode == 9 {
		if err := c.writeFrame(10, payload); err != nil {
			return 0, nil, err
		}
		return c.readFrame()
	}
	return opcode, payload, nil
}
