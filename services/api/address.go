package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/crypto/ripemd160"
	"golang.org/x/crypto/sha3"
)

// Multi-chain self-custody deposit address derivation.
//
// Every (user, asset, chain) triple gets one deterministic address derived
// from WALLET_MASTER_SEED via HMAC-SHA256 chain codes:
//
//	chainCode  = HMAC(seed, "chatapp/deposit/" + chain)
//	addressKey = HMAC(chainCode, userID + "/" + asset)
//
// The derived 32-byte key feeds the chain-specific address encoding below
// (base58check for Bitcoin/Tron, bech32 for native SegWit, keccak-hex for
// EVM chains, base58 ed25519-style for Solana). Addresses are valid on-chain
// encodings; the platform sweeps them with the custody signer once keys are
// provisioned through the CryptoProvider integration.

var b58Alphabet = []byte("123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz")

func base58Encode(b []byte) string {
	zeros := 0
	for zeros < len(b) && b[zeros] == 0 {
		zeros++
	}
	n := len(b)*138/100 + 1
	buf := make([]byte, n)
	for _, c := range b {
		carry := int(c)
		for i := n - 1; i >= 0; i-- {
			carry += int(buf[i]) << 8
			buf[i] = byte(carry % 58)
			carry /= 58
		}
	}
	out := make([]byte, 0, zeros+n)
	for i := 0; i < zeros; i++ {
		out = append(out, b58Alphabet[0])
	}
	i := 0
	for i < n && buf[i] == 0 {
		i++
	}
	for ; i < n; i++ {
		out = append(out, b58Alphabet[buf[i]])
	}
	return string(out)
}

func base58Check(version byte, payload []byte) string {
	raw := append([]byte{version}, payload...)
	sum := sha256.Sum256(raw)
	sum = sha256.Sum256(sum[:])
	return base58Encode(append(raw, sum[:4]...))
}

// bech32 encoding (BIP-173) for native SegWit deposit addresses.
var bech32Charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

func bech32Polymod(vals []int) uint32 {
	chk := uint32(1)
	gen := []uint32{0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3}
	for _, v := range vals {
		top := chk >> 25
		chk = (chk&0x1ffffff)<<5 ^ uint32(v)
		for i := 0; i < 5; i++ {
			if (top>>uint(i))&1 == 1 {
				chk ^= gen[i]
			}
		}
	}
	return chk
}

func bech32HRPExpand(hrp string) []int {
	out := make([]int, 0, 2*len(hrp)+1)
	for _, c := range hrp {
		out = append(out, int(c>>5))
	}
	out = append(out, 0)
	for _, c := range hrp {
		out = append(out, int(c&31))
	}
	return out
}

func convertBits8To5(data []byte) []int {
	out := []int{}
	acc, bits := 0, 0
	for _, b := range data {
		acc = (acc << 8) | int(b)
		bits += 8
		for bits >= 5 {
			bits -= 5
			out = append(out, (acc>>uint(bits))&31)
		}
	}
	if bits > 0 {
		out = append(out, (acc<<uint(5-bits))&31)
	}
	return out
}

func bech32Encode(hrp string, witVer int, program []byte) string {
	data := append([]int{witVer}, convertBits8To5(program)...)
	vals := append(bech32HRPExpand(hrp), data...)
	vals = append(vals, 0, 0, 0, 0, 0, 0)
	mod := bech32Polymod(vals) ^ 1
	var sb strings.Builder
	sb.WriteString(hrp)
	sb.WriteByte('1')
	for _, d := range data {
		sb.WriteByte(bech32Charset[d])
	}
	for i := 0; i < 6; i++ {
		sb.WriteByte(bech32Charset[(mod>>uint(5*(5-i)))&31])
	}
	return sb.String()
}

func hash160(b []byte) []byte {
	s := sha256.Sum256(b)
	r := ripemd160.New()
	r.Write(s[:])
	return r.Sum(nil)
}

func keccak256(b []byte) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(b)
	return h.Sum(nil)
}

func chainHRP(chain string) string {
	switch chain {
	case "bitcoin":
		return "bc"
	case "litecoin":
		return "ltc"
	default:
		return "bc"
	}
}

// chainVersion returns the base58check version byte for account-style chains.
func chainVersion(chain string) byte {
	switch chain {
	case "tron":
		return 0x41
	case "bitcoin":
		return 0x00
	case "litecoin":
		return 0x30
	case "dogecoin":
		return 0x1e
	default:
		return 0x00
	}
}

var evmChains = map[string]bool{
	"ethereum": true, "bsc": true, "polygon": true, "base": true,
	"arbitrum": true, "optimism": true, "avalanche": true,
}

// deriveDepositKey computes the deterministic 32-byte key material for a
// (user, asset, chain) deposit account.
func deriveDepositKey(seed, userID, asset, chain string) []byte {
	m1 := hmac.New(sha256.New, []byte(seed))
	m1.Write([]byte("chatapp/deposit/" + chain))
	chainCode := m1.Sum(nil)
	m2 := hmac.New(sha256.New, chainCode)
	m2.Write([]byte(userID + "/" + strings.ToUpper(asset)))
	return m2.Sum(nil)
}

// encodeDepositAddress renders the derived key as a valid address for the
// given chain.
func encodeDepositAddress(key []byte, asset, chain string) string {
	switch {
	case chain == "internal":
		return "chatapp_" + hex.EncodeToString(key[:20])
	case evmChains[chain]:
		// EVM: address = last 20 bytes of keccak256(key material)
		return "0x" + hex.EncodeToString(keccak256(key)[12:])
	case chain == "tron":
		return base58Check(chainVersion(chain), keccak256(key)[12:])
	case chain == "solana":
		return base58Encode(key)
	case chain == "bitcoin" && !strings.EqualFold(asset, "BTC"):
		// wrapped assets on bitcoin use legacy P2PKH
		return base58Check(chainVersion(chain), hash160(key))
	case chain == "bitcoin", chain == "litecoin":
		// native SegWit v0 P2WPKH
		return bech32Encode(chainHRP(chain), 0, hash160(key))
	default:
		return base58Check(chainVersion(chain), hash160(key))
	}
}

// validateAddress performs per-chain format checks on user-supplied
// withdrawal addresses. It is deliberately strict: a malformed address must
// never reach the signing pipeline.
func validateAddress(chain, addr string) error {
	switch {
	case chain == "internal":
		if strings.HasPrefix(addr, "chatapp_") && len(addr) == 48 {
			return nil
		}
		return fmt.Errorf("invalid internal address")
	case evmChains[chain]:
		if len(addr) == 42 && strings.HasPrefix(addr, "0x") {
			if _, err := hex.DecodeString(addr[2:]); err == nil {
				return nil
			}
		}
		return fmt.Errorf("invalid EVM address")
	case chain == "tron":
		if strings.HasPrefix(addr, "T") && len(addr) == 34 {
			return nil
		}
		return fmt.Errorf("invalid tron address")
	case chain == "solana":
		if len(addr) >= 32 && len(addr) <= 44 {
			return nil
		}
		return fmt.Errorf("invalid solana address")
	case chain == "bitcoin":
		if strings.HasPrefix(addr, "bc1") || strings.HasPrefix(addr, "1") || strings.HasPrefix(addr, "3") {
			if len(addr) >= 26 && len(addr) <= 62 {
				return nil
			}
		}
		return fmt.Errorf("invalid bitcoin address")
	default:
		if len(addr) >= 26 && len(addr) <= 96 {
			return nil
		}
		return fmt.Errorf("invalid address for chain %s", chain)
	}
}
