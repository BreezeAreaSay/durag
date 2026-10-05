package ws

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 8 * 1024
	sendBuffer     = 64
)

// Client is one WebSocket connection.
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte

	mu       sync.Mutex
	roomID   string
	playerID string

	closeOnce sync.Once
	done      chan struct{}
}

func newClient(h *Hub, conn *websocket.Conn) *Client {
	return &Client{hub: h, conn: conn, send: make(chan []byte, sendBuffer), done: make(chan struct{})}
}

func (c *Client) room() (string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.roomID, c.playerID
}

func (c *Client) setRoom(roomID, playerID string) {
	c.mu.Lock()
	c.roomID, c.playerID = roomID, playerID
	c.mu.Unlock()
}

// enqueue pushes a frame to the connection; a client that cannot keep up is
// disconnected instead of blocking the hub.
func (c *Client) enqueue(frame []byte) {
	select {
	case c.send <- frame:
	case <-c.done:
	default:
		c.hub.log.Warn("ws: client too slow, closing", "player", c.playerID)
		c.close()
	}
}

func (c *Client) sendError(code, message, cardID string) {
	c.enqueue(encode(TypeError, ErrorPayload{Message: message, Code: code, CardID: cardID}))
}

// close asks the pumps to shut the connection down. Frames that are already
// queued (for example a final ERROR) are flushed by writePump before the
// socket is closed.
func (c *Client) close() {
	c.closeOnce.Do(func() { close(c.done) })
}

// readPump reads intents until the connection dies.
func (c *Client) readPump() {
	defer func() {
		c.hub.onDisconnect(c)
		c.close()
	}()
	defer c.conn.Close()
	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var env Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			c.sendError("BAD_MESSAGE", "malformed message", "")
			continue
		}
		c.hub.handle(c, env)
	}
}

// writePump serialises writes to the connection and keeps it alive with pings.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.close()
		_ = c.conn.Close()
	}()
	for {
		select {
		case frame := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.TextMessage, frame); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.done:
			// Flush what is still queued, then say goodbye properly.
			_ = c.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
			for drained := false; !drained; {
				select {
				case frame := <-c.send:
					if err := c.conn.WriteMessage(websocket.TextMessage, frame); err != nil {
						return
					}
				default:
					drained = true
				}
			}
			_ = c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return
		}
	}
}
