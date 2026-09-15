package mesh

// packet.go — the encrypted envelope carried hop-to-hop across the mesh.
//
// A packet is the unit of transmission. It carries routing metadata in the
// clear (source, destination, kind, TTL, hops) and an encrypted payload
// (ciphertext + nonce) that only the intended recipient can open. Relays
// forward packets without ever seeing the plaintext.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// PacketKind identifies the type of payload carried in a packet.
type PacketKind string

const (
	KindMessage      PacketKind = "message"
	KindGroupMessage PacketKind = "group_message"
	KindVoiceMessage PacketKind = "voice_message"
	KindCallSignal   PacketKind = "call_signal"
	KindAck          PacketKind = "ack"
)

// Packet is the encrypted envelope carried hop-by-hop across the mesh.
type Packet struct {
	Version byte       `json:"v,omitempty"`
	ID      string     `json:"id"`
	Src     string     `json:"src"`
	Dst     string     `json:"dst"`
	HopSrc  string     `json:"hop_src,omitempty"`
	GroupID string     `json:"group_id,omitempty"`
	Kind    PacketKind `json:"kind"`
	TTL     int        `json:"ttl"`
	Hops    int        `json:"hops"`
	Payload []byte     `json:"payload"`
	Nonce   []byte     `json:"nonce"`
	CreatedAt int64    `json:"created_at"`
	Onion      []byte `json:"onion,omitempty"`
	OnionFinal bool   `json:"onion_final,omitempty"`
	Seq int64 `json:"seq,omitempty"`
	FragIndex int `json:"frag_index,omitempty"`
	FragTotal int `json:"frag_total,omitempty"`
	FragID string `json:"frag_id,omitempty"`
	FragSum []byte `json:"frag_sum,omitempty"`
	AckFor string `json:"ack_for,omitempty"`
	Xfer string `json:"xfer,omitempty"`
}

const EnvelopeVersion byte = 1

func VersionSupported(v byte) bool { return v <= EnvelopeVersion }

func (p *Packet) Validate() error {
	if !VersionSupported(p.Version) {
		return errors.New("mesh: unsupported envelope version")
	}
	if p.ID == "" {
		return errors.New("mesh: packet id is required")
	}
	if p.Src == "" {
		return errors.New("mesh: packet source is required")
	}
	if p.Dst == "" && p.GroupID == "" {
		return errors.New("mesh: packet needs a destination or a group")
	}
	if p.TTL < 0 || p.Hops < 0 {
		return errors.New("mesh: negative ttl or hop count")
	}
	if p.FragTotal > 1 {
		if p.FragID == "" || p.FragIndex < 0 || p.FragIndex >= p.FragTotal {
			return ErrFragmentInvalid
		}
		if len(p.FragSum) != sha256.Size {
			return ErrFragmentInvalid
		}
	} else if p.FragIndex != 0 || p.FragTotal < 0 {
		return ErrFragmentInvalid
	}
	if p.Kind == KindAck && p.AckFor == "" {
		return errors.New("mesh: acknowledgement has no transfer id")
	}
	return nil
}

func NewPacket(kind PacketKind, src, dst string, ttl int) *Packet {
	return &Packet{
		Version: EnvelopeVersion,
		ID: newPacketID(),
		Src: src,
		Dst: dst,
		Kind: kind,
		TTL: ttl,
		CreatedAt: time.Now().UnixMilli(),
	}
}

func (p *Packet) Marshal() ([]byte, error) { return json.Marshal(p) }

func UnmarshalPacket(data []byte) (*Packet, error) {
	if len(data) > 2*1024*1024 {
		return nil, errors.New("packet exceeds maximum wire size")
	}
	var p Packet
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	if !VersionSupported(p.Version) {
		return nil, errors.New("mesh: unsupported envelope version")
	}
	return &p, nil
}

// newPacketID deliberately fails closed if the operating-system CSPRNG fails.
// A timestamp-derived identifier is not a safe substitute: packet IDs feed
// deduplication, transfer correlation and fragment grouping, so predictable IDs
// can enable collision and replay attacks. Callers should not continue after a
// system CSPRNG failure.
func newPacketID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("mesh: cryptographic random source unavailable")
	}
	return hex.EncodeToString(b)
}
