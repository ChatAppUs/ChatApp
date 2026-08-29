//! ChatApp security service: signed media URLs and inter-service request
//! signing. Stateless, std-only, ultra-low-latency. The signing secret is
//! supplied via SIGNING_SECRET and never leaves this process.

mod sha256;

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

fn handle(mut stream: TcpStream, secret: Arc<Vec<u8>>) {
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

        _ => respond(&mut stream, "404 Not Found", "{\"error\":\"not found\"}"),
    }
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
    let port = std::env::var("SECURITY_PORT").unwrap_or_else(|_| "8090".to_string());
    let listener = TcpListener::bind(format!("0.0.0.0:{}", port)).expect("bind failed");
    println!("chatapp-security listening on :{}", port);
    let secret = Arc::new(secret.into_bytes());
    for stream in listener.incoming() {
        match stream {
            Ok(s) => {
                let secret = Arc::clone(&secret);
                thread::spawn(move || handle(s, secret));
            }
            Err(e) => eprintln!("accept error: {}", e),
        }
    }
}
