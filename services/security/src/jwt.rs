//! HS256 JWT verification — the user-plane token contract shared with the
//! Go API. Rejects anything but alg=HS256 (alg-confusion guard), requires
//! typ=access, rejects admin-scope tokens, and enforces exp.

use crate::sha256::{ct_eq, hmac_sha256};

pub struct JwtClaims {
    pub sub: String,
    pub exp: u64,
}

fn b64url_decode(s: &str) -> Option<Vec<u8>> {
    fn val(c: u8) -> Option<u8> {
        match c {
            b'A'..=b'Z' => Some(c - b'A'),
            b'a'..=b'z' => Some(c - b'a' + 26),
            b'0'..=b'9' => Some(c - b'0' + 52),
            b'-' | b'+' => Some(62),
            b'_' | b'/' => Some(63),
            b'=' => None,
            _ => None,
        }
    }
    let mut out = Vec::new();
    let mut acc: u32 = 0;
    let mut bits = 0u32;
    for c in s.bytes() {
        if c == b'=' {
            break;
        }
        let v = val(c)?;
        acc = (acc << 6) | v as u32;
        bits += 6;
        if bits >= 8 {
            bits -= 8;
            out.push((acc >> bits) as u8);
        }
    }
    Some(out)
}

fn json_str_field(body: &str, key: &str) -> Option<String> {
    let needle = format!("\"{}\":\"", key);
    let start = body.find(&needle)? + needle.len();
    let rest = &body[start..];
    let end = rest.find('"')?;
    Some(rest[..end].to_string())
}

fn json_num_field(body: &str, key: &str) -> Option<u64> {
    let needle = format!("\"{}\":", key);
    let start = body.find(&needle)? + needle.len();
    let rest = body[start..].trim_start();
    let end = rest.find(|c: char| !c.is_ascii_digit()).unwrap_or(rest.len());
    rest[..end].parse().ok()
}

pub fn verify_hs256(token: &str, secret: &[u8], now_secs: u64) -> Option<JwtClaims> {
    let mut parts = token.split('.');
    let header_b64 = parts.next()?;
    let payload_b64 = parts.next()?;
    let sig_b64 = parts.next()?;
    if parts.next().is_some() {
        return None; // must be exactly 3 segments
    }
    let signing_input = format!("{}.{}", header_b64, payload_b64);
    let sig = b64url_decode(sig_b64)?;
    let expected = hmac_sha256(secret, signing_input.as_bytes());
    if !ct_eq(&expected, &sig) {
        return None;
    }
    let header = String::from_utf8(b64url_decode(header_b64)?).ok()?;
    if !header.contains("\"HS256\"") {
        return None; // alg-confusion guard
    }
    let payload = String::from_utf8(b64url_decode(payload_b64)?).ok()?;
    if json_str_field(&payload, "typ").as_deref() != Some("access") {
        return None;
    }
    if json_str_field(&payload, "scope").as_deref() == Some("admin") {
        return None; // admin tokens never validate on the user plane
    }
    let exp = json_num_field(&payload, "exp")?;
    if exp <= now_secs {
        return None;
    }
    Some(JwtClaims {
        sub: json_str_field(&payload, "sub")?,
        exp,
    })
}
