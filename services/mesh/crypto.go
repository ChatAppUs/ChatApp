package mesh

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
)

const KeySize = 32
const NonceSize = 12

type IdentityKey [KeySize]byte

func NewIdentityKey() (IdentityKey, error) {
	var k IdentityKey
	if _, err := rand.Read(k[:]); err != nil {
		return k, err
	}
	return k, nil
}

func Encrypt(key *IdentityKey, plaintext []byte) (ciphertext, nonce []byte, err error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return gcm.Seal(nil, nonce, plaintext, nil), nonce, nil
}

func Decrypt(key *IdentityKey, ciphertext, nonce []byte) ([]byte, error) {
	if len(nonce) != NonceSize {
		return nil, errors.New("invalid nonce size")
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.New("decryption failed (bad key or tampered ciphertext)")
	}
	return plaintext, nil
}
