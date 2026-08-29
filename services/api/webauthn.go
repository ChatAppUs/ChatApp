package main

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
)

// Minimal WebAuthn verification primitives (FIDO2), stdlib only.
// Covers: CBOR decoding (definite-length), COSE key parsing (EC2/RSA/OKP),
// authenticatorData parsing, and assertion signature verification.

// ---------- CBOR ----------

type cborParser struct {
	buf []byte
	pos int
}

func parseCBOR(b []byte) (any, error) {
	p := &cborParser{buf: b}
	v, err := p.value()
	if err != nil {
		return nil, err
	}
	if p.pos != len(b) {
		return nil, errors.New("cbor: trailing bytes")
	}
	return v, nil
}

func (p *cborParser) value() (any, error) {
	if p.pos >= len(p.buf) {
		return nil, errors.New("cbor: unexpected end")
	}
	ib := p.buf[p.pos]
	p.pos++
	major, info := ib>>5, ib&0x1f
	arg, err := p.arg(info)
	if err != nil {
		return nil, err
	}
	switch major {
	case 0:
		return arg, nil
	case 1:
		return -1 - int64(arg), nil
	case 2, 3:
		if arg > uint64(len(p.buf)-p.pos) {
			return nil, errors.New("cbor: length overflow")
		}
		raw := p.buf[p.pos : p.pos+int(arg)]
		p.pos += int(arg)
		if major == 2 {
			return raw, nil
		}
		return string(raw), nil
	case 4:
		arr := make([]any, 0, arg)
		for i := uint64(0); i < arg; i++ {
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
		return arr, nil
	case 5:
		m := make(map[any]any, arg)
		for i := uint64(0); i < arg; i++ {
			k, err := p.value()
			if err != nil {
				return nil, err
			}
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			m[k] = v
		}
		return m, nil
	case 7:
		switch info {
		case 20:
			return false, nil
		case 21:
			return true, nil
		case 22, 23:
			return nil, nil
		}
		return nil, fmt.Errorf("cbor: unsupported simple value %d", info)
	}
	return nil, fmt.Errorf("cbor: unsupported major type %d", major)
}

func (p *cborParser) arg(info byte) (uint64, error) {
	switch {
	case info < 24:
		return uint64(info), nil
	case info == 24:
		if p.pos+1 > len(p.buf) {
			return 0, errors.New("cbor: unexpected end")
		}
		v := uint64(p.buf[p.pos])
		p.pos++
		return v, nil
	case info == 25:
		if p.pos+2 > len(p.buf) {
			return 0, errors.New("cbor: unexpected end")
		}
		v := binary.BigEndian.Uint16(p.buf[p.pos:])
		p.pos += 2
		return uint64(v), nil
	case info == 26:
		if p.pos+4 > len(p.buf) {
			return 0, errors.New("cbor: unexpected end")
		}
		v := binary.BigEndian.Uint32(p.buf[p.pos:])
		p.pos += 4
		return uint64(v), nil
	case info == 27:
		if p.pos+8 > len(p.buf) {
			return 0, errors.New("cbor: unexpected end")
		}
		v := binary.BigEndian.Uint64(p.buf[p.pos:])
		p.pos += 8
		return v, nil
	}
	return 0, fmt.Errorf("cbor: indefinite lengths unsupported (info=%d)", info)
}

// ---------- authenticatorData ----------

type authData struct {
	RPIDHash     []byte
	UserPresent  bool
	UserVerified bool
	SignCount    uint32
	// Attested credential data (registration only)
	AAGUID              []byte
	CredentialID        []byte
	CredentialPublicKey []byte // COSE bytes
}

func parseAuthData(b []byte, requireAttested bool) (*authData, error) {
	if len(b) < 37 {
		return nil, errors.New("authData too short")
	}
	ad := &authData{
		RPIDHash:  b[:32],
		SignCount: binary.BigEndian.Uint32(b[33:37]),
	}
	flags := b[32]
	ad.UserPresent = flags&0x01 != 0
	ad.UserVerified = flags&0x04 != 0
	attested := flags&0x40 != 0
	if requireAttested {
		if !attested {
			return nil, errors.New("attested credential data missing")
		}
		if len(b) < 37+16+2 {
			return nil, errors.New("authData attestation truncated")
		}
		ad.AAGUID = b[37:53]
		credLen := int(binary.BigEndian.Uint16(b[53:55]))
		if len(b) < 55+credLen {
			return nil, errors.New("credential id truncated")
		}
		ad.CredentialID = b[55 : 55+credLen]
		// The COSE key is the final CBOR item; use the parser to find its span.
		rest := b[55+credLen:]
		p := &cborParser{buf: rest}
		if _, err := p.value(); err != nil {
			return nil, fmt.Errorf("credential public key: %w", err)
		}
		ad.CredentialPublicKey = rest[:p.pos]
	}
	return ad, nil
}

func (ad *authData) verifyRP(rpID string) error {
	want := sha256.Sum256([]byte(rpID))
	if string(ad.RPIDHash) != string(want[:]) {
		return errors.New("rpIdHash mismatch")
	}
	if !ad.UserPresent {
		return errors.New("user presence flag not set")
	}
	return nil
}

// ---------- COSE keys ----------

// coseGet resolves a COSE label regardless of whether the CBOR decoder
// produced uint64 (non-negative) or int64 (negative) map keys.
func coseGet(m map[any]any, k int64) (any, bool) {
	if k >= 0 {
		if v, ok := m[uint64(k)]; ok {
			return v, true
		}
	}
	v, ok := m[k]
	return v, ok
}

func coseInt(m map[any]any, k int64) (int64, bool) {
	v, ok := coseGet(m, k)
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case uint64:
		return int64(n), true
	case int64:
		return n, true
	}
	return 0, false
}

func coseBytes(m map[any]any, k int64) ([]byte, bool) {
	v, ok := coseGet(m, k)
	if !ok {
		return nil, false
	}
	b, ok := v.([]byte)
	return b, ok
}

// verifyCOSESignature verifies sig over data using a COSE-encoded public key.
// Supports ES256 (EC2/P-256), RS256 (RSA), and EdDSA (OKP/Ed25519).
func verifyCOSESignature(coseKey, data, sig []byte) error {
	v, err := parseCBOR(coseKey)
	if err != nil {
		return fmt.Errorf("cose parse: %w", err)
	}
	m, ok := v.(map[any]any)
	if !ok {
		return errors.New("cose key is not a map")
	}
	kty, _ := coseInt(m, 1)
	alg, _ := coseInt(m, 3)
	switch {
	case kty == 2 && alg == -7: // EC2, ES256
		crv, _ := coseInt(m, -1)
		if crv != 1 {
			return errors.New("unsupported EC2 curve")
		}
		x, ok1 := coseBytes(m, -2)
		y, ok2 := coseBytes(m, -3)
		if !ok1 || !ok2 {
			return errors.New("missing EC2 coordinates")
		}
		pub := &ecdsa.PublicKey{
			Curve: elliptic.P256(),
			X:     new(big.Int).SetBytes(x),
			Y:     new(big.Int).SetBytes(y),
		}
		digest := sha256.Sum256(data)
		if !ecdsa.VerifyASN1(pub, digest[:], sig) {
			return errors.New("invalid ES256 signature")
		}
		return nil
	case kty == 3 && alg == -257: // RSA, RS256
		n, ok1 := coseBytes(m, -1)
		e, ok2 := coseBytes(m, -2)
		if !ok1 || !ok2 {
			return errors.New("missing RSA parameters")
		}
		exp := 0
		for _, b := range e {
			exp = exp<<8 | int(b)
		}
		pub := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exp}
		digest := sha256.Sum256(data)
		return rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sig)
	case kty == 1 && alg == -8: // OKP, EdDSA
		crv, _ := coseInt(m, -1)
		if crv != 6 {
			return errors.New("unsupported OKP curve")
		}
		x, ok := coseBytes(m, -2)
		if !ok || len(x) != ed25519.PublicKeySize {
			return errors.New("bad Ed25519 key")
		}
		if !ed25519.Verify(ed25519.PublicKey(x), data, sig) {
			return errors.New("invalid EdDSA signature")
		}
		return nil
	}
	return fmt.Errorf("unsupported COSE key (kty=%d alg=%d)", kty, alg)
}
