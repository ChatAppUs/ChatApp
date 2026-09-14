package mesh

// signing.go — signed device identity for the offline mesh.
//
// Every device holds an Ed25519 signing key (standard library only — no
// third-party dependency). Discovery beacons are signed so peers can reject
// spoofed device ids. The first public key seen for a device id is pinned
// (trust-on-first-use); a later beacon claiming the same device id with a
// different key is rejected, which blocks impersonation of an already-known
// peer without requiring a central authority.

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
)

// SigningKey is a device's Ed25519 beacon-signing identity.
type SigningKey struct {
	Public  ed25519.PublicKey
	Private ed25519.PrivateKey
}

// NewSigningKey generates a fresh Ed25519 key pair.
func NewSigningKey() (*SigningKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &SigningKey{Public: pub, Private: priv}, nil
}

// SignedBeacon is a discovery beacon plus the signer's public key, the
// signer's X25519 key-agreement advertisement and an Ed25519 signature over
// all three. Signing the KEM advertisement together with the beacon binds the
// session-key exchange to the device identity, so a man-in-the-middle cannot
// substitute its own key-agreement key on a replayed beacon.
type SignedBeacon struct {
	Beacon   Beacon `json:"beacon"`
	PubKey   []byte `json:"pub_key"`
	KEMPub   []byte `json:"kem_pub,omitempty"`
	KEMEpoch int64  `json:"kem_epoch,omitempty"`
	Sig      []byte `json:"sig"`
}

// signedPayload is the canonical, signature-covered projection of a
// SignedBeacon (everything except Sig itself).
type signedPayload struct {
	Beacon   Beacon `json:"beacon"`
	PubKey   []byte `json:"pub_key"`
	KEMPub   []byte `json:"kem_pub,omitempty"`
	KEMEpoch int64  `json:"kem_epoch,omitempty"`
}

func (sb *SignedBeacon) payload() ([]byte, error) {
	return json.Marshal(signedPayload{
		Beacon:   sb.Beacon,
		PubKey:   sb.PubKey,
		KEMPub:   sb.KEMPub,
		KEMEpoch: sb.KEMEpoch,
	})
}

// ErrKeyMismatch is returned when a beacon claims a known device id but is
// signed by a different key than the pinned one.
var ErrKeyMismatch = errors.New("mesh: beacon key does not match pinned device key")

// SignBeacon signs b — together with this device's key-agreement
// advertisement (kemPub/kemEpoch) — with this device's Ed25519 key.
func (k *SigningKey) SignBeacon(b *Beacon, kemPub []byte, kemEpoch int64) (*SignedBeacon, error) {
	sb := &SignedBeacon{
		Beacon:   *b,
		PubKey:   append([]byte(nil), k.Public...),
		KEMPub:   append([]byte(nil), kemPub...),
		KEMEpoch: kemEpoch,
	}
	payload, err := sb.payload()
	if err != nil {
		return nil, err
	}
	sb.Sig = ed25519.Sign(k.Private, payload)
	return sb, nil
}

// MarshalSignedBeacon serializes a signed beacon.
func MarshalSignedBeacon(sb *SignedBeacon) ([]byte, error) { return json.Marshal(sb) }

// UnmarshalSignedBeacon parses a signed beacon.
func UnmarshalSignedBeacon(data []byte) (*SignedBeacon, error) {
	var sb SignedBeacon
	err := json.Unmarshal(data, &sb)
	return &sb, err
}

// VerifySignedBeacon checks the signature and the pinned-key binding for the
// device id. known maps device id -> pinned public key; a nil map disables
// pinning (verification still requires a valid signature). On success the
// (possibly newly pinned) key binding is recorded and the beacon returned.
func VerifySignedBeacon(sb *SignedBeacon, known map[string][]byte) (*Beacon, error) {
	if sb == nil || len(sb.PubKey) != ed25519.PublicKeySize || len(sb.Sig) == 0 {
		return nil, errors.New("mesh: incomplete signed beacon")
	}
	payload, err := sb.payload()
	if err != nil {
		return nil, err
	}
	if !ed25519.Verify(ed25519.PublicKey(sb.PubKey), payload, sb.Sig) {
		return nil, errors.New("mesh: beacon signature verification failed")
	}
	if known != nil && sb.Beacon.DeviceID != "" {
		if prev, ok := known[sb.Beacon.DeviceID]; ok && !bytes.Equal(prev, sb.PubKey) {
			return nil, ErrKeyMismatch
		}
		known[sb.Beacon.DeviceID] = append([]byte(nil), sb.PubKey...)
	}
	return &sb.Beacon, nil
}
