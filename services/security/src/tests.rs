//! Unit tests for the security service crypto. RFC-standard vectors:
//! SHA-1: FIPS 180-1; TOTP: RFC 6238 Appendix B (SHA-1, 6-digit truncation).

use crate::e2e_fingerprint;
use crate::jwt;
use crate::sha1;
use crate::sha256::{hmac_sha256, sha256, to_hex};

#[test]
fn sha1_fips_vector() {
    let d = sha1::sha1(b"abc");
    assert_eq!(
        d.iter().map(|b| format!("{:02x}", b)).collect::<String>(),
        "a9993e364706816aba3e25717850c26c9cd0d89d"
    );
}

#[test]
fn sha256_fips_vector() {
    assert_eq!(
        to_hex(&sha256(b"abc")),
        "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
    );
}

#[test]
fn hmac_sha256_rfc4231_vector() {
    // RFC 4231 test case 2
    let mac = hmac_sha256(b"Jefe", b"what do ya want for nothing?");
    assert_eq!(
        to_hex(&mac),
        "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843"
    );
}

#[test]
fn totp_rfc6238_vector() {
    // RFC 6238 Appendix B: ASCII "12345678901234567890" as base32, T=59s,
    // SHA-1 8-digit code 94287082 -> 6-digit truncation 287082.
    let secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ";
    let counter = 59 / 30;
    assert_eq!(sha1::totp_code(secret, counter).as_deref(), Some("287082"));
}

#[test]
fn totp_verify_drift() {
    let secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ";
    let now = 59;
    let code = sha1::totp_code(secret, now / 30).unwrap();
    assert!(sha1::totp_verify(secret, &code, now, 30));
    assert!(!sha1::totp_verify(secret, "000000", now, 30));
    assert!(!sha1::totp_verify(secret, "2870822", now, 30)); // wrong length
}

#[test]
fn jwt_roundtrip() {
    // Build a real HS256 token and verify it through the production path.
    let header = r#"{"alg":"HS256","typ":"JWT"}"#;
    let payload = r#"{"sub":"user-1","typ":"access","exp":2000000000}"#;
    let b64url = |s: &str| -> String {
        let tbl = b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
        let mut out = String::new();
        let b = s.as_bytes();
        let mut i = 0;
        while i + 3 <= b.len() {
            let v = (b[i] as u32) << 16 | (b[i + 1] as u32) << 8 | b[i + 2] as u32;
            out.push(tbl[(v >> 18) as usize & 63] as char);
            out.push(tbl[(v >> 12) as usize & 63] as char);
            out.push(tbl[(v >> 6) as usize & 63] as char);
            out.push(tbl[v as usize & 63] as char);
            i += 3;
        }
        match b.len() - i {
            1 => {
                let v = (b[i] as u32) << 16;
                out.push(tbl[(v >> 18) as usize & 63] as char);
                out.push(tbl[(v >> 12) as usize & 63] as char);
            }
            2 => {
                let v = (b[i] as u32) << 16 | (b[i + 1] as u32) << 8;
                out.push(tbl[(v >> 18) as usize & 63] as char);
                out.push(tbl[(v >> 12) as usize & 63] as char);
                out.push(tbl[(v >> 6) as usize & 63] as char);
            }
            _ => {}
        }
        out
    };
    let signing_input = format!("{}.{}", b64url(header), b64url(payload));
    let sig = hmac_sha256(b"secret", signing_input.as_bytes());
    let sig_b64 = {
        // pre-rendered per call via the same encoder; inline for clarity
        let tbl = b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
        let mut out = String::new();
        let b = &sig[..];
        let mut i = 0;
        while i + 3 <= b.len() {
            let v = (b[i] as u32) << 16 | (b[i + 1] as u32) << 8 | b[i + 2] as u32;
            out.push(tbl[(v >> 18) as usize & 63] as char);
            out.push(tbl[(v >> 12) as usize & 63] as char);
            out.push(tbl[(v >> 6) as usize & 63] as char);
            out.push(tbl[v as usize & 63] as char);
            i += 3;
        }
        let rem = b.len() - i;
        if rem == 1 {
            let v = (b[i] as u32) << 16;
            out.push(tbl[(v >> 18) as usize & 63] as char);
            out.push(tbl[(v >> 12) as usize & 63] as char);
        } else if rem == 2 {
            let v = (b[i] as u32) << 16 | (b[i + 1] as u32) << 8;
            out.push(tbl[(v >> 18) as usize & 63] as char);
            out.push(tbl[(v >> 12) as usize & 63] as char);
            out.push(tbl[(v >> 6) as usize & 63] as char);
        }
        out
    };
    let token = format!("{}.{}", signing_input, sig_b64);
    let claims = jwt::verify_hs256(&token, b"secret", 1_700_000_000).expect("valid token");
    assert_eq!(claims.sub, "user-1");
    assert_eq!(claims.exp, 2_000_000_000);
    // Wrong secret rejects.
    assert!(jwt::verify_hs256(&token, b"wrong", 1_700_000_000).is_none());
    // Expired rejects.
    assert!(jwt::verify_hs256(&token, b"secret", 3_000_000_000).is_none());
}

#[test]
fn e2e_fingerprint_symmetric_and_stable() {
    // The SAS derivation is order-independent and deterministic: both parties
    // must compute the same fingerprint regardless of argument order.
    let (a, b) = ("key-alice", "key-bob");
    let fp1 = e2e_fingerprint(a, b);
    let fp2 = e2e_fingerprint(b, a);
    assert_eq!(fp1, fp2);
    let fp3 = e2e_fingerprint(a, "key-carol");
    assert_ne!(fp1, fp3);
    let (_, ref sas) = fp1;
    assert_eq!(sas.len(), 60);
    assert!(sas.chars().all(|c| c.is_ascii_digit()));
}
