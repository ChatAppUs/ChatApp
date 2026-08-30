//! SHA-1 + HMAC-SHA1 — required only for RFC 6238 TOTP, which mandates
//! SHA-1. All other crypto in this service is SHA-256/HMAC-SHA256.

pub struct Sha1 {
    state: [u32; 5],
    buf: [u8; 64],
    buflen: usize,
    total: u64,
}

impl Sha1 {
    pub fn new() -> Self {
        Self {
            state: [0x67452301, 0xEFCDAB89, 0x98BADCFE, 0x10325476, 0xC3D2E1F0],
            buf: [0u8; 64],
            buflen: 0,
            total: 0,
        }
    }

    fn process_block(&mut self, block: &[u8]) {
        let mut w = [0u32; 80];
        for i in 0..16 {
            w[i] = u32::from_be_bytes([block[i * 4], block[i * 4 + 1], block[i * 4 + 2], block[i * 4 + 3]]);
        }
        for i in 16..80 {
            w[i] = (w[i - 3] ^ w[i - 8] ^ w[i - 14] ^ w[i - 16]).rotate_left(1);
        }
        let (mut a, mut b, mut c, mut d, mut e) =
            (self.state[0], self.state[1], self.state[2], self.state[3], self.state[4]);
        for i in 0..80 {
            let (f, k) = match i {
                0..=19 => ((b & c) | (!b & d), 0x5A827999u32),
                20..=39 => (b ^ c ^ d, 0x6ED9EBA1u32),
                40..=59 => ((b & c) | (b & d) | (c & d), 0x8F1BBCDCu32),
                _ => (b ^ c ^ d, 0xCA62C1D6u32),
            };
            let tmp = a.rotate_left(5).wrapping_add(f).wrapping_add(e).wrapping_add(k).wrapping_add(w[i]);
            e = d;
            d = c;
            c = b.rotate_left(30);
            b = a;
            a = tmp;
        }
        self.state[0] = self.state[0].wrapping_add(a);
        self.state[1] = self.state[1].wrapping_add(b);
        self.state[2] = self.state[2].wrapping_add(c);
        self.state[3] = self.state[3].wrapping_add(d);
        self.state[4] = self.state[4].wrapping_add(e);
    }

    pub fn update(&mut self, mut data: &[u8]) {
        self.total += data.len() as u64;
        while !data.is_empty() {
            let take = (64 - self.buflen).min(data.len());
            self.buf[self.buflen..self.buflen + take].copy_from_slice(&data[..take]);
            self.buflen += take;
            data = &data[take..];
            if self.buflen == 64 {
                let block = self.buf;
                self.process_block(&block);
                self.buflen = 0;
            }
        }
    }

    pub fn finalize(mut self) -> [u8; 20] {
        let bits = self.total * 8;
        self.update(&[0x80]);
        while self.buflen != 56 {
            self.update(&[0u8]);
        }
        self.update(&bits.to_be_bytes());
        let mut out = [0u8; 20];
        for (i, word) in self.state.iter().enumerate() {
            out[i * 4..i * 4 + 4].copy_from_slice(&word.to_be_bytes());
        }
        out
    }
}

pub fn sha1(data: &[u8]) -> [u8; 20] {
    let mut h = Sha1::new();
    h.update(data);
    h.finalize()
}

pub fn hmac_sha1(key: &[u8], msg: &[u8]) -> [u8; 20] {
    let mut k = [0u8; 64];
    if key.len() > 64 {
        let d = sha1(key);
        k[..20].copy_from_slice(&d);
    } else {
        k[..key.len()].copy_from_slice(key);
    }
    let mut ipad = [0x36u8; 64];
    let mut opad = [0x5cu8; 64];
    for i in 0..64 {
        ipad[i] ^= k[i];
        opad[i] ^= k[i];
    }
    let mut inner = Sha1::new();
    inner.update(&ipad);
    inner.update(msg);
    let ihash = inner.finalize();
    let mut outer = Sha1::new();
    outer.update(&opad);
    outer.update(&ihash);
    outer.finalize()
}

/// RFC 6238 TOTP code for a base32-encoded secret and a time counter.
pub fn totp_code(secret_b32: &str, counter: u64) -> Option<String> {
    let key = base32_decode(secret_b32)?;
    let mac = hmac_sha1(&key, &counter.to_be_bytes());
    let offset = (mac[19] & 0x0f) as usize;
    let code = ((mac[offset] & 0x7f) as u32) << 24
        | (mac[offset + 1] as u32) << 16
        | (mac[offset + 2] as u32) << 8
        | (mac[offset + 3] as u32);
    Some(format!("{:06}", code % 1_000_000))
}

fn base32_decode(s: &str) -> Option<Vec<u8>> {
    fn val(c: u8) -> Option<u8> {
        match c {
            b'A'..=b'Z' => Some(c - b'A'),
            b'2'..=b'7' => Some(c - b'2' + 26),
            _ => None,
        }
    }
    let mut out = Vec::new();
    let mut acc: u32 = 0;
    let mut bits = 0u32;
    for c in s.bytes() {
        let v = val(c.to_ascii_uppercase())?;
        acc = (acc << 5) | v as u32;
        bits += 5;
        if bits >= 8 {
            bits -= 8;
            out.push((acc >> bits) as u8);
        }
    }
    Some(out)
}

/// Verify a TOTP code tolerating one 30s step of clock drift either way.
pub fn totp_verify(secret_b32: &str, code: &str, at_secs: u64, steps: u64) -> bool {
    if code.len() != 6 {
        return false;
    }
    let counter = at_secs / steps;
    for drift in [-1i64, 0, 1] {
        let c = counter as i64 + drift;
        if c < 0 {
            continue;
        }
        if let Some(expected) = totp_code(secret_b32, c as u64) {
            if expected.as_bytes() == code.as_bytes() {
                return true;
            }
        }
    }
    false
}
