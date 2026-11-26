// Package realtime keeps the open WebSocket connections and delivers JSON
// events to users. One user may have several connections (tabs, devices).
package realtime

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait    = 10 * time.Second
	pongWait     = 60 * time.Second
	pingPeriod   = 50 * time.Second // must be shorter than pongWait
	maxReadBytes = 4 << 10          // clients only send small control events
	sendBuffer   = 64
)

// The default CheckOrigin rejects cross-site Origins, which is what stops
// other sites from opening a socket with the user's cookies.
var upgrader = websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 1024}

// Hub tracks connections per user.
type Hub struct {
	mu      sync.RWMutex
	clients map[int]map[*Client]struct{}

	// Called (in their own goroutine) when a user's first connection opens
	// and when their last one closes.
	OnOnline  func(userID int)
	OnOffline func(userID int)
	// Called for every event a client sends.
	OnEvent func(userID int, data []byte)
}

func NewHub() *Hub { return &Hub{clients: map[int]map[*Client]struct{}{}} }

// Client is one WebSocket connection.
type Client struct {
	hub    *Hub
	userID int
	conn   *websocket.Conn
	send   chan []byte
	once   sync.Once
}

// Serve upgrades the request and runs the connection until it closes. The
// caller has already authenticated userID.
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, userID int) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already wrote the HTTP error
	}
	c := &Client{hub: h, userID: userID, conn: conn, send: make(chan []byte, sendBuffer)}
	h.register(c)
	go c.writePump()
	c.readPump()
}

func (h *Hub) register(c *Client) {
	h.mu.Lock()
	set, ok := h.clients[c.userID]
	if !ok {
		set = map[*Client]struct{}{}
		h.clients[c.userID] = set
	}
	set[c] = struct{}{}
	first := len(set) == 1
	h.mu.Unlock()
	if first && h.OnOnline != nil {
		go h.OnOnline(c.userID)
	}
}

func (h *Hub) unregister(c *Client) {
	h.mu.Lock()
	set := h.clients[c.userID]
	_, present := set[c]
	delete(set, c)
	last := present && len(set) == 0
	if last {
		delete(h.clients, c.userID)
	}
	h.mu.Unlock()
	c.close()
	if last && h.OnOffline != nil {
		go h.OnOffline(c.userID)
	}
}

func (c *Client) close() { c.once.Do(func() { close(c.send) }) }

// Online reports whether the user has at least one open connection.
func (h *Hub) Online(userID int) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients[userID]) > 0
}

// Send delivers an event to every connection of the user. It never blocks:
// a connection that can't keep up is dropped and will reconnect.
func (h *Hub) Send(userID int, event any) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("[WS] marshal: %v", err)
		return
	}
	h.mu.RLock()
	var slow []*Client
	for c := range h.clients[userID] {
		select {
		case c.send <- data:
		default:
			slow = append(slow, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range slow {
		h.unregister(c)
	}
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister(c)
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxReadBytes)
	c.conn.SetReadDeadline(time.Now().Add(pongWait)) //nolint:errcheck
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		if c.hub.OnEvent != nil {
			c.hub.OnEvent(c.userID, data)
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait)) //nolint:errcheck
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, nil) //nolint:errcheck
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait)) //nolint:errcheck
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
