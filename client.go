package main

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

type LocalClient struct {
	serverURL  string
	port       int
	iceServers []string
	proxy      *TunnelProxy
	mu         sync.Mutex
	writeMu    sync.Mutex
	peers      map[string]*webrtc.PeerConnection
	conn       *websocket.Conn
}

func NewLocalClient(serverURL string, port int, iceServers []string) *LocalClient {
	return &LocalClient{
		serverURL:  serverURL,
		port:       port,
		iceServers: compactStrings(iceServers),
		proxy:      NewTunnelProxy(port),
		peers:      make(map[string]*webrtc.PeerConnection),
	}
}

func (c *LocalClient) Run(ctx context.Context) error {
	backoff := time.Second
	for {
		err := c.runOnce(ctx)
		c.closePeers()
		if ctx.Err() != nil {
			return nil
		}
		log.Printf("signaling disconnected: %v; reconnecting in %s", err, backoff)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		if backoff < 15*time.Second {
			backoff *= 2
		}
	}
}

func (c *LocalClient) runOnce(ctx context.Context) error {
	dialer := websocket.DefaultDialer
	conn, _, err := dialer.DialContext(ctx, c.serverURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	c.conn = conn
	c.mu.Lock()
	c.peers = make(map[string]*webrtc.PeerConnection)
	c.mu.Unlock()
	log.Printf("signaling connected")

	done := make(chan error, 1)
	go func() {
		for {
			var msg SignalMessage
			if err := conn.ReadJSON(&msg); err != nil {
				done <- err
				return
			}
			if err := c.handleSignal(msg); err != nil {
				log.Printf("signal error: %v", err)
				_ = c.writeSignal(SignalMessage{Type: "error", BrowserID: msg.BrowserID, Error: err.Error()})
			}
		}
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-done:
		return err
	}
}

func (c *LocalClient) handleSignal(msg SignalMessage) error {
	switch msg.Type {
	case "browser-ready":
		return nil
	case "offer":
		return c.handleOffer(msg)
	case "ice":
		return c.handleRemoteICE(msg)
	default:
		return nil
	}
}

func (c *LocalClient) handleOffer(msg SignalMessage) error {
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{ICEServers: c.configuredICEServers()})
	if err != nil {
		return err
	}
	c.mu.Lock()
	if old := c.peers[msg.BrowserID]; old != nil {
		_ = old.Close()
	}
	c.peers[msg.BrowserID] = pc
	c.mu.Unlock()

	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		payload, _ := json.Marshal(candidate.ToJSON())
		_ = c.writeSignal(SignalMessage{Type: "ice", BrowserID: msg.BrowserID, Candidate: payload})
	})
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("browser %s webrtc state: %s", msg.BrowserID, state.String())
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed || state == webrtc.PeerConnectionStateDisconnected {
			c.removePeer(msg.BrowserID)
		}
	})
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		if dc.Label() != "turbomesh" {
			return
		}
		dc.OnOpen(func() {
			log.Printf("browser %s tunnel open", msg.BrowserID)
		})
		c.proxy.Attach(dc)
	})

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: msg.SDP}); err != nil {
		return err
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return err
	}
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		return err
	}
	select {
	case <-gatherComplete:
	case <-time.After(3 * time.Second):
	}
	return c.writeSignal(SignalMessage{Type: "answer", BrowserID: msg.BrowserID, SDP: pc.LocalDescription().SDP})
}

func (c *LocalClient) handleRemoteICE(msg SignalMessage) error {
	c.mu.Lock()
	pc := c.peers[msg.BrowserID]
	c.mu.Unlock()
	if pc == nil || len(msg.Candidate) == 0 {
		return nil
	}
	var candidate webrtc.ICECandidateInit
	if err := json.Unmarshal(msg.Candidate, &candidate); err != nil {
		return err
	}
	return pc.AddICECandidate(candidate)
}

func (c *LocalClient) configuredICEServers() []webrtc.ICEServer {
	if len(c.iceServers) == 0 {
		return nil
	}
	return []webrtc.ICEServer{{URLs: c.iceServers}}
}

func (c *LocalClient) removePeer(browserID string) {
	c.mu.Lock()
	pc := c.peers[browserID]
	delete(c.peers, browserID)
	c.mu.Unlock()
	if pc != nil {
		_ = pc.Close()
	}
}

func (c *LocalClient) closePeers() {
	c.mu.Lock()
	peers := c.peers
	c.peers = make(map[string]*webrtc.PeerConnection)
	c.mu.Unlock()
	for _, pc := range peers {
		_ = pc.Close()
	}
}

func (c *LocalClient) writeSignal(msg SignalMessage) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteJSON(msg)
}

func compactStrings(values []string) []string {
	out := values[:0]
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}

func cloneURL(u *url.URL) *url.URL {
	copy := *u
	return &copy
}
