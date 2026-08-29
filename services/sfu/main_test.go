package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

func postTicket(url, body, secret string) (*http.Response, error) {
	r, _ := http.NewRequest("POST", url, bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+secret)
	return http.DefaultClient.Do(r)
}

func testMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wsHandler)
	mux.HandleFunc("POST /internal/rooms", createRoomHandler)
	mux.HandleFunc("GET /internal/live", activeLiveHandler)
	return mux
}

// End-to-end: issue a ticket via the internal API, connect over the
// signaling WebSocket, and complete a full SDP offer/answer negotiation
// with a real Pion peer connection. No mocks.
func TestSFUNegotiation(t *testing.T) {
	sfuSecret = "test-secret"
	httpSrv := httptest.NewServer(testMux())
	defer httpSrv.Close()

	// API plane: register the room, then mint a ticket exactly like the
	// API service does (shared-secret HMAC, no network call).
	resp, err := postTicket(httpSrv.URL+"/internal/rooms",
		`{"room_id":"room-1","conversation_id":"conv-1","mode":"meeting"}`, "test-secret")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("room register failed: %v %v", err, resp)
	}
	resp.Body.Close()
	payload := fmt.Sprintf("%s|%s|%s|%d", "room-1", "alice", "publisher",
		time.Now().Add(time.Hour).Unix())
	mac := hmac.New(sha256.New, []byte("test-secret"))
	mac.Write([]byte(payload))
	ticket := base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." +
		hex.EncodeToString(mac.Sum(nil))

	// Client plane: connect and negotiate.
	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") +
		"/ws?ticket=" + ticket + "&mode=meeting"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer ws.Close()

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("pc: %v", err)
	}
	defer pc.Close()
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		t.Fatalf("transceiver: %v", err)
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatalf("offer: %v", err)
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatalf("set local: %v", err)
	}
	offerJSON, _ := json.Marshal(pc.LocalDescription())
	if err := ws.WriteJSON(signalMsg{Type: "offer", SDP: json.RawMessage(offerJSON)}); err != nil {
		t.Fatalf("send offer: %v", err)
	}

	ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		var msg signalMsg
		if err := ws.ReadJSON(&msg); err != nil {
			t.Fatalf("read: %v", err)
		}
		if msg.Type != "answer" {
			continue
		}
		var answer webrtc.SessionDescription
		if err := json.Unmarshal([]byte(msg.SDP), &answer); err != nil {
			t.Fatalf("bad answer: %v", err)
		}
		if err := pc.SetRemoteDescription(answer); err != nil {
			t.Fatalf("set remote: %v", err)
		}
		break
	}
}

// Invalid tickets must be rejected before any media setup.
func TestSFURejectsBadTicket(t *testing.T) {
	sfuSecret = "test-secret"
	httpSrv := httptest.NewServer(testMux())
	defer httpSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") +
		"/ws?ticket=forged&mode=meeting"
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected rejection for forged ticket")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %v", resp)
	}
}
