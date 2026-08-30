//! Own-key custody: HKDF-SHA256 key derivation and co-signing.
//!
//! The master seed (CUSTODY_MASTER_SEED) lives only in this process. Callers
//! never receive key material — they receive key fingerprints and HMAC
//! co-signatures, so no other service can unilaterally move funds. Built on
//! the crate's verified SHA-256/HMAC primitives; no external dependencies.

use crate::sha256::{hmac_sha256, to_hex};

fn hkdf_extract(salt: &[u8], ikm: &[u8]) -> [u8; 32] {
    hmac_sha256(salt, ikm)
}

fn hkdf_expand(prk: &[u8; 32], info: &[u8], out_len: usize) -> Vec<u8> {
    let mut out = Vec::with_capacity(out_len);
    let mut prev: Vec<u8> = Vec::new();
    let mut counter: u8 = 1;
    while out.len() < out_len {
        let mut data = Vec::with_capacity(prev.len() + info.len() + 1);
        data.extend_from_slice(&prev);
        data.extend_from_slice(info);
        data.push(counter);
        prev = hmac_sha256(prk, &data).to_vec();
        out.extend_from_slice(&prev);
        counter = counter.wrapping_add(1);
    }
    out.truncate(out_len);
    out
}

/// Deterministic per-(user, purpose) custody key: HKDF(seed, "custody|uid|purpose").
pub fn custody_key(seed: &[u8], uid: &str, purpose: &str) -> [u8; 32] {
    let prk = hkdf_extract(b"chatapp-custody-v1", seed);
    let info = format!("custody|{}|{}", uid, purpose);
    let derived = hkdf_expand(&prk, info.as_bytes(), 32);
    let mut key = [0u8; 32];
    key.copy_from_slice(&derived);
    key
}

/// Public fingerprint of a custody key — safe to return to callers.
pub fn key_fingerprint(key: &[u8; 32]) -> String {
    to_hex(&hmac_sha256(key, b"key-id"))
}

/// Co-signature over a canonical message with the caller's custody key.
pub fn custody_sign(key: &[u8; 32], message: &str) -> String {
    to_hex(&hmac_sha256(key, message.as_bytes()))
}

#[cfg(test)]
mod custody_tests {
    use super::*;

    #[test]
    fn deterministic_and_purpose_isolated() {
        let seed = b"test-seed-at-least-32-bytes-long!!";
        let a1 = custody_key(seed, "user-1", "withdraw");
        let a2 = custody_key(seed, "user-1", "withdraw");
        let b = custody_key(seed, "user-1", "deposit");
        let c = custody_key(seed, "user-2", "withdraw");
        assert_eq!(a1, a2);
        assert_ne!(a1, b);
        assert_ne!(a1, c);
    }

    #[test]
    fn signature_is_stable() {
        let seed = b"test-seed-at-least-32-bytes-long!!";
        let key = custody_key(seed, "user-1", "withdraw");
        let sig1 = custody_sign(&key, "withdraw|id|uid|BTC|bitcoin|addr|1.0|0.001");
        let sig2 = custody_sign(&key, "withdraw|id|uid|BTC|bitcoin|addr|1.0|0.001");
        assert_eq!(sig1, sig2);
        assert_eq!(sig1.len(), 64);
    }
}
