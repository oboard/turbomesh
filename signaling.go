package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type SignalMessage struct {
	Type      string          `json:"type"`
	BrowserID string          `json:"browser_id,omitempty"`
	SDP       string          `json:"sdp,omitempty"`
	Candidate json.RawMessage `json:"candidate,omitempty"`
	Error     string          `json:"error,omitempty"`
}

type SignalHub struct {
	mu       sync.Mutex
	sessions map[string]*SignalSession
	upgrader websocket.Upgrader
}

type SignalSession struct {
	client   *signalPeer
	browsers map[string]*signalPeer
	nextID   int
}

type signalPeer struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func newSignalPeer(conn *websocket.Conn) *signalPeer {
	return &signalPeer{conn: conn}
}

func (p *signalPeer) WriteJSON(v any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conn.WriteJSON(v)
}

func (p *signalPeer) Close() error {
	return p.conn.Close()
}

func NewSignalHub() *SignalHub {
	return &SignalHub{
		sessions: make(map[string]*SignalSession),
		upgrader: websocket.Upgrader{
			HandshakeTimeout: 10 * time.Second,
			CheckOrigin: func(*http.Request) bool {
				return true
			},
		},
	}
}

func (h *SignalHub) ServeClient(w http.ResponseWriter, r *http.Request) {
	slug := r.URL.Query().Get("slug")
	if err := validateSlug(slug); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	peer := newSignalPeer(conn)
	h.registerClient(slug, peer)
	defer h.unregisterClient(slug, peer)

	for {
		var msg SignalMessage
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		if !allowedClientSignal(msg.Type) {
			_ = peer.WriteJSON(SignalMessage{Type: "error", Error: "unsupported signal type"})
			continue
		}
		h.forwardToBrowser(slug, msg)
	}
}

func (h *SignalHub) ServeBrowser(w http.ResponseWriter, r *http.Request) {
	slug := r.URL.Query().Get("slug")
	if err := validateSlug(slug); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	peer := newSignalPeer(conn)
	browserID, err := h.registerBrowser(slug, peer)
	if err != nil {
		_ = peer.WriteJSON(SignalMessage{Type: "session-expired", Error: err.Error()})
		_ = peer.Close()
		return
	}
	defer h.unregisterBrowser(slug, browserID, peer)

	h.forwardToClient(slug, SignalMessage{Type: "browser-ready", BrowserID: browserID})
	for {
		var msg SignalMessage
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		if !allowedBrowserSignal(msg.Type) {
			_ = peer.WriteJSON(SignalMessage{Type: "error", Error: "unsupported signal type"})
			continue
		}
		msg.BrowserID = browserID
		h.forwardToClient(slug, msg)
	}
}

func (h *SignalHub) registerClient(slug string, peer *signalPeer) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if old := h.sessions[slug]; old != nil {
		if old.client != nil {
			_ = old.client.Close()
		}
		for _, browser := range old.browsers {
			_ = browser.WriteJSON(SignalMessage{Type: "session-expired"})
			_ = browser.Close()
		}
	}
	h.sessions[slug] = &SignalSession{client: peer, browsers: make(map[string]*signalPeer)}
}

func (h *SignalHub) unregisterClient(slug string, peer *signalPeer) {
	h.mu.Lock()
	defer h.mu.Unlock()

	session := h.sessions[slug]
	if session == nil || session.client != peer {
		return
	}
	for _, browser := range session.browsers {
		_ = browser.WriteJSON(SignalMessage{Type: "session-expired"})
		_ = browser.Close()
	}
	delete(h.sessions, slug)
}

func (h *SignalHub) registerBrowser(slug string, peer *signalPeer) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	session := h.sessions[slug]
	if session == nil || session.client == nil {
		return "", errors.New("session is not active")
	}
	session.nextID++
	id := slug + "-" + strconvItoa(session.nextID)
	session.browsers[id] = peer
	return id, nil
}

func (h *SignalHub) unregisterBrowser(slug, browserID string, peer *signalPeer) {
	h.mu.Lock()
	defer h.mu.Unlock()

	session := h.sessions[slug]
	if session == nil || session.browsers[browserID] != peer {
		return
	}
	delete(session.browsers, browserID)
}

func (h *SignalHub) forwardToClient(slug string, msg SignalMessage) {
	h.mu.Lock()
	var client *signalPeer
	if session := h.sessions[slug]; session != nil {
		client = session.client
	}
	h.mu.Unlock()
	if client != nil {
		_ = client.WriteJSON(msg)
	}
}

func (h *SignalHub) forwardToBrowser(slug string, msg SignalMessage) {
	h.mu.Lock()
	var browser *signalPeer
	if session := h.sessions[slug]; session != nil {
		browser = session.browsers[msg.BrowserID]
	}
	h.mu.Unlock()
	if browser != nil {
		_ = browser.WriteJSON(msg)
	}
}

func allowedClientSignal(t string) bool {
	switch t {
	case "answer", "ice", "error":
		return true
	default:
		return false
	}
}

func allowedBrowserSignal(t string) bool {
	switch t {
	case "offer", "ice", "error":
		return true
	default:
		return false
	}
}

func strconvItoa(v int) string {
	const digits = "0123456789"
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = digits[v%10]
		v /= 10
	}
	return string(buf[i:])
}
