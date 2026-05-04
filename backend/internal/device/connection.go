package device

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

type Connection struct {
	DeviceID string

	conn      *websocket.Conn
	sendCh    chan Envelope
	closed    chan struct{}
	closeOnce sync.Once

	mu      sync.Mutex
	waiters map[string]chan Envelope
}

func NewConnection(conn *websocket.Conn) *Connection {
	return &Connection{
		conn:    conn,
		sendCh:  make(chan Envelope, 64),
		closed:  make(chan struct{}),
		waiters: make(map[string]chan Envelope),
	}
}

func (c *Connection) Send(ctx context.Context, env Envelope) error {
	select {
	case <-c.closed:
		return errors.New("connection closed")
	default:
	}

	select {
	case c.sendCh <- env:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return errors.New("connection closed")
	}
}

func (c *Connection) Close(reason string) {
	c.closeOnce.Do(func() {
		close(c.closed)
		if c.conn != nil {
			_ = c.conn.Close(websocket.StatusNormalClosure, reason)
		}

		c.mu.Lock()
		for id, ch := range c.waiters {
			delete(c.waiters, id)
			close(ch)
		}
		c.mu.Unlock()
	})
}

func (c *Connection) ReadLoop(ctx context.Context, onMessage func(Envelope)) {
	defer c.Close("read loop ended")

	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			return
		}

		var env Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			return
		}

		if c.resolveWaiter(env) {
			continue
		}

		onMessage(env)
	}
}

func (c *Connection) WriteLoop(ctx context.Context) {
	defer c.Close("write loop ended")

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.closed:
			return
		case env := <-c.sendCh:
			writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := wsjson.Write(writeCtx, c.conn, env)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func (c *Connection) expectResponse(id string) chan Envelope {
	ch := make(chan Envelope, 1)
	c.mu.Lock()
	c.waiters[id] = ch
	c.mu.Unlock()
	return ch
}

func (c *Connection) resolveWaiter(env Envelope) bool {
	if env.Type != MsgAck && env.Type != MsgError && env.Type != MsgWorkspaceResponse {
		return false
	}

	c.mu.Lock()
	ch, ok := c.waiters[env.ID]
	if ok {
		delete(c.waiters, env.ID)
	}
	c.mu.Unlock()

	if !ok {
		return false
	}

	select {
	case ch <- env:
	default:
	}
	close(ch)
	return true
}
