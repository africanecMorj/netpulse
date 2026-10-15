package httpapi

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxAgentFrame = 64 << 10

// serveAgentWebSocket implements the small RFC 6455 subset needed by the agent protocol.
// It accepts final text frames and handles close, ping and pong control frames.
func serveAgentWebSocket(w http.ResponseWriter, r *http.Request, onMessage func([]byte) error) error {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || !headerHasToken(r.Header.Get("Connection"), "upgrade") || r.Header.Get("Sec-WebSocket-Version") != "13" {
		writeError(w, http.StatusBadRequest, "websocket_upgrade_required")
		return errors.New("invalid websocket upgrade")
	}
	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	decoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(decoded) != 16 {
		writeError(w, http.StatusBadRequest, "invalid_websocket_key")
		return errors.New("invalid websocket key")
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		writeError(w, http.StatusInternalServerError, "websocket_not_supported")
		return errors.New("http hijacking is unavailable")
	}
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Time{})
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	accept := base64.StdEncoding.EncodeToString(sum[:])
	if _, err = fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept); err != nil {
		return err
	}
	if err = rw.Flush(); err != nil {
		return err
	}
	for {
		_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		opcode, payload, err := readClientFrame(rw.Reader)
		if err != nil {
			return err
		}
		switch opcode {
		case 8:
			_ = writeServerFrame(rw.Writer, 8, payload)
			_ = rw.Flush()
			return nil
		case 9:
			if err := writeServerFrame(rw.Writer, 10, payload); err != nil {
				return err
			}
			if err := rw.Flush(); err != nil {
				return err
			}
		case 10:
			continue
		case 1:
			if err := onMessage(payload); err != nil {
				response, _ := json.Marshal(map[string]string{"type": "error", "error": err.Error()})
				if writeErr := writeServerFrame(rw.Writer, 1, response); writeErr != nil {
					return writeErr
				}
				if writeErr := rw.Flush(); writeErr != nil {
					return writeErr
				}
				continue
			}
			response, _ := json.Marshal(map[string]string{"type": "ack", "status": "accepted"})
			if err := writeServerFrame(rw.Writer, 1, response); err != nil {
				return err
			}
			if err := rw.Flush(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported websocket opcode %d", opcode)
		}
	}
}

func headerHasToken(value, token string) bool {
	for _, part := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

func readClientFrame(r *bufio.Reader) (byte, []byte, error) {
	var first [2]byte
	if _, err := io.ReadFull(r, first[:]); err != nil {
		return 0, nil, err
	}
	fin := first[0]&0x80 != 0
	opcode := first[0] & 0x0f
	if !fin {
		return 0, nil, errors.New("fragmented websocket frames are not supported")
	}
	if first[0]&0x70 != 0 {
		return 0, nil, errors.New("websocket reserved bits must be zero")
	}
	masked := first[1]&0x80 != 0
	if !masked {
		return 0, nil, errors.New("client websocket frames must be masked")
	}
	length := uint64(first[1] & 0x7f)
	if length == 126 {
		var b [2]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(b[:]))
	} else if length == 127 {
		var b [8]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(b[:])
		if length>>63 != 0 {
			return 0, nil, errors.New("invalid websocket payload length")
		}
	}
	if length > maxAgentFrame {
		return 0, nil, errors.New("websocket message exceeds 64 KiB limit")
	}
	if opcode >= 8 && length > 125 {
		return 0, nil, errors.New("websocket control frame is too large")
	}
	var mask [4]byte
	if _, err := io.ReadFull(r, mask[:]); err != nil {
		return 0, nil, err
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	for i := range payload {
		payload[i] ^= mask[i%4]
	}
	return opcode, payload, nil
}

func writeServerFrame(w *bufio.Writer, opcode byte, payload []byte) error {
	if len(payload) > maxAgentFrame {
		return errors.New("server websocket payload too large")
	}
	if err := w.WriteByte(0x80 | opcode); err != nil {
		return err
	}
	switch n := len(payload); {
	case n < 126:
		if err := w.WriteByte(byte(n)); err != nil {
			return err
		}
	case n <= 65535:
		if err := w.WriteByte(126); err != nil {
			return err
		}
		var b [2]byte
		binary.BigEndian.PutUint16(b[:], uint16(n))
		if _, err := w.Write(b[:]); err != nil {
			return err
		}
	default:
		if err := w.WriteByte(127); err != nil {
			return err
		}
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], uint64(n))
		if _, err := w.Write(b[:]); err != nil {
			return err
		}
	}
	_, err := w.Write(payload)
	return err
}
