package collector

import (
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"dnsdist-collector/model"

	dnstap "github.com/dnstap/golang-dnstap"
	framestream "github.com/farsightsec/golang-framestream"
	"google.golang.org/protobuf/proto"
)

// ClickHouse DateTime format
const chDateTimeFormat = "2006-01-02 15:04:05"

// DnsTapListener listens on a Unix socket for dnstap frames.
type DnsTapListener struct {
	SocketPath string
	LogChan    chan<- model.DNSLog
	Dropped    atomic.Uint64
	listener   net.Listener
	wg         sync.WaitGroup
}

// NewDnsTapListener creates a new listener.
func NewDnsTapListener(socketPath string, logChan chan<- model.DNSLog) *DnsTapListener {
	return &DnsTapListener{
		SocketPath: socketPath,
		LogChan:    logChan,
	}
}

// Start begins listening on the socket.
func (l *DnsTapListener) Start() error {
	// Clean up old socket if exists
	_ = os.Remove(l.SocketPath)

	var err error
	l.listener, err = net.Listen("unix", l.SocketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on socket %s: %w", l.SocketPath, err)
	}

	// Prefer secure perms (group-based). Adjust with systemd User/Group.
	if err := os.Chmod(l.SocketPath, 0660); err != nil {
		_ = l.listener.Close()
		return fmt.Errorf("failed to chmod socket: %w", err)
	}

	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		defer func() {
			if l.listener != nil {
				_ = l.listener.Close()
			}
		}()

		for {
			conn, err := l.listener.Accept()
			if err != nil {
				// On Stop(), listener.Close() will cause Accept() to error -> exit.
				// If it is temporary, we can retry a bit.
				if ne, ok := err.(net.Error); ok && ne.Temporary() {
					time.Sleep(50 * time.Millisecond)
					continue
				}
				return
			}

			l.wg.Add(1)
			go l.handleConn(conn)
		}
	}()

	return nil
}

// Stop closes the listener and waits for all handlers to finish.
func (l *DnsTapListener) Stop() {
	if l.listener != nil {
		_ = l.listener.Close()
	}
	l.wg.Wait()
}

// formatIPv6 converts an IP to ClickHouse IPv6 compatible format.
// IPv4 addresses are converted to IPv6-mapped format (::ffff:x.x.x.x)
func formatIPv6(ip net.IP) string {
	if ip == nil {
		return "::1" // fallback to localhost
	}
	if ip4 := ip.To4(); ip4 != nil {
		// IPv4 -> IPv6-mapped format
		return "::ffff:" + ip4.String()
	}
	return ip.String()
}

func (l *DnsTapListener) handleConn(conn net.Conn) {
	defer l.wg.Done()
	defer conn.Close()

	decoder, err := framestream.NewDecoder(conn, &framestream.DecoderOptions{
		ContentType:   []byte("protobuf:dnstap.Dnstap"),
		Bidirectional: true,
	})
	if err != nil {
		return
	}

	for {
		buf, err := decoder.Decode()
		if err != nil {
			if err != io.EOF {
				// optional: log.Printf("dnstap decode error: %v", err)
			}
			return
		}

		var dt dnstap.Dnstap
		if err := proto.Unmarshal(buf, &dt); err != nil {
			continue
		}
		if dt.Message == nil {
			continue
		}

		msg := dt.Message
		t := msg.GetType()

		// We only care about client-side query/response from dnsdist
		if t != dnstap.Message_CLIENT_QUERY && t != dnstap.Message_CLIENT_RESPONSE {
			continue
		}

		// Get timestamp and truncate to seconds
		var recordTime time.Time
		if t == dnstap.Message_CLIENT_RESPONSE && msg.ResponseTimeSec != nil {
			recordTime = time.Unix(int64(*msg.ResponseTimeSec), 0)
		} else if t == dnstap.Message_CLIENT_QUERY && msg.QueryTimeSec != nil {
			recordTime = time.Unix(int64(*msg.QueryTimeSec), 0)
		} else {
			recordTime = time.Now().UTC().Truncate(time.Second)
		}

		parsedLog := model.DNSLog{
			Timestamp: recordTime.UTC().Format(chDateTimeFormat),
		}

		// Map to CQ/CR (compact)
		if t == dnstap.Message_CLIENT_QUERY {
			parsedLog.ResponseType = "CQ"
		} else {
			parsedLog.ResponseType = "CR"
		}

		// Client IP from QueryAddress (dnsdist sees real client IP)
		// Convert to IPv6 format for ClickHouse IPv6 column
		if msg.QueryAddress != nil {
			parsedLog.ClientIP = formatIPv6(net.IP(msg.QueryAddress))
		} else {
			parsedLog.ClientIP = "::1" // fallback
		}

		// Choose packet data based on message type
		var packetData []byte
		if t == dnstap.Message_CLIENT_RESPONSE && msg.ResponseMessage != nil {
			packetData = msg.ResponseMessage
		} else if t == dnstap.Message_CLIENT_QUERY && msg.QueryMessage != nil {
			packetData = msg.QueryMessage
		}

		if len(packetData) > 0 {
			parsedLog.ResponseSize = uint32(len(packetData))

			// Optimize: Use custom lightweight parser instead of full Unpack
			rcode, qname, qtype, err := ParseHeaderAndQuestion(packetData)
			if err == nil {
				parsedLog.RCode = rcode
				parsedLog.QName = qname
				parsedLog.QType = qtype
			}
		}

		// Non-blocking send (drop on overflow)
		select {
		case l.LogChan <- parsedLog:
		default:
			l.Dropped.Add(1)
		}
	}
}
