//! ChatApp security service: signed media URLs and inter-service request
//! signing. Stateless, std-only, ultra-low-latency. The signing secret is
//! supplied via SIGNING_SECRET and never leaves this process.

mod custody;
mod jwt;
mod sha1;
mod sha256;
#[cfg(test)]
mod tests;

use sha256::{ct_eq, hmac_sha256, sha256, to_hex};
use std::io::{Read, Write};
use std::net::{TcpListener, TcpStream};
use std::sync::Arc;
use std::thread;
use std::time::{SystemTime, UNIX_EPOCH};

fn json_string_field(body: &str, key: &str) -> Option<String> {
    let needle = format!("\"{}\":\"", key);
    let start = body.find(&needle)? + needle.len();
    let rest = &body[start..];
    let mut out = String::new();
    let mut chars = rest.chars();
    while let Some(c) = chars.next() {
        match c {
            '\\' => {
                if let Some(esc) = chars.next() {
                    out.push(match esc {
                        'n' => '\n',
                        't' => '\t',
                        'r' => '\r',
                        other => other,
                    });
                }
            }
            '"' => return Some(out),
            other => out.push(other),
        }
    }
    None
}

fn json_escape(s: &str) -> String {
    s.replace('\\', "\\\\").replace('"', "\\\"")
}

fn now_secs() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0)
}

fn respond(stream: &mut TcpStream, status: &str, body: &str) {
    let resp = format!(
        "HTTP/1.1 {}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
        status,
        body.len(),
        body
    );
    let _ = stream.write_all(resp.as_bytes());
}

fn handle(mut stream: TcpStream, secret: Arc<Vec<u8>>, custody_seed: Arc<Option<Vec<u8>>>) {
    let mut buf = vec![0u8; 64 * 1024];
    let n = match stream.read(&mut buf) {
        Ok(n) if n > 0 => n,
        _ => return,
    };
    let req = String::from_utf8_lossy(&buf[..n]).to_string();
    let mut lines = req.split("\r\n");
    let request_line = lines.next().unwrap_or("");
    let mut parts = request_line.split_whitespace();
    let method = parts.next().unwrap_or("");
    let path = parts.next().unwrap_or("");

    let body_start = req.find("\r\n\r\n").map(|i| i + 4).unwrap_or(req.len());
    let body = &req[body_start..];

    match (method, path) {
        ("GET", "/health") => respond(&mut stream, "200 OK", "{\"status\":\"ok\"}"),

        // Sign a media path or inter-service payload.
        // {"payload":"/media/abc.mp4","expires_in":3600}
        ("POST", "/sign") => {
            let payload = match json_string_field(body, "payload") {
                Some(p) => p,
                None => return respond(&mut stream, "400 Bad Request", "{\"error\":\"payload required\"}"),
            };
            let expires_in: u64 = json_string_field(body, "expires_in")
                .and_then(|v| v.parse().ok())
                .unwrap_or(3600);
            let exp = now_secs() + expires_in.min(86_400);
            let msg = format!("{}:{}", payload, exp);
            let sig = to_hex(&hmac_sha256(&secret, msg.as_bytes()));
            respond(
                &mut stream,
                "200 OK",
                &format!(
                    "{{\"payload\":\"{}\",\"expires\":{},\"signature\":\"{}\"}}",
                    json_escape(&payload),
                    exp,
                    sig
                ),
            );
        }

        // Verify a signature produced by /sign.
        // {"payload":"/media/abc.mp4","expires":1719999999,"signature":"..."}
        ("POST", "/verify") => {
            let (Some(payload), Some(expires), Some(sig)) = (
                json_string_field(body, "payload"),
                json_string_field(body, "expires"),
                json_string_field(body, "signature"),
            ) else {
                return respond(&mut stream, "400 Bad Request", "{\"error\":\"payload, expires and signature required\"}");
            };
            let exp: u64 = match expires.parse() {
                Ok(v) => v,
                Err(_) => return respond(&mut stream, "400 Bad Request", "{\"error\":\"invalid expires\"}"),
            };
            if now_secs() > exp {
                return respond(&mut stream, "200 OK", "{\"valid\":false,\"reason\":\"expired\"}");
            }
            let msg = format!("{}:{}", payload, exp);
            let expected = to_hex(&hmac_sha256(&secret, msg.as_bytes()));
            let valid = ct_eq(expected.as_bytes(), sig.as_bytes());
            respond(
                &mut stream,
                "200 OK",
                &format!("{{\"valid\":{}}}", valid),
            );
        }

        // {"data":"..."} -> sha256 hex
        ("POST", "/hash") => {
            let data = json_string_field(body, "data").unwrap_or_default();
            respond(
                &mut stream,
                "200 OK",
                &format!("{{\"sha256\":\"{}\"}}", to_hex(&sha256(data.as_bytes()))),
            );
        }

        // RFC 6238 TOTP verification (30s steps, ±1 step drift tolerance).
        // {"secret":"BASE32...","code":"123456"}
        ("POST", "/totp/verify") => {
            let (Some(tsecret), Some(code)) = (
                json_string_field(body, "secret"),
                json_string_field(body, "code"),
            ) else {
                return respond(&mut stream, "400 Bad Request", "{\"error\":\"secret and code required\"}");
            };
            let valid = sha1::totp_verify(&tsecret, &code, now_secs(), 30);
            respond(&mut stream, "200 OK", &format!("{{\"valid\":{}}}", valid));
        }

        // Generate a 160-bit base32 TOTP secret (randomness from /dev/urandom).
        ("POST", "/totp/generate") => {
            match random_base32(20) {
                Some(s) => respond(&mut stream, "200 OK", &format!("{{\"secret\":\"{}\"}}", s)),
                None => respond(&mut stream, "500 Internal Server Error", "{\"error\":\"entropy unavailable\"}"),
            }
        }

        // HS256 user-token verification for edge services that do not share
        // JWT_SECRET themselves. {"token":"...","secret":"<JWT_SECRET>"}
        // The caller supplies the secret so this service never holds it at
        // rest; SIGNING_SECRET-only deployments stay unaffected.
        ("POST", "/jwt/verify") => {
            let (Some(token), Some(jwt_secret)) = (
                json_string_field(body, "token"),
                json_string_field(body, "secret"),
            ) else {
                return respond(&mut stream, "400 Bad Request", "{\"error\":\"token and secret required\"}");
            };
            match jwt::verify_hs256(&token, jwt_secret.as_bytes(), now_secs()) {
                Some(claims) => respond(
                    &mut stream,
                    "200 OK",
                    &format!(
                        "{{\"valid\":true,\"sub\":\"{}\",\"exp\":{}}}",
                        json_escape(&claims.sub),
                        claims.exp
                    ),
                ),
                None => respond(&mut stream, "200 OK", "{\"valid\":false}"),
            }
        }


        // Custody: per-user key fingerprint (never key material).
        // {"uid":"...","purpose":"withdraw"}
        ("POST", "/custody/derive") => {
            let seed = match custody_seed.as_ref() {
                Some(s) => s,
                None => return respond(&mut stream, "503 Service Unavailable", "{\"error\":\"custody not configured\"}"),
            };
            let (Some(uid), Some(purpose)) = (
                json_string_field(body, "uid"),
                json_string_field(body, "purpose"),
            ) else {
                return respond(&mut stream, "400 Bad Request", "{\"error\":\"uid and purpose required\"}");
            };
            if purpose.len() > 32 || !purpose.chars().all(|c| c.is_ascii_alphanumeric() || c == '-' || c == '_') {
                return respond(&mut stream, "400 Bad Request", "{\"error\":\"invalid purpose\"}");
            }
            let key = custody::custody_key(seed, &uid, &purpose);
            respond(
                &mut stream,
                "200 OK",
                &format!("{{\"key_id\":\"{}\"}}", custody::key_fingerprint(&key)),
            );
        }

        // Custody co-signature over a canonical message. Keys never leave
        // this process; the API must obtain this co-signature before
        // broadcasting a withdrawal through our own nodes.
        // {"uid":"...","purpose":"withdraw","message":"withdraw|id|uid|..."}
        ("POST", "/custody/sign") => {
            let seed = match custody_seed.as_ref() {
                Some(s) => s,
                None => return respond(&mut stream, "503 Service Unavailable", "{\"error\":\"custody not configured\"}"),
            };
            let (Some(uid), Some(purpose), Some(message)) = (
                json_string_field(body, "uid"),
                json_string_field(body, "purpose"),
                json_string_field(body, "message"),
            ) else {
                return respond(&mut stream, "400 Bad Request", "{\"error\":\"uid, purpose and message required\"}");
            };
            if message.len() > 4096 {
                return respond(&mut stream, "400 Bad Request", "{\"error\":\"message too large\"}");
            }
            let key = custody::custody_key(seed, &uid, &purpose);
            respond(
                &mut stream,
                "200 OK",
                &format!(
                    "{{\"key_id\":\"{}\",\"signature\":\"{}\"}}",
                    custody::key_fingerprint(&key),
                    custody::custody_sign(&key, &message)
                ),
            );
        }

        _ => respond(&mut stream, "404 Not Found", "{\"error\":\"not found\"}"),
    }
}

fn random_base32(n: usize) -> Option<String> {
    let mut buf = vec![0u8; n];
    std::fs::File::open("/dev/urandom")
        .ok()?
        .read_exact(&mut buf)
        .ok()?;
    const ALPHABET: &[u8] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
    let mut out = String::with_capacity(n * 8 / 5 + 1);
    let mut acc: u32 = 0;
    let mut bits = 0u32;
    for b in buf {
        acc = (acc << 8) | b as u32;
        bits += 8;
        while bits >= 5 {
            bits -= 5;
            out.push(ALPHABET[((acc >> bits) & 31) as usize] as char);
        }
    }
    if bits > 0 {
        out.push(ALPHABET[((acc << (5 - bits)) & 31) as usize] as char);
    }
    Some(out)
}

fn main() {
    let app_env = std::env::var("APP_ENV").unwrap_or_else(|_| "development".to_string());
    let secret = match std::env::var("SIGNING_SECRET") {
        Ok(s) if s.len() >= 32 => s,
        Ok(_) => {
            eprintln!("FATAL: SIGNING_SECRET must be at least 32 random bytes");
            std::process::exit(1);
        }
        Err(_) => {
            if app_env == "production" {
                eprintln!("FATAL: SIGNING_SECRET must be set in production");
                std::process::exit(1);
            }
            eprintln!("WARNING: SIGNING_SECRET not set; using development default");
            "dev-signing-secret".to_string()
        }
    };
    let custody_seed: Option<Vec<u8>> = match std::env::var("CUSTODY_MASTER_SEED") {
        Ok(v) if v.len() >= 32 => Some(v.into_bytes()),
        Ok(_) => {
            eprintln!("FATAL: CUSTODY_MASTER_SEED must be at least 32 bytes when set");
            std::process::exit(1);
        }
        Err(_) => {
            if app_env == "production" {
                eprintln!("FATAL: CUSTODY_MASTER_SEED must be set in production");
                std::process::exit(1);
            }
            None // custody endpoints return 503 until configured
        }
    };
    let custody_seed = Arc::new(custody_seed);
    let port = std::env::var("SECURITY_PORT").unwrap_or_else(|_| "8090".to_string());
    let listener = TcpListener::bind(format!("0.0.0.0:{}", port)).expect("bind failed");
    println!("chatapp-security listening on :{}", port);
    let secret = Arc::new(secret.into_bytes());
    for stream in listener.incoming() {
        match stream {
            Ok(s) => {
                let secret = Arc::clone(&secret);
                let custody_seed = Arc::clone(&custody_seed);
                thread::spawn(move || handle(s, secret, custody_seed));
            }
            Err(e) => eprintln!("accept error: {}", e),
        }
    }
}
