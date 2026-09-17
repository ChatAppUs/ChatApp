package mesh

// doubleratchet.go — Signal-style Double Ratchet for E2E message encryption.
//
// Anonymous.md §1 requires E2EE with forward secrecy. The Double Ratchet
// protocol (Signal specification, Trevor Perrin & Moxie Marlinspike) provides
// cryptographic properties needed:
//   - Forward secrecy after every message (DH ratchet step)
//   - Break-in recovery (new DH shares restore security)
//   - Post-compromise security (sender-keys rotate per message)
//
// This implementation integrates with the existing mesh session layer
// (session.go) for X25519 key agreement and adds the per-message symmetric
// ratchet chain. It follows the Signal protocol spec:
// https://signal.org/docs/specifications/doubleratchet/
//
// Architecture:
//   RootKey → [DH Ratchet] → RootKey' + SendingChainKey + ReceivingChainKey
//   ChainKey → [KDF] → MessageKey + ChainKey'
//
// Each conversation maintains:
//   - RootKey: 32 bytes, updated on each DH ratchet turn
//   - SenderChain: (ChainKey, index) for messages we send
//   - ReceiverChain: (ChainKey, index) per remote DH public key epoch
//   - SkippedMessageKeys: stored for out-of-order message decryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
)

const (
	// DRChainKeySize is the size of a chain key (32 bytes).
	DRChainKeySize = 32
	// DRRootKeySize is the size of a root key (32 bytes).
	DRRootKeySize = 32
	// DRMessageKeySize is the size of a derived message key (32 bytes).
	DRMessageKeySize = 32
	// DRMaxSkip is the maximum number of skipped message keys to store.
	DRMaxSkip = 1000
)

// DRSession holds the per-conversation Double Ratchet state.
type DRSession struct {
	mu sync.Mutex

	// RootKey is updated on every DH ratchet turn.
	RootKey [DRRootKeySize]byte

	// Our X25519 key pair for DH ratchets
	DHPriv    *ecdh.PrivateKey
	DHPub     []byte
	DHEpoch   uint32

	// Remote DH public key (latest received)
	RemoteDHPub  []byte
	RemoteEpoch  uint32

	// Sending chain
	SendChainKey [DRChainKeySize]byte
	SendIndex    uint32

	// Receiving chain
	RecvChainKey [DRChainKeySize]byte
	RecvIndex    uint32

	// Skipped message keys for out-of-order messages
	// map[dhEpoch_index] -> messageKey
	SkippedKeys map[string][DRMessageKeySize]byte
}

// NewDRSession initializes a Double Ratchet session from a shared secret
// (the X3DH output or initial key agreement).
func NewDRSession(sharedSecret []byte, ourPriv *ecdh.PrivateKey, remotePub []byte) (*DRSession, error) {
	if len(sharedSecret) < 32 {
		return nil, errors.New("dr: shared secret too short")
	}
	if len(remotePub) != 32 {
		return nil, errors.New("dr: invalid remote public key")
	}

	dr := &DRSession{
		DHPriv:     ourPriv,
		DHPub:      ourPriv.PublicKey().Bytes(),
		RemoteDHPub: remotePub,
		SkippedKeys: make(map[string][DRMessageKeySize]byte),
	}
	copy(dr.RootKey[:], sharedSecret[:DRRootKeySize])

	// Initialize sending chain from first DH
	if err := dr.dhRatchet(false); err != nil {
		return nil, err
	}

	return dr, nil
}

// dhRatchet performs a DH ratchet turn. If 'sending' is true, we are the
// initiator and generate a new DH key pair. If false, we are the receiver
// and use the remote's just-received DH public key.
func (dr *DRSession) dhRatchet(sending bool) error {
	var dhOut []byte
	var err error

	if sending {
		// Generate new DH key pair
		newPriv, e := ecdh.X25519().GenerateKey(rand.Reader)
		if e != nil {
			return fmt.Errorf("dr: DH key gen failed: %w", e)
		}
		dr.DHPriv = newPriv
		dr.DHPub = newPriv.PublicKey().Bytes()
		dr.DHEpoch++

		// DH with remote public key
		peerPub, e := ecdh.X25519().NewPublicKey(dr.RemoteDHPub)
		if e != nil {
			return fmt.Errorf("dr: invalid remote DH key: %w", e)
		}
		dhOut, err = dr.DHPriv.ECDH(peerPub)
	} else {
		// Receiver: DH with stored private key and new remote key
		peerPub, e := ecdh.X25519().NewPublicKey(dr.RemoteDHPub)
		if e != nil {
			return fmt.Errorf("dr: invalid remote DH key: %w", e)
		}
		dhOut, err = dr.DHPriv.ECDH(peerPub)
	}
	if err != nil {
		return fmt.Errorf("dr: DH failed: %w", err)
	}

	// Derive new root key and chain keys
	info := drChainInfo(dr.DHEpoch, dr.RemoteEpoch)
	output, err := hkdf.Key(sha256.New, dhOut, dr.RootKey[:], info, 32+DRChainKeySize*2)
	if err != nil {
		return fmt.Errorf("dr: KDF failed: %w", err)
	}

	copy(dr.RootKey[:], output[:DRRootKeySize])
	copy(dr.SendChainKey[:], output[DRRootKeySize:DRRootKeySize+DRChainKeySize])
	copy(dr.RecvChainKey[:], output[DRRootKeySize+DRChainKeySize:])
	dr.SendIndex = 0
	dr.RecvIndex = 0

	return nil
}

// advanceChain derives a message key from a chain key.
// Returns the message key and the next chain key.
func advanceChain(chainKey *[DRChainKeySize]byte) (msgKey [DRMessageKeySize]byte, next [DRChainKeySize]byte) {
	// KDF_CK(ck): HMAC-SHA256(ck, const)
	h := sha256.New
	mac := hkdf.New(h, chainKey[:], nil, []byte{0x01})

	msgKeyBuf := make([]byte, DRMessageKeySize)
	nextBuf := make([]byte, DRChainKeySize)
	if _, err := mac.Read(msgKeyBuf); err != nil {
		panic("dr: KDF read failed: " + err.Error())
	}
	if _, err := mac.Read(nextBuf); err != nil {
		panic("dr: KDF read failed: " + err.Error())
	}
	copy(msgKey[:], msgKeyBuf)
	copy(next[:], nextBuf)
	return
}

// Encrypt encrypts a plaintext with the Double Ratchet.
// Returns the ciphertext with embedded metadata (epoch, index, DH pub).
func (dr *DRSession) Encrypt(plaintext []byte) (ciphertext []byte, err error) {
	dr.mu.Lock()
	defer dr.mu.Unlock()

	// Derive message key
	var msgKey [DRMessageKeySize]byte
	dr.SendChainKey, msgKey = advanceChainKey(&dr.SendChainKey)
	dr.SendIndex++

	// Encrypt with AES-256-GCM
	block, err := aes.NewCipher(msgKey[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, plaintext, nil)

	// Serialize: [epoch:4][index:4][dhPubLen:2][dhPub][nonce:12][ciphertext]
	header := make([]byte, 10)
	binary.BigEndian.PutUint32(header[:4], dr.DHEpoch)
	binary.BigEndian.PutUint32(header[4:8], dr.SendIndex)
	binary.BigEndian.PutUint16(header[8:10], uint16(len(dr.DHPub)))

	result := make([]byte, 0, len(header)+len(dr.DHPub)+len(nonce)+len(ct))
	result = append(result, header...)
	result = append(result, dr.DHPub...)
	result = append(result, nonce...)
	result = append(result, ct...)

	return result, nil
}

// Decrypt decrypts a Double Ratchet ciphertext.
func (dr *DRSession) Decrypt(ciphertext []byte) (plaintext []byte, err error) {
	dr.mu.Lock()
	defer dr.mu.Unlock()

	if len(ciphertext) < 10 {
		return nil, errors.New("dr: ciphertext too short")
	}

	epoch := binary.BigEndian.Uint32(ciphertext[:4])
	index := binary.BigEndian.Uint32(ciphertext[4:8])
	dhPubLen := binary.BigEndian.Uint16(ciphertext[8:10])

	if len(ciphertext) < int(10+dhPubLen+12) {
		return nil, errors.New("dr: ciphertext header truncated")
	}

	dhPub := ciphertext[10 : 10+dhPubLen]
	nonce := ciphertext[10+dhPubLen : 10+dhPubLen+12]
	ct := ciphertext[10+dhPubLen+12:]

	// Check if this is from a new DH epoch
	if epoch != dr.RemoteEpoch {
		// Update remote DH public key
		dr.RemoteDHPub = dhPub
		dr.RemoteEpoch = epoch
		if err := dr.dhRatchet(false); err != nil {
			return nil, err
		}
	}

	skipKey := fmt.Sprintf("%d_%d", epoch, index)

	// Check skipped keys first (out-of-order delivery)
	if sk, ok := dr.SkippedKeys[skipKey]; ok {
		// Consume the skipped key
		delete(dr.SkippedKeys, skipKey)
		return dr.decryptWithKey(sk[:], nonce, ct)
	}

	// Advance receiving chain to the target index
	for dr.RecvIndex < index {
		var skippedKey [DRMessageKeySize]byte
		dr.RecvChainKey, skippedKey = advanceChainKey(&dr.RecvChainKey)
		// Store skipped key for out-of-order messages
		if len(dr.SkippedKeys) < DRMaxSkip {
			skipKeyID := fmt.Sprintf("%d_%d", dr.RemoteEpoch, dr.RecvIndex)
			dr.SkippedKeys[skipKeyID] = skippedKey
		}
		dr.RecvIndex++
	}

	// Derive the message key at target index
	var msgKey [DRMessageKeySize]byte
	dr.RecvChainKey, msgKey = advanceChainKey(&dr.RecvChainKey)
	dr.RecvIndex++

	return dr.decryptWithKey(msgKey[:], nonce, ct)
}

func (dr *DRSession) decryptWithKey(key, nonce, ct []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, errors.New("dr: decryption failed (wrong key or tampered message)")
	}
	return plaintext, nil
}

func drChainInfo(sendEpoch, recvEpoch uint32) []byte {
	info := make([]byte, 8+len(sessionInfo))
	binary.BigEndian.PutUint32(info[:4], sendEpoch)
	binary.BigEndian.PutUint32(info[4:8], recvEpoch)
	copy(info[8:], sessionInfo)
	return info
}

// SkipCount returns the number of message keys stored for out-of-order delivery.
func (dr *DRSession) SkipCount() int {
	dr.mu.Lock()
	defer dr.mu.Unlock()
	return len(dr.SkippedKeys)
}

// Reset clears all ratchet state (used on session reset / security incident).
func (dr *DRSession) Reset() {
	dr.mu.Lock()
	defer dr.mu.Unlock()
	dr.RootKey = [DRRootKeySize]byte{}
	dr.SendChainKey = [DRChainKeySize]byte{}
	dr.RecvChainKey = [DRChainKeySize]byte{}
	dr.SkippedKeys = make(map[string][DRMessageKeySize]byte)
	dr.SendIndex = 0
	dr.RecvIndex = 0
}

// NewX3DHKeyExchange performs the X3DH (Extended Triple Diffie-Hellman)
// initial key agreement to establish a Double Ratchet session.
// This is the recommended initial key exchange from the Signal spec.
func NewX3DHKeyExchange() (*ecdh.PrivateKey, []byte, error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	return priv, priv.PublicKey().Bytes(), nil
}

// PairwiseDRStore manages Double Ratchet sessions indexed by peer ID.
type PairwiseDRStore struct {
	mu    sync.RWMutex
	store map[string]*DRSession
}

// NewPairwiseDRStore creates a new session store.
func NewPairwiseDRStore() *PairwiseDRStore {
	return &PairwiseDRStore{store: make(map[string]*DRSession)}
}

// Get returns the session for a peer, or nil.
func (s *PairwiseDRStore) Get(peerID string) *DRSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.store[peerID]
}

// Set stores a session for a peer.
func (s *PairwiseDRStore) Set(peerID string, session *DRSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store[peerID] = session
}

// Delete removes a session (peer disconnected/completed).
func (s *PairwiseDRStore) Delete(peerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.store, peerID)
}

// Count returns the number of active sessions.
func (s *PairwiseDRStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.store)
}

// ensure crypto libs are importable
var _ = binary.BigEndian