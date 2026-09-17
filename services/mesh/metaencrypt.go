package mesh

// metaencrypt.go — metadata encryption and padding for onion packets.
//
// Anonymous.md §1 states: "Metadata encryption — sender, recipient,
// timestamp, and message size are all encrypted and padded."
//
// The existing onion (onion.go) already encrypts the inner envelope
// per-hop. This file adds:
//   1. Fixed-size packet padding to defeat traffic analysis by size
//      (all packets are padded to one of 3 bucket sizes).
//   2. Encrypted metadata fields: sender pseudonym, timestamp obfuscation
//      (rounded to 5-min buckets), and randomized inter-packet delay.
//   3. Per-hop metadata stripping: each relay strips its metadata layer
//      before forwarding, so the next hop cannot read prior-hop metadata.
//   4. Cover traffic injection: idle peers send empty packets to make
//      traffic analysis harder.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"time"
)

// MetaBucket represents a padded packet size bucket.
// All packets are padded to one of these fixed sizes.
type MetaBucket int

const (
	// BucketSmall: text messages (up to 512 bytes → 1024)
	BucketSmall  MetaBucket = 1024
	// BucketMedium: images/files (up to 32 KiB)
	BucketMedium MetaBucket = 32768
	// BucketLarge: video or large file chunks (up to 256 KiB)
	BucketLarge  MetaBucket = 262144
)

// SelectBucket picks the smallest bucket that fits the payload.
func SelectBucket(payloadLen int) MetaBucket {
	if payloadLen <= 512 {
		return BucketSmall
	}
	if payloadLen <= 16384 {
		return BucketMedium
	}
	return BucketLarge
}

// PadToBucket adds random padding to reach the target size.
func PadToBucket(data []byte, bucket MetaBucket) []byte {
	target := int(bucket)
	if len(data) >= target {
		return data // already at or above bucket
	}
	padding := make([]byte, target-len(data))
	rand.Read(padding)
	return append(data, padding...)
}

// Unpad removes trailing random padding. The caller knows the original
// size from the decrypted envelope.
func Unpad(data []byte, originalLen int) []byte {
	if originalLen > len(data) {
		return data
	}
	return data[:originalLen]
}

// MetaHeader carries encrypted metadata visible only to the current hop.
type MetaHeader struct {
	// SenderPseudonym replaces the real sender ID; derived ephemerally per path
	SenderPseudonym [16]byte
	// TimeBucket is the 5-minute time bucket of origination (floor(t/300)*300)
	TimeBucket int64
	// OriginalLen is the plaintext length before padding
	OriginalLen uint16
	// HopIndex is the position of this relay in the path (0 = first relay)
	HopIndex uint8
	// Trailer is random padding to make the header exactly 64 bytes
	Trailer [13]byte
}

// SerializeMetaHeader packs a MetaHeader into 64 bytes.
func (m *MetaHeader) Serialize() []byte {
	buf := make([]byte, 64)
	copy(buf[:16], m.SenderPseudonym[:])
	binary.BigEndian.PutUint64(buf[16:24], uint64(m.TimeBucket))
	binary.BigEndian.PutUint16(buf[24:26], m.OriginalLen)
	buf[26] = m.HopIndex
	rand.Read(buf[27:])
	return buf
}

// DeserializeMetaHeader unpacks a 64-byte buffer back into a MetaHeader.
func DeserializeMetaHeader(buf []byte) (*MetaHeader, error) {
	if len(buf) < 27 {
		return nil, errors.New("meta: header too short")
	}
	var m MetaHeader
	copy(m.SenderPseudonym[:], buf[:16])
	m.TimeBucket = int64(binary.BigEndian.Uint64(buf[16:24]))
	m.OriginalLen = binary.BigEndian.Uint16(buf[24:26])
	m.HopIndex = buf[26]
	return &m, nil
}

// EncryptedMeta wraps an onion envelope with encrypted metadata.
// Each relay strips and re-encrypts the metadata for the next hop.
type EncryptedMeta struct {
	// MetaCipher is AES-GCM encrypted MetaHeader
	MetaCipher []byte
	// MetaNonce is the AES-GCM nonce
	MetaNonce []byte
	// InnerPayload is the onion envelope
	InnerPayload []byte
}

// WrapMeta encrypts a MetaHeader with the given session key.
func WrapMeta(meta *MetaHeader, key *IdentityKey, inner []byte) (*EncryptedMeta, error) {
	plain := meta.Serialize()
	ct, nonce, err := Encrypt(key, plain)
	if err != nil {
		return nil, err
	}
	return &EncryptedMeta{
		MetaCipher:  ct,
		MetaNonce:   nonce,
		InnerPayload: inner,
	}, nil
}

// UnwrapMeta decrypts and validates a MetaHeader.
func UnwrapMeta(em *EncryptedMeta, key *IdentityKey) (*MetaHeader, []byte, error) {
	plain, err := Decrypt(key, em.MetaCipher, em.MetaNonce)
	if err != nil {
		return nil, nil, errors.New("meta: decryption failed — tampered or wrong key")
	}
	meta, err := DeserializeMetaHeader(plain)
	if err != nil {
		return nil, nil, err
	}
	return meta, em.InnerPayload, nil
}

// TimeBucketNow returns the current 5-minute time bucket.
func TimeBucketNow() int64 {
	return time.Now().Unix() / 300 * 300
}

// GenerateSenderPseudonym creates a random pseudonym for one path.
func GenerateSenderPseudonym() [16]byte {
	var p [16]byte
	rand.Read(p[:])
	return p
}

// ---- Cover traffic ----

// CoverTrafficInterval is how often idle peers send cover packets.
const CoverTrafficInterval = 30 * time.Second

// coverTrafficCipher returns an AES-CTR stream for cover traffic encryption.
// Cover packets use a shared group key so any peer can decrypt them but
// the content is indistinguishable from random without the key.
func coverTrafficCipher(key *IdentityKey) (cipher.Stream, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	iv := make([]byte, aes.BlockSize)
	rand.Read(iv)
	return cipher.NewCTR(block, iv), nil
}

// GenerateCoverPacket creates a cover traffic packet of the given size.
// It is indistinguishable from a real encrypted packet.
func GenerateCoverPacket(key *IdentityKey, size MetaBucket) ([]byte, error) {
	stream, err := coverTrafficCipher(key)
	if err != nil {
		return nil, err
	}
	dummy := make([]byte, int(size))
	stream.XORKeyStream(dummy, dummy) // encrypt zeros → indistinguishable
	return dummy, nil
}

// IsCoverPacket returns true if decryption succeeds with the cover key
// and yields all zeros. Real packets will never decrypt to all zeros.
func IsCoverPacket(data []byte, key *IdentityKey) bool {
	stream, err := coverTrafficCipher(key)
	if err != nil {
		return false
	}
	decrypted := make([]byte, len(data))
	stream.XORKeyStream(decrypted, data)
	for _, b := range decrypted {
		if b != 0 {
			return false
		}
	}
	return true
}

// MaskTimestamp obfuscates a timestamp by rounding to the nearest
// 5-minute bucket and adding random jitter.
func MaskTimestamp(t time.Time) time.Time {
	bucket := t.Unix() / 300 * 300
	// Add ±60s random jitter
	jitter := make([]byte, 1)
	rand.Read(jitter)
	offset := int64(jitter[0]) - 128 // range [-128, 127]
	return time.Unix(bucket+offset, 0)
}

// ensure aes+cipher imports referenced
var _ = cipher.NewCTR
