package mesh

// packet.go — the encrypted envelope carried hop-to-hop across the mesh.
//
// A packet is the unit of transmission. It carries routing metadata in the
// clear (source, destination, kind, TTL, hops) and an encrypted payload
// (ciphertext + nonce) that only the intended recipient can open. Relays
// forward packets without ever seeing the plaintext.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// PacketKind identifies the type of payload carried in a packet.
type PacketKind string

const (
	// KindMessage is a 1:1 chat message.
	KindMessage PacketKind = "message"
	// KindGroupMessage is a group chat message.
	KindGroupMessage PacketKind = "group_message"
	// KindVoiceMessage is a voice note.
	KindVoiceMessage PacketKind = "voice_message"
	// KindCallSignal is call-control signaling (offer/answer/ice/hangup).
	KindCallSignal PacketKind = "call_signal"
)

// Packet is the encrypted envelope carried hop-to-hop across the mesh.
type Packet struct {
	ID  string `json:"id"`
	Src string `json:"src"`
	Dst string `json:"dst"`
	// HopSrc is the immediate sender of this copy. Src remains the stable
	// origin for quotas/replay, while HopSrc lets a relay derive the inbound
	// hop key without exposing the final destination.
	HopSrc    string     `json:"hop_src,omitempty"`
	GroupID   string     `json:"group_id,omitempty"`
	Kind      PacketKind `json:"kind"`
	TTL       int        `json:"ttl"`
	Hops      int        `json:"hops"`
	Payload   []byte     `json:"payload"`
	Nonce     []byte     `json:"nonce"`
	CreatedAt int64      `json:"created_at"`
	// Onion is a layered envelope. Relays peel exactly one authenticated layer
	// and learn only the next hop; the final destination is revealed only at
	// the last hop.
	Onion      []byte `json:"onion,omitempty"`
	OnionFinal bool   `json:"onion_final,omitempty"`
	// Seq is a per-sender monotonic sequence number used by receivers'
	// replay filter (see replay.go). Zero means the sender predates
	// sequence numbering; those packets are accepted without replay checks.
	Seq int64 `json:"seq,omitempty"`
}

// NewPacket creates a packet with a fresh random id and the given TTL.
func NewPacket(kind PacketKind, src, dst string, ttl int) *Packet {
	return &Packet{
		ID:        newPacketID(),
		Src:       src,
		Dst:       dst,
		Kind:      kind,
		TTL:       ttl,
		CreatedAt: time.Now().UnixMilli(),
	}
}

// Marshal serializes the packet to JSON for transmission.
func (p *Packet) Marshal() ([]byte, error) { return json.Marshal(p) }

// UnmarshalPacket parses a packet from its wire representation.
func UnmarshalPacket(data []byte) (*Packet, error) {
	if len(data) > 2*1024*1024 {
		return nil, errors.New("packet exceeds maximum wire size")
	}
	var p Packet
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// newPacketID returns a random hex id used for duplicate suppression.
func newPacketID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(time.Now().String()))
	}
	return hex.EncodeToString(b)
}
