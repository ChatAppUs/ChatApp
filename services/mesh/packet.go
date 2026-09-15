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
	// KindMessage is a 1:1 chat message.
	KindMessage PacketKind = "message"
	// KindGroupMessage is a group chat message.
	KindGroupMessage PacketKind = "group_message"
	// KindVoiceMessage is a voice note.
	KindVoiceMessage PacketKind = "voice_message"
	// KindCallSignal is call-control signaling (offer/answer/ice/hangup).
	KindCallSignal PacketKind = "call_signal"
	// KindAck is an end-to-end delivery acknowledgement for a reliable
	// transfer (see reliability.go). It carries the acknowledged transfer id
	// in AckFor and no payload, and is drained ahead of data traffic because
	// a delayed acknowledgement stalls a sender's whole retry budget.
	KindAck PacketKind = "ack"
)

// Packet is the encrypted envelope carried hop-to-hop across the mesh.
type Packet struct {
	// Version is the envelope format version. See EnvelopeVersion.
	Version byte   `json:"v,omitempty"`
	ID      string `json:"id"`
	Src     string `json:"src"`
	Dst     string `json:"dst"`
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
	// Fragment metadata (see fragment.go). FragmentTotal <= 1 means the
	// packet carries a complete payload in one datagram and is not part of a
	// fragment group. FragSum is the SHA-256 digest of the complete plaintext,
	// committed by the sender and verified after reassembly.
	FragIndex int    `json:"frag_index,omitempty"`
	FragTotal int    `json:"frag_total,omitempty"`
	FragID    string `json:"frag_id,omitempty"`
	FragSum   []byte `json:"frag_sum,omitempty"`
	// AckFor is the reliable-transfer id this packet acknowledges. It is set
	// only on KindAck packets; every retransmission of a transfer carries the
	// SAME transfer id, so an acknowledgement for any copy settles the
	// transfer even when an earlier copy was lost.
	AckFor string `json:"ack_for,omitempty"`
	// Xfer identifies the reliable transfer this packet belongs to (see
	// reliability.go). Every retransmission of one transfer shares a Xfer
	// value but carries a fresh packet id and AEAD nonce, so intermediate
	// duplicate suppression does not swallow a retry.
	Xfer string `json:"xfer,omitempty"`
}

// EnvelopeVersion is the wire format version stamped on packets this build
// sends. It is the forward-compatibility anchor required by the mesh
// specifications: a receiver can reject a future incompatible envelope instead
// of misparsing it, and a deployment can move the whole fleet to a new format
// deliberately rather than by accident.
//
// Version 0 is reserved for packets from clients that predate versioning; such
// packets are still accepted (see VersionSupported) so a rolling upgrade does
// not partition the mesh.
const EnvelopeVersion byte = 1

// VersionSupported reports whether a received envelope version can be
// processed. Unknown *future* versions fail closed; the legacy zero value is
// accepted for interoperability with pre-versioning peers.
func VersionSupported(v byte) bool { return v <= EnvelopeVersion }

// Validate performs structural checks that must hold before a packet is
// routed, forwarded or delivered. It rejects a future envelope version,
// inconsistent fragment metadata, a missing route, and payloads beyond the
// wire ceiling. It is deliberately cheap and allocation-free.
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
	// A fragmented packet must carry coherent indices and a group id.
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
	// An acknowledgement must name the transfer it settles; a nameless ACK
	// carries no meaning and would only consume relay quota.
	if p.Kind == KindAck && p.AckFor == "" {
		return errors.New("mesh: acknowledgement has no transfer id")
	}
	return nil
}

// NewPacket creates a packet with a fresh random id and the given TTL.
func NewPacket(kind PacketKind, src, dst string, ttl int) *Packet {
	return &Packet{
		Version:   EnvelopeVersion,
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
	// Forward-compatibility gate: an envelope from a newer, incompatible
	// format is rejected here rather than misparsed downstream. The legacy
	// zero value is still accepted so pre-versioning peers keep working.
	if !VersionSupported(p.Version) {
		return nil, errors.New("mesh: unsupported envelope version")
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
