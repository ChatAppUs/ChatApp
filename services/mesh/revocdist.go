package mesh

// revocdist.go — network-wide distribution of revocation decisions.
//
// Local revocation (node.go RevokePeer) rejects a peer on one device, but the
// decision stayed local: every other node would keep trusting the revoked
// identity until it independently revoked it. This file adds a signed
// revocation notice that a node can flood to the mesh so a revocation
// propagates network-wide. The notice is signed by the revoking node's
// Ed25519 key and names the revoked device id and its pinned public key, so a
// peer cannot forge a revocation for an identity it does not control and
// cannot revoke a different key than the one pinned.

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// RevocationNotice is a signed, network-floodable revocation decision.
type RevocationNotice struct {
	// Revoker is the device id issuing the revocation.
	Revoker string `json:"revoker"`
	// DeviceID is the revoked device id.
	DeviceID string `json:"device_id"`
	// PublicKey is the revoked pinned Ed25519 public key.
	PublicKey []byte `json:"public_key"`
	// IssuedAt is a Unix millisecond timestamp.
	IssuedAt int64 `json:"issued_at"`
	// Sig is the revoker's Ed25519 signature over the notice payload.
	Sig []byte `json:"sig"`
}

// revocationPayload is the canonical, signature-covered projection of a
// RevocationNotice (everything except Sig).
type revocationPayload struct {
	Revoker   string `json:"revoker"`
	DeviceID  string `json:"device_id"`
	PublicKey []byte `json:"public_key"`
	IssuedAt  int64  `json:"issued_at"`
}

func (r *RevocationNotice) payload() ([]byte, error) {
	return json.Marshal(revocationPayload{
		Revoker:   r.Revoker,
		DeviceID:  r.DeviceID,
		PublicKey: r.PublicKey,
		IssuedAt:  r.IssuedAt,
	})
}

// SignRevocation signs a revocation notice with the given Ed25519 key.
func SignRevocation(sk *SigningKey, revoker, deviceID string, pub []byte) (*RevocationNotice, error) {
	if sk == nil || revoker == "" || deviceID == "" || len(pub) != ed25519.PublicKeySize {
		return nil, errors.New("mesh: incomplete revocation notice")
	}
	r := &RevocationNotice{
		Revoker:   revoker,
		DeviceID:  deviceID,
		PublicKey: append([]byte(nil), pub...),
		IssuedAt:  time.Now().UnixMilli(),
	}
	payload, err := r.payload()
	if err != nil {
		return nil, err
	}
	r.Sig = ed25519.Sign(sk.Private, payload)
	return r, nil
}

// VerifyRevocation checks a revocation notice's signature and that it names a
// key matching the pinned identity for the revoked device. known maps device
// id -> pinned public key. It returns the notice on success.
func VerifyRevocation(r *RevocationNotice, known map[string][]byte) (*RevocationNotice, error) {
	if r == nil || r.Revoker == "" || r.DeviceID == "" || len(r.PublicKey) != ed25519.PublicKeySize || len(r.Sig) == 0 {
		return nil, errors.New("mesh: incomplete revocation notice")
	}
	payload, err := r.payload()
	if err != nil {
		return nil, err
	}
	// The revoker must be a known, pinned peer (we can only trust a
	// revocation from an identity we have authenticated).
	revokerPub, ok := known[r.Revoker]
	if !ok {
		return nil, errors.New("mesh: revocation from unknown revoker")
	}
	if !ed25519.Verify(ed25519.PublicKey(revokerPub), payload, r.Sig) {
		return nil, errors.New("mesh: revocation signature verification failed")
	}
	// The revoked key must match the pinned identity for the device, so a
	// revoker cannot revoke a different key than the one the mesh trusts.
	if prev, ok := known[r.DeviceID]; ok && !bytes.Equal(prev, r.PublicKey) {
		return nil, errors.New("mesh: revocation names a key that does not match the pinned identity")
	}
	return r, nil
}

// MarshalRevocation serializes a revocation notice.
func MarshalRevocation(r *RevocationNotice) ([]byte, error) { return json.Marshal(r) }

// UnmarshalRevocation parses a revocation notice.
func UnmarshalRevocation(data []byte) (*RevocationNotice, error) {
	var r RevocationNotice
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// revocationStore tracks revocation notices this node has accepted, so a
// flooded notice is applied once and not re-applied.
type revocationStore struct {
	mu      sync.Mutex
	notices map[string]RevocationNotice
}

func newRevocationStore() *revocationStore {
	return &revocationStore{notices: make(map[string]RevocationNotice)}
}

// Apply records a verified revocation notice for a device. It returns true if
// this is the first time the device was revoked (so the caller reacts once).
func (s *revocationStore) Apply(r *RevocationNotice) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.notices[r.DeviceID]; ok {
		return false
	}
	s.notices[r.DeviceID] = *r
	return true
}

// IsRevoked reports whether a device has a recorded revocation notice.
func (s *revocationStore) IsRevoked(deviceID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.notices[deviceID]
	return ok
}
