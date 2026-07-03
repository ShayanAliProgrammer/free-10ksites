package app

import (
        "encoding/json"
        "log"
        "net/http"
        "strings"
        "sync"
        "time"

        "github.com/gorilla/websocket"
)

// WSMessage is the JSON message broadcast to all connected clients.
type WSMessage struct {
        Type       string `json:"type"`                 // stats_updated, request_updated, request_created, request_deleted
        TrackingID string `json:"trackingId,omitempty"` // present for request_* messages
        Status     string `json:"status,omitempty"`     // new status (for request_updated)
}

// Hub manages all connected WebSocket clients. It broadcasts messages
// to every client without any per-client DB queries — clients receive
// a notification and fetch a cached fragment via HTMX.
type Hub struct {
        mu         sync.RWMutex
        clients    map[*Client]bool
        register   chan *Client
        unregister chan *Client
        broadcast  chan WSMessage
}

// Client represents a single WebSocket connection.
type Client struct {
        hub  *Hub
        conn *websocket.Conn
        send chan []byte
}

const (
        wsWriteWait      = 10 * time.Second
        wsPongWait       = 60 * time.Second
        wsPingPeriod     = 30 * time.Second
        wsMaxMessageSize = 1024 // clients only send tiny pings
        wsSendBufferSize = 256
)

func NewHub() *Hub {
        return &Hub{
                clients:    make(map[*Client]bool),
                register:   make(chan *Client, 64),
                unregister: make(chan *Client, 64),
                broadcast:  make(chan WSMessage, 256),
        }
}

// Run starts the hub's main loop. It should run in its own goroutine.
func (h *Hub) Run() {
        ticker := time.NewTicker(5 * time.Minute)
        defer ticker.Stop()
        for {
                select {
                case client := <-h.register:
                        h.mu.Lock()
                        h.clients[client] = true
                        count := len(h.clients)
                        h.mu.Unlock()
                        log.Printf("WS client connected (total: %d)", count)
                case client := <-h.unregister:
                        h.mu.Lock()
                        if _, ok := h.clients[client]; ok {
                                delete(h.clients, client)
                                close(client.send)
                        }
                        count := len(h.clients)
                        h.mu.Unlock()
                        log.Printf("WS client disconnected (total: %d)", count)
                case msg := <-h.broadcast:
                        data, err := json.Marshal(msg)
                        if err != nil {
                                log.Printf("WS marshal error: %v", err)
                                continue
                        }
                        h.mu.RLock()
                        for client := range h.clients {
                                select {
                                case client.send <- data:
                                default:
                                        // Client buffer full — disconnect slow client
                                        go func(c *Client) { h.unregister <- c }(client)
                                }
                        }
                        h.mu.RUnlock()
                case <-ticker.C:
                        // Periodic cleanup is handled by ping/pong in readPump
                }
        }
}

// Broadcast sends a message to all connected clients.
func (h *Hub) Broadcast(msg WSMessage) {
        select {
        case h.broadcast <- msg:
        default:
                log.Printf("WS broadcast channel full, dropping message: %+v", msg)
        }
}

// ClientCount returns the current number of connected clients.
func (h *Hub) ClientCount() int {
        h.mu.RLock()
        defer h.mu.RUnlock()
        return len(h.clients)
}

var upgrader = websocket.Upgrader{
        ReadBufferSize:  1024,
        WriteBufferSize: 1024,
        CheckOrigin: func(r *http.Request) bool {
                // Strict same-origin check: only allow connections from our own host
                origin := r.Header.Get("Origin")
                if origin == "" {
                        return true // Non-browser clients (curl, etc.) — no origin header
                }
                // Parse origin URL and compare host
                return isSameOrigin(origin, r.Host)
        },
}

// isSameOrigin checks if the Origin header matches the request Host.
// Both are normalized to hostname-only (port stripped) for comparison,
// since reverse proxies (Caddy, nginx) may strip or modify the port.
func isSameOrigin(origin, host string) bool {
        originHost := origin
        if i := strings.Index(origin, "://"); i >= 0 {
                originHost = origin[i+3:]
        }
        // Strip path
        if i := strings.Index(originHost, "/"); i >= 0 {
                originHost = originHost[:i]
        }
        // Strip port from both sides for comparison
        originHostname := stripPort(originHost)
        hostHostname := stripPort(host)
        return originHostname == hostHostname
}

// stripPort removes the :port suffix from a host string.
func stripPort(host string) string {
        if i := strings.LastIndex(host, ":"); i >= 0 {
                // Make sure this is a port, not part of an IPv6 address
                // (simple heuristic: only strip if there's no "]" before the ":")
                if strings.Index(host, "]") < 0 || i > strings.Index(host, "]") {
                        return host[:i]
                }
        }
        return host
}

func indexOf(sub, s string) int {
        return strings.Index(s, sub)
}

// HandleWS upgrades an HTTP connection to WebSocket and registers the client.
func (app *App) HandleWS(w http.ResponseWriter, r *http.Request) {
        conn, err := upgrader.Upgrade(w, r, nil)
        if err != nil {
                return // Upgrade failed (origin check, bad headers, etc.)
        }
        conn.SetReadLimit(wsMaxMessageSize)
        conn.SetReadDeadline(time.Now().Add(wsPongWait))
        conn.SetPongHandler(func(string) error {
                conn.SetReadDeadline(time.Now().Add(wsPongWait))
                return nil
        })

        client := &Client{
                hub:  app.hub,
                conn: conn,
                send: make(chan []byte, wsSendBufferSize),
        }

        app.hub.register <- client

        // Start read and write pumps
        go client.writePump()
        go client.readPump()
}

// readPump reads messages from the client (just pings/keepalives).
// It detects dead connections via the pong deadline.
func (c *Client) readPump() {
        defer func() {
                c.hub.unregister <- c
                c.conn.Close()
        }()
        for {
                _, _, err := c.conn.ReadMessage()
                if err != nil {
                        break // connection closed or error
                }
                // We don't process client messages — clients only listen.
        }
}

// writePump sends messages and periodic pings to the client.
// It ensures the connection stays alive without requiring client traffic.
func (c *Client) writePump() {
        ticker := time.NewTicker(wsPingPeriod)
        defer func() {
                ticker.Stop()
                c.conn.Close()
        }()
        for {
                select {
                case message, ok := <-c.send:
                        c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
                        if !ok {
                                c.conn.WriteMessage(websocket.CloseMessage, []byte{})
                                return
                        }
                        if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
                                return
                        }
                case <-ticker.C:
                        c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
                        if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
                                return
                        }
                }
        }
}
