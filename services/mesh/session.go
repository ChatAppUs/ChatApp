package mesh

// session.go — per-peer session keys for unicast mesh payloads.
//
// The original payload AEAD used one pre-shared key, so any device that ever
// held the key could decrypt (and forge) every device's traffic. This file
// replaces that for unicast traffic: each device generates an X25519
// key-agreement pair (standard library only), advertises the public key
// inside its Ed25519-signed beacon, and both sides of a conversation derive
// the same AES-256 session key via ECDH + HKDF. A device that never held a
// peer's private key cannot derive the session key, closing the
// shared-key model's biggest hole. Rotation is epoch-based: Rotate()
// regenerates the pair, beacons advertise the new key, and peers pin the
// newest epoch.

import (
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
)

// sessionInfo binds derived keys to this protocol and version.
const sessionInfo = "chatapp-mesh-session-v2"

// AdvertisedKEM is a peer's signed key-agreement advertisement (from a
// signed beacon).
type AdvertisedKEM struct {
	Public []byte
	Epoch  int64
}

// KeyExchange holds an X25519 key-agreement pair and its epoch. The public
// half is advertised in signed beacons; session keys are derived pairwise.
type KeyExchange struct {
	Epoch int64
	priv  *ecdh.PrivateKey
}

// NewKeyExchange generates a fresh key-agreement pair at the given epoch.
func NewKeyExchange(epoch int64) *KeyExchange {
	k, err := newKeyExchange(epoch)
	if err != nil {
		// X25519 key generation fails only on a broken entropy source;
		// there is no safe fallback, so the process must not run crypto.
		panic("mesh: key agreement generation failed: " + err.Error())
	}
	return k
}

func newKeyExchange(epoch int64) (*KeyExchange, error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &KeyExchange{Epoch: epoch, priv: priv}, nil
}

// Public returns a copy of the X25519 public key (32 bytes).
func (k *KeyExchange) Public() []byte {
	if k == nil || k.priv == nil {
		return nil
	}
	return append([]byte(nil), k.priv.PublicKey().Bytes()...)
}

// Rotate regenerates the key pair and bumps the epoch.
func (k *KeyExchange) Rotate() error {
	nk, err := newKeyExchange(k.Epoch + 1)
	if err != nil {
		return err
	}
	k.priv = nk.priv
	k.Epoch = nk.Epoch
	return nil
}

// SessionKey derives the unicast AES-256 key shared between a local device
// and a remote peer. Both sides compute the same key: X25519 is symmetric
// and the HKDF salt is the sorted device-id pair, so derivation is
// order-independent.
func SessionKey(local *KeyExchange, localID string, remotePub []byte, remoteID string) (*IdentityKey, error) {
	if local == nil || local.priv == nil {
		return nil, errors.New("mesh: no local key-agreement key")
	}
	if len(remotePub) != 32 {
		return nil, errors.New("mesh: invalid peer key-agreement public key")
	}
	peer, err := ecdh.X25519().NewPublicKey(remotePub)
	if err != nil {
		return nil, errors.New("mesh: invalid peer key-agreement public key")
	}
	shared, err := local.priv.ECDH(peer)
	if err != nil {
		return nil, err
	}
	a, b := localID, remoteID
	if a > b {
		a, b = b, a
	}
	key, err := hkdf.Key(sha256.New, shared, []byte(a+"|"+b), sessionInfo, KeySize)
	if err != nil {
		return nil, err
	}
	out := new(IdentityKey)
	copy(out[:], key)
	return out, nil
}
