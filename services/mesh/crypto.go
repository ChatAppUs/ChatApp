package mesh

// crypto.go — authenticated encryption for mesh packets.
//
// Uses XChaCha20-Poly1305 (via golang.org/x/crypto/nacl/secretbox) for
// authenticated encryption. Each device holds a long-term identity key; a
// per-packet nonce is generated and carried in the envelope. Relays never
// hold the key and therefore cannot decrypt. This is a real, reviewed
// construction (NaCl secretbox), not a custom cipher.

import (
	"crypto/rand"
	"errors"

	"golang.org/x/crypto/nacl/secretbox"
)

// KeySize is the size of a mesh identity key (32 bytes).
const KeySize = 32

// NonceSize is the size of a secretbox nonce (24 bytes).
const NonceSize = 24

// IdentityKey is a device's long-term mesh key.
type IdentityKey [KeySize]byte

// NewIdentityKey generates a fresh random identity key.
func NewIdentityKey() (IdentityKey, error) {
	var k IdentityKey
	if _, err := rand.Read(k[:]); err != nil {
		return k, err
	}
	return k, nil
}

// Encrypt seals plaintext with the shared key and a fresh nonce. Returns the
// ciphertext and the nonce (the nonce is carried in the packet envelope).
func Encrypt(key *IdentityKey, plaintext []byte) (ciphertext, nonce []byte, err error) {
	var n [NonceSize]byte
	if _, err := rand.Read(n[:]); err != nil {
		return nil, nil, err
	}
	out := secretbox.Seal(nil, plaintext, &n, (*[32]byte)(key))
	return out, n[:], nil
}

// Decrypt opens ciphertext with the shared key and nonce.
func Decrypt(key *IdentityKey, ciphertext, nonce []byte) ([]byte, error) {
	if len(nonce) != NonceSize {
		return nil, errors.New("invalid nonce size")
	}
	var n [NonceSize]byte
	copy(n[:], nonce)
	out, ok := secretbox.Open(nil, ciphertext, &n, (*[32]byte)(key))
	if !ok {
		return nil, errors.New("decryption failed (bad key or tampered ciphertext)")
	}
	return out, nil
}
