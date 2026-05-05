package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

type TunnelFrame struct {
	Type       string              `json:"type"`
	ID         string              `json:"id"`
	Method     string              `json:"method,omitempty"`
	URL        string              `json:"url,omitempty"`
	Headers    map[string][]string `json:"headers,omitempty"`
	Body       string              `json:"body,omitempty"`
	Status     int                 `json:"status,omitempty"`
	StatusText string              `json:"statusText,omitempty"`
	Opcode     int                 `json:"opcode,omitempty"`
}

type TunnelProxy struct {
	port   int
	http   *http.Client
	wsMu   sync.Mutex
	wsConn map[string]*websocket.Conn
}

func NewTunnelProxy(port int) *TunnelProxy {
	return &TunnelProxy{
		port:   port,
		http:   &http.Client{Timeout: 0},
		wsConn: make(map[string]*websocket.Conn),
	}
}

func (p *TunnelProxy) Attach(dc *webrtc.DataChannel) {
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		var frame TunnelFrame
		if err := json.Unmarshal(msg.Data, &frame); err != nil {
			p.send(dc, TunnelFrame{Type: "error", StatusText: "invalid tunnel frame"})
			return
		}

		switch frame.Type {
		case "http-request":
			go p.handleHTTPRequest(dc, frame)
		case "ws-open":
			go p.handleWSOpen(dc, frame)
		case "ws-send":
			go p.handleWSSend(dc, frame)
		case "ws-close":
			p.handleWSClose(frame.ID)
		}
	})
}

func (p *TunnelProxy) handleHTTPRequest(dc *webrtc.DataChannel, frame TunnelFrame) {
	body, err := decodeBody(frame.Body)
	if err != nil {
		p.send(dc, TunnelFrame{Type: "http-error", ID: frame.ID, Status: http.StatusBadRequest, StatusText: err.Error()})
		return
	}

	target, err := p.localURL("http", frame.URL)
	if err != nil {
		p.send(dc, TunnelFrame{Type: "http-error", ID: frame.ID, Status: http.StatusBadRequest, StatusText: err.Error()})
		return
	}
	req, err := http.NewRequest(frame.Method, target, bytes.NewReader(body))
	if err != nil {
		p.send(dc, TunnelFrame{Type: "http-error", ID: frame.ID, Status: http.StatusBadRequest, StatusText: err.Error()})
		return
	}
	copyHeaders(req.Header, frame.Headers)

	resp, err := p.http.Do(req)
	if err != nil {
		p.send(dc, TunnelFrame{Type: "http-error", ID: frame.ID, Status: http.StatusBadGateway, StatusText: err.Error()})
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		p.send(dc, TunnelFrame{Type: "http-error", ID: frame.ID, Status: http.StatusBadGateway, StatusText: err.Error()})
		return
	}

	p.send(dc, TunnelFrame{
		Type:       "http-response",
		ID:         frame.ID,
		Status:     resp.StatusCode,
		StatusText: resp.Status,
		Headers:    filteredHeaders(resp.Header),
		Body:       base64.StdEncoding.EncodeToString(respBody),
	})
}

func (p *TunnelProxy) handleWSOpen(dc *webrtc.DataChannel, frame TunnelFrame) {
	target, err := p.localURL("ws", frame.URL)
	if err != nil {
		p.send(dc, TunnelFrame{Type: "ws-error", ID: frame.ID, StatusText: err.Error()})
		return
	}
	conn, _, err := websocket.DefaultDialer.Dial(target, http.Header(frame.Headers))
	if err != nil {
		p.send(dc, TunnelFrame{Type: "ws-error", ID: frame.ID, StatusText: err.Error()})
		return
	}

	p.wsMu.Lock()
	p.wsConn[frame.ID] = conn
	p.wsMu.Unlock()
	p.send(dc, TunnelFrame{Type: "ws-opened", ID: frame.ID})

	go func() {
		defer p.handleWSClose(frame.ID)
		for {
			opcode, payload, err := conn.ReadMessage()
			if err != nil {
				p.send(dc, TunnelFrame{Type: "ws-close", ID: frame.ID, StatusText: err.Error()})
				return
			}
			p.send(dc, TunnelFrame{
				Type:   "ws-message",
				ID:     frame.ID,
				Opcode: opcode,
				Body:   base64.StdEncoding.EncodeToString(payload),
			})
		}
	}()
}

func (p *TunnelProxy) handleWSSend(dc *webrtc.DataChannel, frame TunnelFrame) {
	payload, err := decodeBody(frame.Body)
	if err != nil {
		p.send(dc, TunnelFrame{Type: "ws-error", ID: frame.ID, StatusText: err.Error()})
		return
	}
	p.wsMu.Lock()
	conn := p.wsConn[frame.ID]
	p.wsMu.Unlock()
	if conn == nil {
		p.send(dc, TunnelFrame{Type: "ws-error", ID: frame.ID, StatusText: "websocket is not open"})
		return
	}
	if err := conn.WriteMessage(frame.Opcode, payload); err != nil {
		p.send(dc, TunnelFrame{Type: "ws-error", ID: frame.ID, StatusText: err.Error()})
	}
}

func (p *TunnelProxy) handleWSClose(id string) {
	p.wsMu.Lock()
	conn := p.wsConn[id]
	delete(p.wsConn, id)
	p.wsMu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func (p *TunnelProxy) localURL(scheme, raw string) (string, error) {
	if raw == "" {
		raw = "/"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	u.Scheme = scheme
	u.Host = "127.0.0.1:" + strconvItoa(p.port)
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), nil
}

func (p *TunnelProxy) send(dc *webrtc.DataChannel, frame TunnelFrame) {
	payload, err := json.Marshal(frame)
	if err == nil {
		_ = dc.Send(payload)
	}
}

func decodeBody(encoded string) ([]byte, error) {
	if encoded == "" {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(encoded)
}

func copyHeaders(dst http.Header, src map[string][]string) {
	for key, values := range src {
		if isHopHeader(key) {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func filteredHeaders(headers http.Header) map[string][]string {
	out := make(map[string][]string, len(headers))
	for key, values := range headers {
		if isHopHeader(key) {
			continue
		}
		out[key] = append([]string(nil), values...)
	}
	return out
}

func isHopHeader(key string) bool {
	switch strings.ToLower(key) {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}
