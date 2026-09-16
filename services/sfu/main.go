// ChatApp SFU — our own selective forwarding unit for group calls, large
// meetings and live broadcasting. No external kit or hosted media service:
// signaling over our WebSocket, media over our own pion-based SFU, NAT
// traversal via our own embedded STUN/TURN server (pion/turn).
//
// Modes:
//   - meeting: every participant publishes; all tracks forwarded to everyone.
//   - live:    one publisher broadcasts; subscribers receive only.
//
// Tickets are HMAC credentials minted by the API service:
//
//	base64url(roomID.userID.role.expiryUnix).hexhmac256
package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/turn/v4"
	"github.com/pion/webrtc/v4"
)

var (
	sfuSecret  = os.Getenv("SFU_SECRET")
	turnSecret = os.Getenv("TURN_SECRET")
	publicIP   = getenv("PUBLIC_IP", "127.0.0.1")
	listenAddr = getenv("SFU_LISTEN", ":8095")
	turnPort   = getenv("TURN_PORT", "3478")
)

const (
	// iceReconnectGrace is how long a disconnected ICE session is kept
	// alive before the peer is removed — enough for Wi-Fi/cellular swaps
	// and NAT rebinds, short enough to free dead seats quickly.
	iceReconnectGrace = 15 * time.Second
)

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// ---- tickets ----

type ticket struct {
	RoomID, UserID, Role string
	Expiry               int64
}

func parseTicket(raw string) (*ticket, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return nil, fmt.Errorf("malformed ticket")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, err
	}
	sig, err := hex.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, []byte(sfuSecret))
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return nil, fmt.Errorf("bad signature")
	}
	f := strings.Split(string(payload), "|")
	if len(f) != 4 {
		return nil, fmt.Errorf("malformed payload")
	}
	exp, err := strconv.ParseInt(f[3], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return nil, fmt.Errorf("ticket expired")
	}
	return &ticket{RoomID: f[0], UserID: f[1], Role: f[2], Expiry: exp}, nil
}

// ---- signaling ----

type signalMsg struct {
	Type      string          `json:"type"` // offer | answer | ice
	SDP       json.RawMessage `json:"sdp,omitempty"`
	Candidate json.RawMessage `json:"candidate,omitempty"`
}

type peer struct {
	id   string
	role string // publisher | subscriber
	pc   *webrtc.PeerConnection
	ws   *websocket.Conn
	wmu  sync.Mutex

	// Quality telemetry (updated from the RTP pump and ICE callbacks).
	bytesRelayed atomic.Int64
	packetsRelayed atomic.Int64
	iceState    atomic.Value // string
	connectedAt atomic.Value // time.Time
}

func (p *peer) send(m signalMsg) {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	_ = p.ws.WriteJSON(m)
}

type room struct {
	id     string
	convID string
	mode   string // meeting | live
	mu     sync.Mutex
	peers  map[string]*peer
	// one local track per (sourcePeer, trackID), shared across subscribers
	tracks map[string]*webrtc.TrackLocalStaticRTP
	empty  time.Time
}

type hub struct {
	mu    sync.Mutex
	rooms map[string]*room
}

var rooms = &hub{rooms: map[string]*room{}}

func (h *hub) getOrCreate(id, convID, mode string) *room {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r, ok := h.rooms[id]; ok {
		return r
	}
	r := &room{id: id, convID: convID, mode: mode, peers: map[string]*peer{}, tracks: map[string]*webrtc.TrackLocalStaticRTP{}}
	h.rooms[id] = r
	return r
}

func (h *hub) reap() {
	for range time.Tick(30 * time.Second) {
		h.mu.Lock()
		for id, r := range h.rooms {
			r.mu.Lock()
			if len(r.peers) == 0 && time.Since(r.empty) > 60*time.Second {
				delete(h.rooms, id)
			}
			r.mu.Unlock()
		}
		h.mu.Unlock()
	}
}

func (r *room) addPeer(p *peer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.peers[p.id] = p
	// Existing tracks must reach the new peer (and renegotiation follows).
	for key, track := range r.tracks {
		if strings.HasPrefix(key, p.id+"|") {
			continue
		}
		if r.mode == "live" && p.role == "publisher" {
			continue
		}
		if _, err := p.pc.AddTrack(track); err == nil {
			r.offerLocked(p)
		}
	}
}

func (r *room) removePeer(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.peers[id]; ok {
		_ = p.pc.Close()
		delete(r.peers, id)
	}
	for key := range r.tracks {
		if strings.HasPrefix(key, id+"|") {
			delete(r.tracks, key)
		}
	}
	if len(r.peers) == 0 {
		r.empty = time.Now()
	}
}

// offerLocked renegotiates with p (server is impolite, clients are polite).
func (r *room) offerLocked(p *peer) {
	if p.pc.SignalingState() != webrtc.SignalingStateStable {
		return
	}
	offer, err := p.pc.CreateOffer(nil)
	if err != nil {
		return
	}
	if err := p.pc.SetLocalDescription(offer); err != nil {
		return
	}
	sdp, _ := json.Marshal(offer)
	p.send(signalMsg{Type: "offer", SDP: sdp})
}

func (r *room) handleTrack(p *peer, remote *webrtc.TrackRemote) {
	// In live mode only the publisher's media is distributed.
	if r.mode == "live" && p.role != "publisher" {
		return
	}
	local, err := webrtc.NewTrackLocalStaticRTP(
		remote.Codec().RTPCodecCapability, remote.ID(), p.id)
	if err != nil {
		return
	}
	key := p.id + "|" + remote.ID()
	r.mu.Lock()
	r.tracks[key] = local
	peers := make([]*peer, 0, len(r.peers))
	for _, other := range r.peers {
		if other.id == p.id {
			continue
		}
		if r.mode == "live" && other.role == "publisher" {
			continue
		}
		peers = append(peers, other)
	}
	r.mu.Unlock()
	for _, other := range peers {
		if _, err := other.pc.AddTrack(local); err != nil {
			continue
		}
		r.mu.Lock()
		r.offerLocked(other)
		r.mu.Unlock()
	}
	// Pump RTP from the source into the shared local track.
	buf := make([]byte, 1500)
	for {
		n, _, err := remote.Read(buf)
		if err != nil {
			return
		}
		p.bytesRelayed.Add(int64(n))
		p.packetsRelayed.Add(1)
		if _, err := local.Write(buf[:n]); err != nil {
			return
		}
	}
}

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func wsHandler(w http.ResponseWriter, req *http.Request) {
	tk, err := parseTicket(req.URL.Query().Get("ticket"))
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	convID := req.URL.Query().Get("conv")
	mode := req.URL.Query().Get("mode")
	if mode != "live" {
		mode = "meeting"
	}
	role := tk.Role
	if mode != "live" {
		role = "publisher" // everyone publishes in meetings
	}
	ws, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		return
	}
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:" + publicIP + ":" + turnPort}}},
	})
	if err != nil {
		_ = ws.Close()
		return
	}
	p := &peer{id: tk.UserID, role: role, pc: pc, ws: ws}
	r := rooms.getOrCreate(tk.RoomID, convID, mode)

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		j := c.ToJSON()
		raw, _ := json.Marshal(j)
		p.send(signalMsg{Type: "ice", Candidate: raw})
	})
	// ICE connection-state policy: 'disconnected' gets a reconnect grace
	// window (network changes, NAT rebinds); 'failed' or 'closed' tears the
	// peer down so its tracks stop being forwarded and the seat frees up.
	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		p.iceState.Store(state.String())
		switch state {
		case webrtc.ICEConnectionStateConnected:
			if v, ok := p.connectedAt.Load().(time.Time); !ok || v.IsZero() {
				p.connectedAt.Store(time.Now())
			}
		case webrtc.ICEConnectionStateDisconnected:
			log.Printf("room %s: %s ice disconnected (reconnect grace)", r.id, p.id)
			go func() {
				time.Sleep(iceReconnectGrace)
				if s, _ := p.iceState.Load().(string); s == "disconnected" {
					log.Printf("room %s: %s reconnect grace expired", r.id, p.id)
					r.removePeer(p.id)
				}
			}()
		case webrtc.ICEConnectionStateFailed, webrtc.ICEConnectionStateClosed:
			log.Printf("room %s: %s ice %s", r.id, p.id, state.String())
			r.removePeer(p.id)
		}
	})
	pc.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		go r.handleTrack(p, remote)
	})

	r.addPeer(p)
	log.Printf("room %s (%s): %s joined as %s", r.id, r.mode, p.id, role)
	defer func() {
		r.removePeer(p.id)
		_ = ws.Close()
	}()

	for {
		var m signalMsg
		if err := ws.ReadJSON(&m); err != nil {
			return
		}
		switch m.Type {
		case "offer":
			var sdp webrtc.SessionDescription
			if json.Unmarshal(m.SDP, &sdp) != nil {
				continue
			}
			if err := pc.SetRemoteDescription(sdp); err != nil {
				continue
			}
			answer, err := pc.CreateAnswer(nil)
			if err != nil || pc.SetLocalDescription(answer) != nil {
				continue
			}
			raw, _ := json.Marshal(answer)
			p.send(signalMsg{Type: "answer", SDP: raw})
		case "answer":
			var sdp webrtc.SessionDescription
			if json.Unmarshal(m.SDP, &sdp) == nil {
				_ = pc.SetRemoteDescription(sdp)
			}
		case "ice":
			var c webrtc.ICECandidateInit
			if json.Unmarshal(m.Candidate, &c) == nil {
				_ = pc.AddICECandidate(c)
			}
		}
	}
}

// ---- internal API (called by the ChatApp API service) ----

func internalAuth(req *http.Request) bool {
	return req.Header.Get("Authorization") == "Bearer "+sfuSecret
}

func createRoomHandler(w http.ResponseWriter, req *http.Request) {
	if !internalAuth(req) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var body struct {
		RoomID string `json:"room_id"`
		ConvID string `json:"conversation_id"`
		Mode   string `json:"mode"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.RoomID == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	rooms.getOrCreate(body.RoomID, body.ConvID, body.Mode)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"room_id": body.RoomID, "status": "ok"})
}

// statsHandler reports per-room, per-peer quality telemetry for the ops
// surface: participant counts, ICE states, session durations and relayed
// byte/packet totals. Internal-only (shared-secret bearer).
func statsHandler(w http.ResponseWriter, req *http.Request) {
	if !internalAuth(req) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	type peerStats struct {
		ID             string `json:"id"`
		Role           string `json:"role"`
		ICEState       string `json:"ice_state"`
		ConnectedFor   int    `json:"connected_for_seconds"`
		BytesRelayed   int64  `json:"bytes_relayed"`
		PacketsRelayed int64  `json:"packets_relayed"`
	}
	type roomStats struct {
		Mode        string      `json:"mode"`
		Peers       []peerStats `json:"peers"`
		TrackCount  int         `json:"track_count"`
		EmptySeconds int        `json:"empty_seconds"`
	}
	out := map[string]any{"rooms": []roomStats{}}
	rooms.mu.Lock()
	for id, r := range rooms.rooms {
		r.mu.Lock()
		rs := roomStats{Mode: r.mode, TrackCount: len(r.tracks), Peers: []peerStats{}}
		if len(r.peers) == 0 && !r.empty.IsZero() {
			rs.EmptySeconds = int(time.Since(r.empty).Seconds())
		}
		for _, p := range r.peers {
			ps := peerStats{
				ID:             p.id,
				Role:           p.role,
				ICEState:       "new",
				BytesRelayed:   p.bytesRelayed.Load(),
				PacketsRelayed: p.packetsRelayed.Load(),
			}
			if s, ok := p.iceState.Load().(string); ok {
				ps.ICEState = s
			}
			if v, ok := p.connectedAt.Load().(time.Time); ok && !v.IsZero() {
				ps.ConnectedFor = int(time.Since(v).Seconds())
			}
			rs.Peers = append(rs.Peers, ps)
		}
		r.mu.Unlock()
		out["rooms"] = append(out["rooms"].([]roomStats), rs)
		_ = id
	}
	rooms.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func activeLiveHandler(w http.ResponseWriter, req *http.Request) {
	if !internalAuth(req) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	type liveRoom struct {
		RoomID  string `json:"room_id"`
		ConvID  string `json:"conversation_id"`
		Viewers int    `json:"viewers"`
	}
	out := []liveRoom{}
	rooms.mu.Lock()
	for _, r := range rooms.rooms {
		r.mu.Lock()
		if r.mode == "live" && len(r.peers) > 0 {
			out = append(out, liveRoom{RoomID: r.id, ConvID: r.convID, Viewers: len(r.peers) - 1})
		}
		r.mu.Unlock()
	}
	rooms.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"rooms": out})
}

// ---- embedded STUN/TURN ----

func startTURN() {
	udp, err := net.ListenPacket("udp4", "0.0.0.0:"+turnPort)
	if err != nil {
		log.Fatalf("turn listen: %v", err)
	}
	// Standard TURN REST credentials: username "expiry:userid", password is
	// base64(hmac-sha1(TURN_SECRET, username)). The API mints these per session.
	auth := func(username, _ string, _ net.Addr) ([]byte, bool) {
		parts := strings.SplitN(username, ":", 2)
		if len(parts) != 2 {
			return nil, false
		}
		exp, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || time.Now().Unix() > exp {
			return nil, false
		}
		mac := hmac.New(sha1.New, []byte(turnSecret))
		mac.Write([]byte(username))
		return turn.GenerateAuthKey(username, "chatapp", base64.StdEncoding.EncodeToString(mac.Sum(nil))), true
	}
	_, err = turn.NewServer(turn.ServerConfig{
		Realm: "chatapp",
		AuthHandler: func(username, realm string, srcAddr net.Addr) ([]byte, bool) {
			return auth(username, realm, srcAddr)
		},
		ListenerConfigs: []turn.ListenerConfig{},
		PacketConnConfigs: []turn.PacketConnConfig{{
			PacketConn: udp,
			RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{
				RelayAddress: net.ParseIP(publicIP),
				Address:      "0.0.0.0",
			},
		}},
	})
	if err != nil {
		log.Fatalf("turn server: %v", err)
	}
	log.Printf("STUN/TURN listening on udp/%s (public %s)", turnPort, publicIP)
}

func main() {
	if len(sfuSecret) < 32 || len(turnSecret) < 32 {
		log.Fatal("SFU_SECRET and TURN_SECRET must each contain at least 32 random bytes")
	}
	go rooms.reap()
	go startTURN()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wsHandler)
	mux.HandleFunc("POST /internal/rooms", createRoomHandler)
	mux.HandleFunc("GET /internal/live", activeLiveHandler)
	mux.HandleFunc("GET /internal/stats", statsHandler)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	log.Printf("SFU signaling listening on %s", listenAddr)
	log.Fatal(http.ListenAndServe(listenAddr, mux))
}
