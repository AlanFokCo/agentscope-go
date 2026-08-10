package grpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// Message is the unit of communication between agents.
type Message struct {
	ID        string          `json:"id"`
	From      string          `json:"from"`
	To        string          `json:"to"`
	Method    string          `json:"method"`
	Payload   json.RawMessage `json:"payload"`
	Timestamp time.Time       `json:"timestamp"`
	IsStream  bool            `json:"is_stream,omitempty"`
	StreamEnd bool            `json:"stream_end,omitempty"`
}

// Transport provides bidirectional agent communication over TCP.
type Transport interface {
	Send(ctx context.Context, msg *Message) error
	Receive(ctx context.Context) (*Message, error)
	Close() error
}

// connTransport implements Transport over a single TCP connection using newline-delimited JSON.
type connTransport struct {
	conn    net.Conn
	encoder *json.Encoder
	scanner *bufio.Scanner
	mu      sync.Mutex // protects encoder writes
}

func newConnTransport(conn net.Conn) *connTransport {
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB max message
	return &connTransport{
		conn:    conn,
		encoder: json.NewEncoder(conn),
		scanner: scanner,
	}
}

func (t *connTransport) Send(_ context.Context, msg *Message) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.encoder.Encode(msg)
}

func (t *connTransport) Receive(_ context.Context) (*Message, error) {
	if !t.scanner.Scan() {
		if err := t.scanner.Err(); err != nil {
			return nil, fmt.Errorf("transport: read: %w", err)
		}
		return nil, errors.New("transport: connection closed")
	}
	var msg Message
	if err := json.Unmarshal(t.scanner.Bytes(), &msg); err != nil {
		return nil, fmt.Errorf("transport: unmarshal: %w", err)
	}
	return &msg, nil
}

func (t *connTransport) Close() error {
	return t.conn.Close()
}

// Server accepts incoming agent connections.
type Server struct {
	addr      string
	handler   func(msg *Message) *Message
	mu        sync.Mutex
	listener  net.Listener // guarded by mu for Addr(); Accept/Close are goroutine-safe
	conns     []net.Conn
	done      chan struct{}
	closeOnce sync.Once
	closeErr  error
	wg        sync.WaitGroup
}

// NewServer creates a new server bound to the given address.
// Pass ":0" to let the OS pick a free port.
func NewServer(addr string) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("server: listen: %w", err)
	}
	return &Server{
		addr:     addr,
		listener: ln,
		done:     make(chan struct{}),
	}, nil
}

// OnMessage registers a handler that processes incoming messages and returns a response.
func (s *Server) OnMessage(handler func(msg *Message) *Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handler = handler
}

// Addr returns the actual listener address (useful when using ":0").
func (s *Server) Addr() string {
	s.mu.Lock()
	ln := s.listener
	s.mu.Unlock()
	return ln.Addr().String()
}

// Listen starts accepting connections. Blocks until the context is canceled or Close is called.
func (s *Server) Listen(ctx context.Context) error {
	// Snapshot listener under the lock so concurrent Close doesn't race on
	// the struct field. net.Listener.Accept and Close are goroutine-safe.
	s.mu.Lock()
	ln := s.listener
	s.mu.Unlock()

	go func() {
		select {
		case <-ctx.Done():
			_ = s.Close()
		case <-s.done:
		}
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.done:
				return nil
			default:
				return fmt.Errorf("server: accept: %w", err)
			}
		}
		s.mu.Lock()
		s.conns = append(s.conns, conn)
		s.wg.Add(1) // must be inside the lock to avoid racing with Close's wg.Wait
		s.mu.Unlock()

		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer s.wg.Done()
	defer func() { _ = conn.Close() }()

	t := newConnTransport(conn)
	for {
		msg, err := t.Receive(context.Background())
		if err != nil {
			return // connection closed or error
		}

		s.mu.Lock()
		h := s.handler
		s.mu.Unlock()

		if h != nil {
			resp := h(msg)
			if resp != nil {
				if err := t.Send(context.Background(), resp); err != nil {
					return
				}
			}
		}
	}
}

// Close shuts down the server and all active connections.
// It is safe to call Close concurrently or multiple times.
func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)

		// Close listener — unblocks Accept in Listen.
		s.mu.Lock()
		ln := s.listener
		s.mu.Unlock()
		s.closeErr = ln.Close()

		// After ln.Close(), Accept will fail and the for-loop exits.
		// However, a conn may have been accepted just before ln.Close().
		// Its wg.Add(1) is under s.mu. Acquire+release the lock to
		// ensure any in-flight wg.Add(1) completes before we Wait.
		s.mu.Lock() //nolint:gocritic // sync barrier
		//nolint:staticcheck // intentional lock-unlock to synchronize
		s.mu.Unlock() //nolint:gocritic // intentional: sync barrier for wg.Add

		// Now safe: no new wg.Add(1) can happen.
		s.wg.Wait()

		// Close all tracked connections.
		s.mu.Lock()
		for _, c := range s.conns {
			_ = c.Close()
		}
		s.conns = nil
		s.mu.Unlock()
	})
	return s.closeErr
}

// Client connects to a remote agent server.
type Client struct {
	addr      string
	conn      net.Conn
	t         *connTransport
	mu        sync.Mutex
	pending   map[string]chan *Message
	streams   map[string]chan *Message
	closed    chan struct{}
	closeOnce sync.Once
	readWg    sync.WaitGroup
}

// NewClient connects to the remote server at addr.
func NewClient(addr string) (*Client, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("client: dial: %w", err)
	}
	c := &Client{
		addr:    addr,
		conn:    conn,
		t:       newConnTransport(conn),
		pending: make(map[string]chan *Message),
		streams: make(map[string]chan *Message),
		closed:  make(chan struct{}),
	}
	c.readWg.Add(1)
	go c.readLoop()
	return c, nil
}

func (c *Client) readLoop() {
	defer c.readWg.Done()
	for {
		msg, err := c.t.Receive(context.Background())
		if err != nil {
			select {
			case <-c.closed:
				return
			default:
				// Connection error: close all pending channels
				c.mu.Lock()
				for _, ch := range c.pending {
					close(ch)
				}
				c.pending = make(map[string]chan *Message)
				for _, ch := range c.streams {
					close(ch)
				}
				c.streams = make(map[string]chan *Message)
				c.mu.Unlock()
				return
			}
		}

		c.mu.Lock()
		// Check if it is a streaming response
		if ch, ok := c.streams[msg.ID]; ok {
			if msg.StreamEnd {
				ch <- msg
				close(ch)
				delete(c.streams, msg.ID)
			} else {
				ch <- msg
			}
		} else if ch, ok := c.pending[msg.ID]; ok {
			ch <- msg
			close(ch)
			delete(c.pending, msg.ID)
		}
		c.mu.Unlock()
	}
}

// Send sends a request and waits for a single response (request-response pattern).
func (c *Client) Send(ctx context.Context, msg *Message) (*Message, error) {
	ch := make(chan *Message, 1)

	c.mu.Lock()
	c.pending[msg.ID] = ch
	c.mu.Unlock()

	if err := c.t.Send(ctx, msg); err != nil {
		c.mu.Lock()
		delete(c.pending, msg.ID)
		c.mu.Unlock()
		return nil, fmt.Errorf("client: send: %w", err)
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, msg.ID)
		c.mu.Unlock()
		return nil, ctx.Err()
	case resp, ok := <-ch:
		if !ok {
			return nil, errors.New("client: connection closed")
		}
		return resp, nil
	}
}

// Stream sends a message and returns a channel that receives streaming responses.
// The channel is closed after a message with StreamEnd=true is received.
func (c *Client) Stream(ctx context.Context, msg *Message) (<-chan *Message, error) {
	ch := make(chan *Message, 64)

	c.mu.Lock()
	c.streams[msg.ID] = ch
	c.mu.Unlock()

	msg.IsStream = true
	if err := c.t.Send(ctx, msg); err != nil {
		c.mu.Lock()
		delete(c.streams, msg.ID)
		c.mu.Unlock()
		return nil, fmt.Errorf("client: stream send: %w", err)
	}

	return ch, nil
}

// Close terminates the client connection.
// It is safe to call Close concurrently or multiple times.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
	})
	err := c.conn.Close()
	c.readWg.Wait()
	return err
}
