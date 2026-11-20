package main

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// hub delivers tap events to the tap recipient only. There is no feed:
// a message contains the recipient's new lifetime points total, the tapper's
// user id, and how many times that tapper has tapped the recipient today.
type wsClient struct {
	conn *websocket.Conn
	mu   sync.Mutex // gorilla/websocket allows a single concurrent writer
}

func (c *wsClient) write(msgType int, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.conn.WriteMessage(msgType, data)
}

type hub struct {
	mu    sync.Mutex
	conns map[int64]map[*wsClient]bool
}

func newHub() *hub { return &hub{conns: map[int64]map[*wsClient]bool{}} }

func (h *hub) add(userID int64, c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conns[userID] == nil {
		h.conns[userID] = map[*wsClient]bool{}
	}
	h.conns[userID][c] = true
}

func (h *hub) remove(userID int64, c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if m := h.conns[userID]; m != nil {
		delete(m, c)
		if len(m) == 0 {
			delete(h.conns, userID)
		}
	}
}

type tapEvent struct {
	Type   string `json:"type"`
	From   int64  `json:"from"`
	Pair   int64  `json:"pair"`   // times the tapper has tapped you today
	Points int64  `json:"points"` // your new lifetime points total
}

func (h *hub) pushTap(toID int64, ev tapEvent) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	h.mu.Lock()
	targets := make([]*wsClient, 0)
	for c := range h.conns[toID] {
		targets = append(targets, c)
	}
	h.mu.Unlock()
	for _, c := range targets {
		if err := c.write(websocket.TextMessage, data); err != nil {
			h.remove(toID, c)
			_ = c.conn.Close()
		}
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// The session cookie is the authentication; the socket is same-site.
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (a *app) handleWS(w http.ResponseWriter, r *http.Request) {
	u := a.currentUser(r)
	if u == nil || u.Name == "" {
		http.Error(w, tr(langOf(r), "err_signin_first"), http.StatusUnauthorized)
		return
	}
	raw, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &wsClient{conn: raw}
	a.hub.add(u.ID, c)
	defer func() {
		a.hub.remove(u.ID, c)
		_ = raw.Close()
	}()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := raw.ReadMessage(); err != nil {
				return
			}
		}
	}()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if err := c.write(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
