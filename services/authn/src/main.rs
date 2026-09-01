// ChatApp authn — Rust trust-critical authentication core.
//
// Owns the P0 surfaces from docs/RUST_CONVERSION_PLAN.md: argon2id password
// hashing/verify (PHC format, m=65536 t=3 p=2), HS256 JWT mint/verify of
// ChatApp claims (sub/typ/scope/exp/iat), RFC 6238 TOTP with +-1 drift,
// the self-built 6-digit OTP engine (unbiased crypto-random codes, salted
// SHA-256), CSPRNG token minting, and the generic HMAC-SHA256 primitive the
// callers use for SFU tickets and withdrawal signatures. The audit trail in
// RUST_CONVERSION_PLAN treats this conversion as hardening for exactly this
// code.
//
// Wire semantics intentionally mirror services/api's Go implementations so
// tokens and passcodes stay interchangeable across services.
//
// All routes require Authorization: Bearer <AUTHN_SECRET> (except /health).
// The service refuses to boot when the secret is unset or when APP_ENV is
// production while a secret is under 32 chars.
//
// Endpoints:
//   POST /password/hash   {password}              -> {hash}
//   POST /password/verify {password,hash}         -> {ok}
//   POST /jwt/mint        {sub,typ,scope,exp,iat} -> {token}
//   POST /jwt/verify      {token}                 -> {claims}
//   POST /totp/generate   {}                      -> {secret}
//   POST /totp/verify     {secret,code}           -> {ok}
//   POST /otp/generate    {}                      -> {code,salt,hash}
//   POST /otp/hash        {salt,code}             -> {hash}
//   POST /random          {bytes}                 -> {token}
//   POST /hmac            {key,message}           -> {signature}
//
// Env: AUTHN_PORT (default 8400), AUTHN_SECRET (required),
//      JWT_SECRET (for mint/verify), APP_ENV (production gate).

use argon2::{Algorithm, Argon2, Params, Version};
use base64::engine::general_purpose::{STANDARD_NO_PAD, URL_SAFE_NO_PAD};
use base64::Engine as _;
use hmac::{Mac, SimpleHmac};
use sha1::Sha1;
use sha2::{Digest, Sha256};
use std::io::{Read, Write};
use std::net::{TcpListener, TcpStream};
use std::sync::OnceLock;
use std::time::{SystemTime, UNIX_EPOCH};

// ---- tiny helpers ----

fn getenv(k: &str, def: &str) -> String {
    std::env::var(k).unwrap_or_else(|_| def.to_string())
}

fn now_unix() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}

fn hex(b: &[u8]) -> String {
    const T: &[u8; 16] = b"0123456789abcdef";
    let mut s = String::with_capacity(b.len() * 2);
    for &c in b {
        s.push(T[(c >> 4) as usize] as char);
        s.push(T[(c & 15) as usize] as char);
    }
    s
}

fn rand_bytes(n: usize) -> Vec<u8> {
    let mut f = std::fs::File::open("/dev/urandom").expect("open /dev/urandom");
    let mut b = vec![0u8; n];
    f.read_exact(&mut b).expect("read /dev/urandom");
    b
}

// Fixed-shape string extraction; control payloads are small and ASCII keys.
fn json_get(js: &str, key: &str) -> Option<String> {
    let pat = format!("\"{}\"", key);
    let k = js.find(&pat)?;
    let colon = js[(k + pat.len())..].find(':')?;
    let base = k + pat.len() + colon + 1;
    let q1 = js[base..].find('"')?;
    let bytes = js.as_bytes();
    let mut out = String::new();
    let mut i = base + q1 + 1;
    while i < bytes.len() {
        let ch = bytes[i];
        if ch == b'\\' && i + 1 < bytes.len() {
            let n = bytes[i + 1];
            let push = match n {
                b'n' => '\n',
                b't' => '\t',
                b'r' => '\r',
                other => other as char,
            };
            out.push(push);
            i += 2;
            continue;
        }
        if ch == b'"' {
            break;
        }
        out.push(ch as char);
        i += 1;
    }
    Some(out)
}

fn json_get_num(js: &str, key: &str) -> Option<i64> {
    let pat = format!("\"{}\"", key);
    let k = js.find(&pat)?;
    let colon = js[(k + pat.len())..].find(':')?;
    let base = k + pat.len() + colon + 1;
    let bytes = js[base..].as_bytes();
    let mut i = 0usize;
    // tolerate whitespace between ':' and the value (Go and Python both emit
    // spaced JSON)
    while i < bytes.len() && bytes[i].is_ascii_whitespace() {
        i += 1;
    }
    let mut neg = false;
    if i < bytes.len() && bytes[i] == b'-' {
        neg = true;
        i += 1;
    }
    let mut v: i64 = 0;
    let mut any = false;
    while i < bytes.len() && bytes[i].is_ascii_digit() {
        v = v
            .saturating_mul(10)
            .saturating_add((bytes[i] - b'0') as i64);
        any = true;
        i += 1;
    }
    if !any {
        return None;
    }
    Some(if neg { -v } else { v })
}

fn json_escape(s: &str) -> String {
    let mut out = String::with_capacity(s.len() + 2);
    for c in s.chars() {
        match c {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            c if (c as u32) < 0x20 => out.push_str(&format!("\\u{:04x}", c as u32)),
            c => out.push(c),
        }
    }
    out
}

// RFC 4648 base32 (uppercase, no padding) — matches Go's base32 StdEncoding.
fn b32_encode(data: &[u8]) -> String {
    const ALPH: &[u8; 32] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
    let mut out = String::with_capacity((data.len() * 8 + 4) / 5);
    let mut acc: u64 = 0;
    let mut bits = 0u32;
    for &b in data {
        acc = (acc << 8) | b as u64;
        bits += 8;
        while bits >= 5 {
            bits -= 5;
            out.push(ALPH[((acc >> bits) & 31) as usize] as char);
        }
    }
    if bits > 0 {
        out.push(ALPH[((acc << (5 - bits)) & 31) as usize] as char);
    }
    out
}

fn b32_decode(s: &str) -> Option<Vec<u8>> {
    let up = s.to_uppercase();
    let mut acc: u64 = 0;
    let mut bits = 0u32;
    let mut out = Vec::new();
    for c in up.bytes() {
        let v = match c {
            b'A'..=b'Z' => c - b'A',
            b'2'..=b'7' => c - b'2' + 26,
            _ => return None,
        };
        acc = (acc << 5) | v as u64;
        bits += 5;
        if bits >= 8 {
            bits -= 8;
            out.push(((acc >> bits) & 0xff) as u8);
        }
    }
    Some(out)
}

static JWT_SECRET: OnceLock<Vec<u8>> = OnceLock::new();

fn hmac_sha256(key: &[u8], msg: &[u8]) -> Vec<u8> {
    let mut m = SimpleHmac::<Sha256>::new_from_slice(key).expect("hmac accepts any key");
    m.update(msg);
    m.finalize().into_bytes().to_vec()
}

fn hmac_sha1(key: &[u8], msg: &[u8]) -> Vec<u8> {
    let mut m = SimpleHmac::<Sha1>::new_from_slice(key).expect("hmac accepts any key");
    m.update(msg);
    m.finalize().into_bytes().to_vec()
}

// Constant-time equality (same contract as Go's subtle.ConstantTimeCompare).
fn ct_eq(a: &[u8], b: &[u8]) -> bool {
    if a.len() != b.len() {
        return false;
    }
    let mut diff = 0u8;
    for i in 0..a.len() {
        diff |= a[i] ^ b[i];
    }
    diff == 0
}

// ---- password: argon2id PHC ($argon2id$v=19$m=65536,t=3,p=2$salt$key) ----

const ARGON_MEM: u32 = 65536;
const ARGON_TIME: u32 = 3;
const ARGON_THREADS: u32 = 2;
const ARGON_OUT: usize = 32;

fn password_hash(password: &str) -> String {
    let salt = rand_bytes(16);
    let params = Params::new(ARGON_MEM, ARGON_TIME, ARGON_THREADS, Some(ARGON_OUT))
        .expect("valid argon2 params");
    let a = Argon2::new(Algorithm::Argon2id, Version::V0x13, params);
    let mut out = [0u8; ARGON_OUT];
    a.hash_password_into(password.as_bytes(), &salt, &mut out)
        .expect("argon2 derive");
    format!(
        "$argon2id$v=19$m=65536,t=3,p=2${}${}",
        STANDARD_NO_PAD.encode(salt),
        STANDARD_NO_PAD.encode(out)
    )
}

fn password_verify(password: &str, encoded: &str) -> bool {
    let parts: Vec<&str> = encoded.split('$').collect();
    if parts.len() != 6 || parts[1] != "argon2id" {
        return false;
    }
    let (mut mem, mut time, mut threads) = (0u32, 0u32, 0u32);
    for kv in parts[3].split(',') {
        let (k, v) = match kv.split_once('=') {
            Some(kv) => kv,
            None => return false,
        };
        let n: u32 = match v.parse() {
            Ok(n) => n,
            Err(_) => return false,
        };
        match k {
            "m" => mem = n,
            "t" => time = n,
            "p" => threads = n,
            _ => return false,
        }
    }
    let salt = match STANDARD_NO_PAD.decode(parts[4]) {
        Ok(s) => s,
        Err(_) => return false,
    };
    let want = match STANDARD_NO_PAD.decode(parts[5]) {
        Ok(w) => w,
        Err(_) => return false,
    };
    if want.is_empty() || want.len() > 64 {
        return false;
    }
    let params = match Params::new(mem, time, threads, Some(want.len())) {
        Ok(p) => p,
        Err(_) => return false,
    };
    let a = Argon2::new(Algorithm::Argon2id, Version::V0x13, params);
    let mut got = vec![0u8; want.len()];
    if a.hash_password_into(password.as_bytes(), &salt, &mut got).is_err() {
        return false;
    }
    ct_eq(&got, &want)
}

// ---- JWT HS256 over ChatApp claims (signature verified before parsing) ----

fn b64url(b: &[u8]) -> String {
    URL_SAFE_NO_PAD.encode(b)
}

fn b64url_dec(s: &str) -> Option<Vec<u8>> {
    URL_SAFE_NO_PAD.decode(s).ok()
}

fn jwt_mint(sub: &str, typ: &str, scope: &str, exp: i64, iat: i64) -> String {
    let header = b64url(b"{\"alg\":\"HS256\",\"typ\":\"JWT\"}");
    // Serialize in the Go struct field order: sub, typ, scope?, exp, iat.
    let mut payload = format!("{{\"sub\":\"{}\",\"typ\":\"{}\"", json_escape(sub), json_escape(typ));
    if !scope.is_empty() {
        payload.push_str(&format!(",\"scope\":\"{}\"", json_escape(scope)));
    }
    payload.push_str(&format!(",\"exp\":{},\"iat\":{}}}", exp, iat));
    let body = format!("{}.{}", header, b64url(payload.as_bytes()));
    let sig = hmac_sha256(JWT_SECRET.get().expect("jwt secret"), body.as_bytes());
    format!("{}.{}", body, b64url(&sig))
}

fn jwt_verify(token: &str) -> Option<String> {
    let mut parts = token.split('.');
    let h = parts.next()?;
    let p = parts.next()?;
    let s = parts.next()?;
    if parts.next().is_some() {
        return None;
    }
    let expected = hmac_sha256(JWT_SECRET.get().expect("jwt secret"), format!("{}.{}", h, p).as_bytes());
    let got = b64url_dec(s)?;
    if !ct_eq(&expected, &got) {
        return None;
    }
    let payload = String::from_utf8(b64url_dec(p)?).ok()?;
    match json_get_num(&payload, "exp") {
        Some(exp) if exp > now_unix() => Some(payload),
        _ => None,
    }
}

// ---- TOTP (RFC 6238, 30s period, 6 digits, one-step drift tolerance) ----

fn totp_code(secret_bytes: &[u8], counter: u64) -> String {
    let mac = hmac_sha1(secret_bytes, &counter.to_be_bytes());
    let offset = (mac[mac.len() - 1] & 0x0f) as usize;
    let code = ((mac[offset] as u32 & 0x7f) << 24)
        | ((mac[offset + 1] as u32) << 16)
        | ((mac[offset + 2] as u32) << 8)
        | (mac[offset + 3] as u32);
    format!("{:06}", code % 1_000_000)
}

fn totp_generate() -> String {
    b32_encode(&rand_bytes(20))
}

fn totp_verify(secret: &str, code: &str) -> bool {
    if code.len() != 6 {
        return false;
    }
    let key = match b32_decode(secret) {
        Some(k) => k,
        None => return false,
    };
    let counter = (now_unix() / 30) as u64;
    for drift in [0i64, -1, 1] {
        let c = counter as i64 + drift;
        if c < 0 {
            continue;
        }
        let expected = totp_code(&key, c as u64);
        if ct_eq(expected.as_bytes(), code.as_bytes()) {
            return true;
        }
    }
    false
}

// ---- OTP engine (unbiased 6-digit codes, salted SHA-256) ----

fn otp_generate() -> (String, String, String) {
    // Unbiased 6-digit code via rejection sampling over the CSPRNG — the
    // same distribution as the Go crypto/rand big.Int path.
    loop {
        let b = rand_bytes(4);
        let v = u32::from_be_bytes([b[0], b[1], b[2], b[3]]) as u64;
        let limit = 4_294_967_296u64 - (4_294_967_296u64 % 1_000_000);
        if v < limit {
            let code = format!("{:06}", v % 1_000_000);
            let salt = hex(&rand_bytes(8));
            let mut h = Sha256::new();
            h.update(salt.as_bytes());
            h.update(b":");
            h.update(code.as_bytes());
            return (code, salt, hex(&h.finalize()));
        }
    }
}

fn otp_hash(salt: &str, code: &str) -> String {
    let mut h = Sha256::new();
    h.update(salt.as_bytes());
    h.update(b":");
    h.update(code.as_bytes());
    hex(&h.finalize())
}

// ---- HTTP plumbing (std-only, same style as services/security) ----

fn respond(stream: &mut TcpStream, status: &str, body: &str) {
    let resp = format!(
        "HTTP/1.1 {}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nCache-Control: no-store\r\nConnection: close\r\n\r\n{}",
        status,
        body.len(),
        body
    );
    let _ = stream.write_all(resp.as_bytes());
}

fn handle(mut stream: TcpStream, secret: &str) {
    let mut raw = Vec::new();
    let mut buf = [0u8; 8192];
    loop {
        let n = match stream.read(&mut buf) {
            Ok(0) => return,
            Ok(n) => n,
            Err(_) => return,
        };
        raw.extend_from_slice(&buf[..n]);
        if raw.len() > 8192 * 4 {
            respond(&mut stream, "400 Bad Request", "{\"error\":\"request too large\"}");
            return;
        }
        if let Some(end) = find_headers_end(&raw) {
            let head = String::from_utf8_lossy(&raw[..end]);
            let mut cl = 0usize;
            for line in head.split("\r\n") {
                if let Some(v) = line.to_ascii_lowercase().strip_prefix("content-length:") {
                    cl = v.trim().parse().unwrap_or(0);
                }
            }
            if raw.len() >= end + 4 + cl {
                break;
            }
        }
    }
    let text = String::from_utf8_lossy(&raw).to_string();
    let first = text.split("\r\n").next().unwrap_or("");
    let mut fit = first.split(' ');
    let method = fit.next().unwrap_or("").to_string();
    let path = fit.next().unwrap_or("").to_string();
    let hdr_end = find_headers_end(&raw).unwrap_or(0);
    let body = &text[(hdr_end + 4).min(text.len())..];

    if path == "/health" {
        respond(&mut stream, "200 OK", "{\"status\":\"ok\"}");
        return;
    }

    let mut authorized = false;
    for line in text[..hdr_end].split("\r\n") {
        if let Some(rest) = line.strip_prefix("Authorization: Bearer ") {
            authorized = ct_eq(rest.as_bytes(), secret.as_bytes());
        }
    }
    if !authorized {
        respond(&mut stream, "401 Unauthorized", "{\"error\":\"unauthorized\"}");
        return;
    }

    match (method.as_str(), path.as_str()) {
        ("POST", "/password/hash") => {
            let pw = json_get(body, "password").unwrap_or_default();
            if pw.is_empty() {
                respond(&mut stream, "400 Bad Request", "{\"error\":\"password required\"}");
                return;
            }
            let h = password_hash(&pw);
            respond(&mut stream, "200 OK", &format!("{{\"hash\":\"{}\"}}", json_escape(&h)));
        }
        ("POST", "/password/verify") => {
            let pw = json_get(body, "password").unwrap_or_default();
            let hash = json_get(body, "hash").unwrap_or_default();
            let ok = password_verify(&pw, &hash);
            respond(&mut stream, "200 OK", &format!("{{\"ok\":{}}}", ok));
        }
        ("POST", "/jwt/mint") => {
            let sub = json_get(body, "sub").unwrap_or_default();
            let typ = json_get(body, "typ").unwrap_or_default();
            let scope = json_get(body, "scope").unwrap_or_default();
            let exp = json_get_num(body, "exp").unwrap_or(0);
            let iat = json_get_num(body, "iat").unwrap_or_else(now_unix_at_call);
            if sub.is_empty() || typ.is_empty() || exp <= 0 {
                respond(&mut stream, "400 Bad Request", "{\"error\":\"sub, typ and positive exp required\"}");
                return;
            }
            let t = jwt_mint(&sub, &typ, &scope, exp, iat);
            respond(&mut stream, "200 OK", &format!("{{\"token\":\"{}\"}}", json_escape(&t)));
        }
        ("POST", "/jwt/verify") => {
            let token = json_get(body, "token").unwrap_or_default();
            match jwt_verify(&token) {
                Some(payload) => respond(&mut stream, "200 OK", &format!("{{\"claims\":{}}}", payload)),
                None => respond(&mut stream, "200 OK", "{\"claims\":null}"),
            }
        }
        ("POST", "/totp/generate") => {
            respond(&mut stream, "200 OK", &format!("{{\"secret\":\"{}\"}}", totp_generate()));
        }
        ("POST", "/totp/verify") => {
            let sec = json_get(body, "secret").unwrap_or_default();
            let code = json_get(body, "code").unwrap_or_default();
            let ok = totp_verify(&sec, &code);
            respond(&mut stream, "200 OK", &format!("{{\"ok\":{}}}", ok));
        }
        ("POST", "/otp/generate") => {
            let (code, salt, hash) = otp_generate();
            respond(
                &mut stream,
                "200 OK",
                &format!("{{\"code\":\"{}\",\"salt\":\"{}\",\"hash\":\"{}\"}}", code, salt, hash),
            );
        }
        ("POST", "/otp/hash") => {
            let salt = json_get(body, "salt").unwrap_or_default();
            let code = json_get(body, "code").unwrap_or_default();
            let h = otp_hash(&salt, &code);
            respond(&mut stream, "200 OK", &format!("{{\"hash\":\"{}\"}}", h));
        }
        ("POST", "/random") => {
            let n = json_get_num(body, "bytes").unwrap_or(32);
            if n <= 0 || n > 4096 {
                respond(&mut stream, "400 Bad Request", "{\"error\":\"bytes must be 1..4096\"}");
                return;
            }
            respond(&mut stream, "200 OK", &format!("{{\"token\":\"{}\"}}", hex(&rand_bytes(n as usize))));
        }
        ("POST", "/hmac") => {
            let key = json_get(body, "key").unwrap_or_default();
            let msg = json_get(body, "message").unwrap_or_default();
            let sig = hex(&hmac_sha256(key.as_bytes(), msg.as_bytes()));
            respond(&mut stream, "200 OK", &format!("{{\"signature\":\"{}\"}}", sig));
        }
        _ => respond(&mut stream, "404 Not Found", "{\"error\":\"not found\"}"),
    }
}

fn now_unix_at_call() -> i64 {
    now_unix()
}

fn find_headers_end(b: &[u8]) -> Option<usize> {
    b.windows(4).position(|w| w == b"\r\n\r\n")
}

fn main() {
    let port: u16 = getenv("AUTHN_PORT", "8400").parse().unwrap_or(8400);
    let secret = getenv("AUTHN_SECRET", "");
    let app_env = getenv("APP_ENV", "development");
    if secret.is_empty() {
        eprintln!("FATAL: AUTHN_SECRET is required (fail-closed)");
        std::process::exit(1);
    }
    if app_env == "production" && secret.len() < 32 {
        eprintln!("FATAL: AUTHN_SECRET must be >= 32 chars in production");
        std::process::exit(1);
    }
    let jwt = std::env::var("JWT_SECRET").unwrap_or_default();
    if app_env == "production" && jwt.len() < 32 {
        eprintln!("FATAL: JWT_SECRET must be >= 32 bytes in production");
        std::process::exit(1);
    }
    JWT_SECRET
        .set(if jwt.is_empty() {
            eprintln!("WARNING: JWT_SECRET unset; development default");
            b"dev-only-insecure-secret".to_vec()
        } else {
            jwt.into_bytes()
        })
        .expect("jwt secret once");

    let listener = TcpListener::bind(("0.0.0.0", port)).expect("bind authn port");
    println!("authn on :{}", port);
    for stream in listener.incoming() {
        match stream {
            Ok(s) => {
                let sec = secret.clone();
                std::thread::spawn(move || handle(s, &sec));
            }
            Err(_) => continue,
        }
    }
}

// ---- tests: these vectors define the wire contract with the Go side ----
#[cfg(test)]
mod tests {
    use super::*;

    fn setup() {
        let _ = JWT_SECRET.set(b"dev-only-insecure-secret".to_vec());
    }

    #[test]
    fn argon2_roundtrip() {
        let h = password_hash("Secret123!");
        assert!(h.starts_with("$argon2id$v=19$m=65536,t=3,p=2$"));
        assert!(password_verify("Secret123!", &h));
        assert!(!password_verify("wrong", &h));
        assert!(!password_verify("Secret123!", "$argon2i$v=19$m=8,t=1,p=1$aa$bb"));
    }

    #[test]
    fn go_cross_vector() {
        // Hash produced by services/api's Go implementation (x/crypto/argon2,
        // same parameters, fixed salt "0123456789abcdef"): secrets must be
        // interchangeable across the two implementations.
        let go_hash = "$argon2id$v=19$m=65536,t=3,p=2$MDEyMzQ1Njc4OWFiY2RlZg$hgG0Gl6IspQbr/FDRooOxkKfabfpfdg+IgCvs6jYLqQ";
        assert!(password_verify("Secret123!", go_hash));
        assert!(!password_verify("nope", go_hash));
    }

    #[test]
    fn base32_cycle() {
        let enc = b32_encode(b"hello");
        assert_eq!(enc, "NBSWY3DP");
        let dec = b32_decode(&enc).unwrap();
        assert_eq!(dec, b"hello".to_vec());
    }

    #[test]
    fn totp_code_shape_and_drift() {
        let key = b32_encode(b"12345678901234567890");
        let secs = b32_decode(&key).unwrap();
        assert_eq!(secs, b"12345678901234567890".to_vec());
        // RFC 6238 SHA-1 appendix: T = 59s -> counter 1 -> 94287082 in the
        // 8-digit reference; both implementations use 6 digits -> 287082.
        let c = totp_code(&secs, 1);
        assert_eq!(c.len(), 6);
        assert_eq!(c, "287082");
    }

    #[test]
    fn jwt_cycle_and_forgery_rejection() {
        setup();
        let t = jwt_mint("u1", "access", "", now_unix() + 60, now_unix());
        let claims = jwt_verify(&t).expect("self-minted token must verify");
        assert!(claims.contains("\"sub\":\"u1\""));
        // Forged signature must fail.
        let mut forged = t.clone();
        let last = forged.pop().unwrap();
        forged.push(if last == 'A' { 'B' } else { 'A' });
        assert!(jwt_verify(&forged).is_none());
        // Expired tokens must fail.
        let old = {
            let now = now_unix();
            jwt_mint("u1", "access", "", now - 10, now - 120)
        };
        assert!(jwt_verify(&old).is_none());
    }

    #[test]
    fn otp_shape_and_hash() {
        let (code, salt, hash) = otp_generate();
        assert_eq!(code.len(), 6);
        assert_eq!(salt.len(), 16);
        assert_eq!(hash.len(), 64);
        assert_eq!(otp_hash(&salt, &code), hash);
        assert!(code.bytes().all(|b| b.is_ascii_digit()));
    }

    #[test]
    fn hmac_known_vector() {
        // RFC 2202 test case 1: key = 0x0b * 20, data = "Hi There"
        // -> b0344c61d8db38535ca8afceaf0bf12b881dc200c9833da726e9376c2e32cff7
        let sig = hex(&hmac_sha256(&[0x0bu8; 20], b"Hi There"));
        assert_eq!(sig, "b0344c61d8db38535ca8afceaf0bf12b881dc200c9833da726e9376c2e32cff7");
    }

    #[test]
    fn json_number_spacing_tolerance() {
        // Go (encoding/json) and Python (json.dumps) emit spaced JSON; the
        // numeric extractor must skip whitespace between ':' and the value.
        assert_eq!(json_get_num("{\"bytes\": 16}", "bytes"), Some(16));
        assert_eq!(json_get_num("{\"bytes\":16}", "bytes"), Some(16));
        assert_eq!(json_get_num("{\"x\": -42}", "x"), Some(-42));
        assert_eq!(json_get_num("{\"x\": \"var\"}", "x"), None);
    }
}
