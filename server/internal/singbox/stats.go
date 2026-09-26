package singbox

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// StatsClient queries sing-box's v2ray_api StatsService (registered under
// v2ray's service name for compatibility). It speaks just
// enough gRPC (HTTP/2 cleartext, length-prefixed protobuf) for one unary
// method, which avoids pulling grpc-go and protobuf into the panel.
type StatsClient struct {
	addr   string
	client *http.Client
}

func NewStatsClient(addr string) *StatsClient {
	var p http.Protocols
	p.SetUnencryptedHTTP2(true)
	return &StatsClient{
		addr: addr,
		client: &http.Client{
			Timeout:   5 * time.Second,
			Transport: &http.Transport{Protocols: &p},
		},
	}
}

// UserTraffic is the traffic of one sing-box user since the previous reset.
type UserTraffic struct {
	User     string
	Uplink   int64
	Downlink int64
}

// QueryUsers returns and resets the per-user counters.
func (c *StatsClient) QueryUsers(ctx context.Context) ([]UserTraffic, error) {
	stats, err := c.queryStats(ctx, "user>>>", true)
	if err != nil {
		return nil, err
	}
	byUser := map[string]*UserTraffic{}
	var order []string
	for name, v := range stats {
		// user>>>NAME>>>traffic>>>uplink|downlink
		parts := strings.Split(name, ">>>")
		if len(parts) != 4 || parts[0] != "user" || parts[2] != "traffic" {
			continue
		}
		u := byUser[parts[1]]
		if u == nil {
			u = &UserTraffic{User: parts[1]}
			byUser[parts[1]] = u
			order = append(order, parts[1])
		}
		switch parts[3] {
		case "uplink":
			u.Uplink += v
		case "downlink":
			u.Downlink += v
		}
	}
	out := make([]UserTraffic, 0, len(order))
	for _, name := range order {
		out = append(out, *byUser[name])
	}
	return out, nil
}

func (c *StatsClient) queryStats(ctx context.Context, pattern string, reset bool) (map[string]int64, error) {
	// QueryStatsRequest{pattern = 1; reset = 2}
	var msg []byte
	msg = appendTag(msg, 1, 2)
	msg = binary.AppendUvarint(msg, uint64(len(pattern)))
	msg = append(msg, pattern...)
	if reset {
		msg = appendTag(msg, 2, 0)
		msg = append(msg, 1)
	}
	frame := make([]byte, 5, 5+len(msg))
	binary.BigEndian.PutUint32(frame[1:], uint32(len(msg)))
	frame = append(frame, msg...)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://"+c.addr+"/v2ray.core.app.stats.command.StatsService/QueryStats", bytes.NewReader(frame))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/grpc")
	req.Header.Set("TE", "trailers")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stats api: HTTP %d", resp.StatusCode)
	}
	status := resp.Trailer.Get("Grpc-Status")
	if status == "" {
		status = resp.Header.Get("Grpc-Status")
	}
	if status != "" && status != "0" {
		return nil, fmt.Errorf("stats api: grpc status %s: %s", status, resp.Trailer.Get("Grpc-Message"))
	}
	if len(body) < 5 {
		return map[string]int64{}, nil
	}
	n := binary.BigEndian.Uint32(body[1:5])
	if body[0] != 0 || int(n) > len(body)-5 {
		return nil, errors.New("stats api: malformed response frame")
	}
	return decodeQueryStatsResponse(body[5 : 5+n])
}

func appendTag(b []byte, field, wire int) []byte {
	return binary.AppendUvarint(b, uint64(field<<3|wire))
}

// decodeQueryStatsResponse parses QueryStatsResponse{repeated Stat stat = 1}
// with Stat{string name = 1; int64 value = 2}.
func decodeQueryStatsResponse(b []byte) (map[string]int64, error) {
	out := map[string]int64{}
	err := walk(b, func(field, wire int, v uint64, data []byte) error {
		if field != 1 || wire != 2 {
			return nil
		}
		var (
			name  string
			value int64
		)
		err := walk(data, func(field, wire int, v uint64, data []byte) error {
			switch {
			case field == 1 && wire == 2:
				name = string(data)
			case field == 2 && wire == 0:
				value = int64(v)
			}
			return nil
		})
		if err != nil {
			return err
		}
		out[name] += value
		return nil
	})
	return out, err
}

var errProto = errors.New("stats api: malformed protobuf")

// walk iterates the fields of a protobuf message.
func walk(b []byte, fn func(field, wire int, v uint64, data []byte) error) error {
	for len(b) > 0 {
		key, n := binary.Uvarint(b)
		if n <= 0 {
			return errProto
		}
		b = b[n:]
		field, wire := int(key>>3), int(key&7)
		switch wire {
		case 0:
			v, n := binary.Uvarint(b)
			if n <= 0 {
				return errProto
			}
			b = b[n:]
			if err := fn(field, wire, v, nil); err != nil {
				return err
			}
		case 1:
			if len(b) < 8 {
				return errProto
			}
			b = b[8:]
		case 2:
			l, n := binary.Uvarint(b)
			if n <= 0 || uint64(len(b)-n) < l {
				return errProto
			}
			data := b[n : n+int(l)]
			b = b[n+int(l):]
			if err := fn(field, wire, 0, data); err != nil {
				return err
			}
		case 5:
			if len(b) < 4 {
				return errProto
			}
			b = b[4:]
		default:
			return errProto
		}
	}
	return nil
}
