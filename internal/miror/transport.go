package miror

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// Note: QUIC support is currently disabled due to missing dependencies.
// To enable QUIC support, uncomment the QUIC-related code and add the
// required dependencies to go.mod.

// Message types
const (
	MessageTypeHello      byte = 0x01
	MessageTypeOffer      byte = 0x02
	MessageTypeAccept     byte = 0x03
	MessageTypeData       byte = 0x04
	MessageTypeAck        byte = 0x05
	MessageTypeComplete   byte = 0x06
	MessageTypeError      byte = 0x07
	MessageTypeVersion    byte = 0x08
	MessageTypeVersionAck byte = 0x09
	MessageTypeResume     byte = 0x0A
)

// TransportFactory creates transports based on the configuration.
type TransportFactory struct {
	config TransportConfig
}

// NewTransportFactory creates a new transport factory.
func NewTransportFactory(config TransportConfig) *TransportFactory {
	return &TransportFactory{
		config: config,
	}
}

// CreateTransport creates a new transport for the given peer.
func (f *TransportFactory) CreateTransport(ctx context.Context, peer string) (Transport, error) {
	// QUIC support is currently disabled
	// Always use TCP for now
	tcpTransport, err := NewTCPTransport(peer, f.config)
	if err != nil {
		return nil, fmt.Errorf("failed to create TCP transport: %w", err)
	}

	err = tcpTransport.Connect(ctx)
	if err != nil {
		tcpTransport.Close()
		return nil, fmt.Errorf("failed to connect with TCP: %w", err)
	}

	return tcpTransport, nil
}

// QUICTransport is a placeholder for the QUIC transport implementation.
// This is currently disabled due to missing dependencies.
type QUICTransport struct {
	peer   string
	config TransportConfig
}

// NewQUICTransport creates a new QUIC transport.
func NewQUICTransport(peer string, config TransportConfig) (*QUICTransport, error) {
	return nil, fmt.Errorf("QUIC transport is not implemented")
}

// Connect establishes a QUIC connection to the peer.
func (t *QUICTransport) Connect(ctx context.Context) error {
	return fmt.Errorf("QUIC transport is not implemented")
}

// Close closes the QUIC connection.
func (t *QUICTransport) Close() error {
	return nil
}

// Send sends a message to the peer.
func (t *QUICTransport) Send(ctx context.Context, msgType byte, data []byte) error {
	return fmt.Errorf("QUIC transport is not implemented")
}

// Receive receives a message from the peer.
func (t *QUICTransport) Receive(ctx context.Context) (byte, []byte, error) {
	return 0, nil, fmt.Errorf("QUIC transport is not implemented")
}

// Type returns the transport type.
func (t *QUICTransport) Type() TransportType {
	return TransportQUIC
}

// RemoteAddr returns the remote address.
func (t *QUICTransport) RemoteAddr() string {
	return ""
}

// TCPTransport implements the Transport interface using TCP.
type TCPTransport struct {
	peer   string
	config TransportConfig
	conn   net.Conn
}

// NewTCPTransport creates a new TCP transport.
func NewTCPTransport(peer string, config TransportConfig) (*TCPTransport, error) {
	return &TCPTransport{
		peer:   peer,
		config: config,
	}, nil
}

// Connect establishes a TCP connection to the peer.
func (t *TCPTransport) Connect(ctx context.Context) error {
	// Parse the peer address
	host, port, err := net.SplitHostPort(t.peer)
	if err != nil {
		// If no port is specified, use the default TCP port
		host = t.peer
		port = "7001" // Default TCP port for n1
	}

	// Create a dialer with the context
	dialer := &net.Dialer{
		Timeout: t.config.ConnectTimeout,
	}

	// Connect to the peer
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return fmt.Errorf("failed to dial TCP: %w", err)
	}

	// Set keep-alive
	tcpConn, ok := conn.(*net.TCPConn)
	if ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(t.config.KeepAliveInterval)
	}

	t.conn = conn

	return nil
}

// Close closes the TCP connection.
func (t *TCPTransport) Close() error {
	if t.conn == nil {
		return nil
	}

	err := t.conn.Close()
	t.conn = nil

	if err != nil {
		return fmt.Errorf("failed to close TCP connection: %w", err)
	}

	return nil
}

// Send sends a message to the peer.
func (t *TCPTransport) Send(ctx context.Context, msgType byte, data []byte) error {
	if t.conn == nil {
		return ErrSessionClosed
	}

	// Set a deadline if the context has one
	if deadline, ok := ctx.Deadline(); ok {
		t.conn.SetWriteDeadline(deadline)
		defer t.conn.SetWriteDeadline(time.Time{}) // Clear deadline after write
	}

	// Create a header with the message type and length
	header := make([]byte, 5)
	header[0] = msgType
	binary.BigEndian.PutUint32(header[1:], uint32(len(data)))

	// Write the header
	_, err := t.conn.Write(header)
	if err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	// Write the data
	_, err = t.conn.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	return nil
}

// Receive receives a message from the peer.
func (t *TCPTransport) Receive(ctx context.Context) (byte, []byte, error) {
	if t.conn == nil {
		return 0, nil, ErrSessionClosed
	}

	// Set a deadline if the context has one
	if deadline, ok := ctx.Deadline(); ok {
		t.conn.SetReadDeadline(deadline)
		defer t.conn.SetReadDeadline(time.Time{}) // Clear deadline after read
	}

	// Read the header
	header := make([]byte, 5)
	_, err := io.ReadFull(t.conn, header)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return 0, nil, ctx.Err()
		}
		return 0, nil, fmt.Errorf("failed to read header: %w", err)
	}

	// Parse the header
	msgType := header[0]
	dataLen := binary.BigEndian.Uint32(header[1:])

	// Read the data
	data := make([]byte, dataLen)
	_, err = io.ReadFull(t.conn, data)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return 0, nil, ctx.Err()
		}
		return 0, nil, fmt.Errorf("failed to read data: %w", err)
	}

	return msgType, data, nil
}

// Type returns the transport type.
func (t *TCPTransport) Type() TransportType {
	return TransportTCP
}

// RemoteAddr returns the remote address.
func (t *TCPTransport) RemoteAddr() string {
	if t.conn == nil {
		return ""
	}
	return t.conn.RemoteAddr().String()
}
