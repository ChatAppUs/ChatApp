// ChatApp SFU forwarder — C++17 TURN relay data plane.
//
// Packet-forwarding hot loop split out of the Go/Pion SFU (services/sfu),
// completing the CPP_CONVERSION_PLAN P1 item: Go keeps signaling and its
// embedded STUN/TURN as fallback; this engine owns relayed media — the
// same control/data split as the realtime relay and the media edge.
// Relay sockets + epoll, zero allocations on the per-packet relay path.
//
// Protocol surface (RFC 5389 STUN / RFC 5766 TURN subset, IPv4 UDP):
//   Binding, Allocate, Refresh, CreatePermission, ChannelBind,
//   Send/Data indications, ChannelData fast-path relay.
// Long-term credentials identical to the API (handlers_calls.go):
//   username "expiryUnix:uid", password = base64(HMAC-SHA1(TURN_SECRET, username)),
//   key = MD5(username:realm:password), MESSAGE-INTEGRITY = HMAC-SHA1(key).
// Control surface (TCP, bearer SFU_SECRET): GET /health, GET /stats.
//
// Env: TURN_SECRET (required), PUBLIC_IP (default 127.0.0.1),
// SFU_SECRET (control bearer), TURN_LISTEN (default 3479),
// CONTROL_PORT (default 8099), MAX_ALLOCATIONS (default 4096),
// REALM (default "chatapp").

#include <arpa/inet.h>
#include <fcntl.h>
#include <netdb.h>
#include <netinet/in.h>
#include <sys/epoll.h>
#include <sys/socket.h>
#include <unistd.h>

#include <cerrno>
#include <chrono>
#include <cmath>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <map>
#include <random>
#include <string>
#include <unordered_map>
#include <unordered_set>
#include <vector>

// ===================== crypto (dependency-free, from spec) =================

namespace crypto {

struct Sha1 {
    uint32_t h[5] = {0x67452301u, 0xEFCDAB89u, 0x98BADCFEu, 0x10325476u, 0xC3D2E1F0u};
    uint64_t total = 0;
    uint8_t buf[64];
    size_t buflen = 0;
    static uint32_t rol(uint32_t v, int n) { return (v << n) | (v >> (32 - n)); }
    void block(const uint8_t* p) {
        uint32_t w[80];
        for (int i = 0; i < 16; i++)
            w[i] = (uint32_t)p[i*4] << 24 | (uint32_t)p[i*4+1] << 16 |
                   (uint32_t)p[i*4+2] << 8 | (uint32_t)p[i*4+3];
        for (int i = 16; i < 80; i++) w[i] = rol(w[i-3] ^ w[i-8] ^ w[i-14] ^ w[i-16], 1);
        uint32_t a = h[0], b = h[1], c = h[2], d = h[3], e = h[4];
        for (int i = 0; i < 80; i++) {
            uint32_t f, k;
            if (i < 20)      { f = (b & c) | (~b & d);   k = 0x5A827999u; }
            else if (i < 40) { f = b ^ c ^ d;            k = 0x6ED9EBA1u; }
            else if (i < 60) { f = (b & c) | (b & d) | (c & d); k = 0x8F1BBCDCu; }
            else             { f = b ^ c ^ d;            k = 0xCA62C1D6u; }
            uint32_t t = rol(a, 5) + f + e + k + w[i];
            e = d; d = c; c = rol(b, 30); b = a; a = t;
        }
        h[0] += a; h[1] += b; h[2] += c; h[3] += d; h[4] += e;
    }
    void update(const uint8_t* p, size_t n) {
        total += n;
        while (n) {
            size_t take = (64 - buflen < n) ? 64 - buflen : n;
            memcpy(buf + buflen, p, take);
            buflen += take; p += take; n -= take;
            if (buflen == 64) { block(buf); buflen = 0; }
        }
    }
    void final(uint8_t out[20]) {
        uint64_t bits = total * 8;
        update((const uint8_t*)"\x80", 1);
        while (buflen != 56) update((const uint8_t*)"\x00", 1);
        uint8_t lb[8];
        for (int i = 0; i < 8; i++) lb[i] = (uint8_t)(bits >> ((7 - i) * 8));
        update(lb, 8);
        for (int i = 0; i < 5; i++) {
            out[i*4] = (uint8_t)(h[i] >> 24); out[i*4+1] = (uint8_t)(h[i] >> 16);
            out[i*4+2] = (uint8_t)(h[i] >> 8); out[i*4+3] = (uint8_t)h[i];
        }
    }
};

inline void sha1(const uint8_t* p, size_t n, uint8_t out[20]) {
    Sha1 s; s.update(p, n); s.final(out);
}

inline void hmacSha1(const uint8_t* key, size_t klen, const uint8_t* msg, size_t mlen, uint8_t out[20]) {
    uint8_t k0[64] = {};
    if (klen > 64) { uint8_t d[20]; sha1(key, klen, d); memcpy(k0, d, 20); }
    else memcpy(k0, key, klen);
    uint8_t ipad[64], opad[64];
    for (int i = 0; i < 64; i++) { ipad[i] = k0[i] ^ 0x36; opad[i] = k0[i] ^ 0x5C; }
    Sha1 s;
    s.update(ipad, 64); s.update(msg, mlen);
    uint8_t inner[20]; s.final(inner);
    Sha1 s2;
    s2.update(opad, 64); s2.update(inner, 20); s2.final(out);
}

struct Md5 {
    uint32_t a = 0x67452301u, b = 0xEFCDAB89u, c = 0x98BADCFEu, d = 0x10325476u;
    uint64_t total = 0;
    uint8_t buf[64];
    size_t buflen = 0;
    static uint32_t rol(uint32_t v, int n) { return (v << n) | (v >> (32 - n)); }
    static const int S[64];
    static uint32_t K[64];
    static bool init() {
        for (int i = 0; i < 64; i++) {
            double v = std::fabs(std::sin((double)i + 1.0)) * 4294967296.0;
            K[i] = (uint32_t)v;
        }
        return true;
    }
    void block(const uint8_t* p) {
        uint32_t m[16];
        for (int i = 0; i < 16; i++)
            m[i] = (uint32_t)p[i*4] | (uint32_t)p[i*4+1] << 8 |
                   (uint32_t)p[i*4+2] << 16 | (uint32_t)p[i*4+3] << 24;
        uint32_t A = a, B = b, C = c, D = d;
        for (int i = 0; i < 64; i++) {
            uint32_t f; int g;
            if (i < 16)      { f = (B & C) | (~B & D);   g = i; }
            else if (i < 32) { f = (D & B) | (~D & C);   g = (5*i + 1) % 16; }
            else if (i < 48) { f = B ^ C ^ D;            g = (3*i + 5) % 16; }
            else             { f = C ^ (B | ~D);         g = (7*i) % 16; }
            uint32_t t = D; D = C; C = B;
            B = B + rol(A + f + K[i] + m[g], S[i]);
            A = t;
        }
        a += A; b += B; c += C; d += D;
    }
    void update(const uint8_t* p, size_t n) {
        total += n;
        while (n) {
            size_t take = (64 - buflen < n) ? 64 - buflen : n;
            memcpy(buf + buflen, p, take);
            buflen += take; p += take; n -= take;
            if (buflen == 64) { block(buf); buflen = 0; }
        }
    }
    void final(uint8_t out[16]) {
        uint64_t bits = total * 8;
        update((const uint8_t*)"\x80", 1);
        while (buflen != 56) update((const uint8_t*)"\x00", 1);
        uint8_t lb[8];
        for (int i = 0; i < 8; i++) lb[i] = (uint8_t)(bits >> (i * 8));
        update(lb, 8);
        for (int i = 0; i < 4; i++) {
            out[i] = (uint8_t)(a >> (i*8));     out[i+4] = (uint8_t)(b >> (i*8));
            out[i+8] = (uint8_t)(c >> (i*8));   out[i+12] = (uint8_t)(d >> (i*8));
        }
    }
};
const int Md5::S[64] = {
    7,12,17,22, 7,12,17,22, 7,12,17,22, 7,12,17,22,
    5,9,14,20,  5,9,14,20,  5,9,14,20,  5,9,14,20,
    4,11,16,23, 4,11,16,23, 4,11,16,23, 4,11,16,23,
    6,10,15,21, 6,10,15,21, 6,10,15,21, 6,10,15,21,
};
uint32_t Md5::K[64] = {};
bool md5InitOk = Md5::init();

inline void md5(const uint8_t* p, size_t n, uint8_t out[16]) {
    Md5 m; m.update(p, n); m.final(out);
}

} // namespace crypto

// ===================== STUN/TURN =================

static const uint32_t MAGIC = 0x2112A442u;

namespace msg {
enum Method : uint16_t {
    BINDING = 0x001, ALLOCATE = 0x003, REFRESH = 0x004, SEND = 0x006,
    DATA = 0x007, CREATE_PERM = 0x008, CHANNEL_BIND = 0x009,
};
enum Cls : uint16_t { REQUEST = 0, INDICATION = 1, SUCCESS = 2, ERROR = 3 };
enum Attr : uint16_t {
    USERNAME = 0x0006, MI = 0x0008, ERROR_CODE = 0x0009, CHANNEL_NUMBER = 0x000C,
    LIFETIME = 0x000D, XOR_PEER = 0x0012, DATA_PAYLOAD = 0x0013, REALM = 0x0014,
    NONCE = 0x0015, XOR_RELAYED = 0x0016, REQUESTED_TRANSPORT = 0x0019,
    XOR_MAPPED = 0x0020, FINGERPRINT = 0x8028,
};
} // namespace msg

static uint16_t encodeType(uint16_t method, uint16_t cls) {
    uint16_t t = (uint16_t)(((method & 0x0F80) << 2) | ((method & 0x0070) << 1) | (method & 0x000F));
    if (cls == msg::INDICATION) t |= 0x0010;
    else if (cls == msg::SUCCESS) t |= 0x0100;
    else if (cls == msg::ERROR)   t |= 0x0110;
    return t;
}
static uint16_t decodeMethod(uint16_t t) {
    return (uint16_t)(((t & 0x3E00) >> 2) | ((t & 0x00E0) >> 1) | (t & 0x000F));
}

struct Parsed {
    uint16_t type = 0;
    uint8_t txid[12] = {};
    std::map<uint16_t, std::pair<const uint8_t*, uint16_t>> attrs; // first occurrence
    std::vector<std::pair<uint16_t, std::pair<const uint8_t*, uint16_t>>> allAttrs;
    size_t miOffset = 0; // absolute msg offset of MI attr (0 = none)
};

static bool parseMsg(const uint8_t* buf, size_t n, Parsed& out) {
    if (n < 20 || n - 20 > 0xFFFF) return false;
    out.type = (uint16_t)buf[0] << 8 | buf[1];
    uint16_t declLen = (uint16_t)buf[2] << 8 | buf[2+1];
    if (declLen != n - 20) return false;
    memcpy(out.txid, buf + 8, 12);
    size_t off = 20;
    while (off + 4 <= n) {
        uint16_t at = (uint16_t)buf[off] << 8 | buf[off+1];
        uint16_t al = (uint16_t)buf[off+2] << 8 | buf[off+3];
        if (off + 4 + al > n) return false;
        if (at == msg::MI && al == 20) out.miOffset = off;
        out.allAttrs.push_back({at, {buf + off + 4, al}});
        if (out.attrs.count(at) == 0) out.attrs[at] = {buf + off + 4, al};
        off += 4 + ((al + 3) & ~3u);
        if (at == msg::MI) break; // nothing after MI matters for auth
    }
    return true;
}

struct OutBuf {
    std::vector<uint8_t> b;
    void putType(uint16_t typeMethod, uint16_t cls) {
        uint16_t t = encodeType(typeMethod, cls);
        b.push_back((uint8_t)(t >> 8)); b.push_back((uint8_t)t);
        b.push_back(0); b.push_back(0); // length placeholder
        b.push_back((uint8_t)(MAGIC >> 24)); b.push_back((uint8_t)(MAGIC >> 16));
        b.push_back((uint8_t)(MAGIC >> 8));  b.push_back((uint8_t)MAGIC);
    }
    void putTxid(const uint8_t* txid) { for (int i = 0; i < 12; i++) b.push_back(txid[i]); }
    void padTo4() { while (b.size() % 4) b.push_back(0); }
    void attr(uint16_t at, const void* data, uint16_t len) {
        b.push_back((uint8_t)(at >> 8)); b.push_back((uint8_t)at);
        b.push_back((uint8_t)(len >> 8)); b.push_back((uint8_t)len);
        const uint8_t* p = (const uint8_t*)data;
        for (uint16_t i = 0; i < len; i++) b.push_back(p[i]);
        padTo4();
    }
    void attrStr(uint16_t at, const std::string& s) { attr(at, s.data(), (uint16_t)s.size()); }
    void attrU32(uint16_t at, uint32_t v) {
        uint8_t tmp[4] = {(uint8_t)(v >> 24), (uint8_t)(v >> 16), (uint8_t)(v >> 8), (uint8_t)v};
        attr(at, tmp, 4);
    }
    void errorCode(uint16_t code, const char* reason) {
        uint8_t tmp[4] = {0, 0, (uint8_t)((code / 100) & 0x7), (uint8_t)(code % 100)};
        std::vector<uint8_t> v(tmp, tmp + 4);
        const char* p = reason;
        while (*p) v.push_back((uint8_t)*p++);
        attr(msg::ERROR_CODE, v.data(), (uint16_t)v.size());
    }
    void xorAddr(uint16_t at, const sockaddr_in& a) {
        uint16_t port = ntohs(a.sin_port) ^ (uint16_t)(MAGIC >> 16);
        uint32_t ip = ntohl(a.sin_addr.s_addr) ^ MAGIC;
        uint8_t tmp[8] = {0, 0x01, (uint8_t)(port >> 8), (uint8_t)port,
                          (uint8_t)(ip >> 24), (uint8_t)(ip >> 16), (uint8_t)(ip >> 8), (uint8_t)ip};
        attr(at, tmp, 8);
    }
    void finish(uint8_t* out, size_t& len) {
        b[2] = (uint8_t)((b.size() - 20) >> 8); b[3] = (uint8_t)(b.size() - 20);
        memcpy(out, b.data(), b.size());
        len = b.size();
    }
    void finishMI(const uint8_t key[16]) {
        // compute HMAC over message with length adjusted to include a 24-byte MI attr
        size_t before = b.size();
        b[2] = (uint8_t)((before - 20 + 24) >> 8); b[3] = (uint8_t)(before - 20 + 24);
        uint8_t mac[20];
        crypto::hmacSha1(key, 16, b.data(), before, mac);
        b[2] = (uint8_t)((before - 20) >> 8); b[3] = (uint8_t)(before - 20);
        attr(msg::MI, mac, 20);
        b[2] = (uint8_t)((b.size() - 20) >> 8); b[3] = (uint8_t)(b.size() - 20);
    }
};

static std::string addrKey(const sockaddr_in& a) {
    char buf[64];
    snprintf(buf, sizeof(buf), "%u:%u", (unsigned)ntohl(a.sin_addr.s_addr), (unsigned)ntohs(a.sin_port));
    return std::string(buf);
}

// ===================== globals =================

static std::string turnSecret, sfuSecret, realm, publicIP;
static int turnListen, controlPort, maxAlloc;
static uint32_t defaultLifetime = 600;

struct Alloc {
    std::string user;
    int relayFd = -1;
    sockaddr_in relayAddr{};
    int64_t expiresAt = 0;
    std::unordered_map<std::string, bool> permissions;
    std::unordered_map<uint16_t, sockaddr_in> channels;
    std::unordered_map<std::string, uint16_t> channelOf;
};

static std::map<std::string, Alloc> allocations;          // client addr -> alloc
static std::unordered_map<int, std::string> relayFdOwner; // relay fd -> client key
static std::unordered_set<std::string> issuedNonces;
static uint64_t statPackets = 0, statBytes = 0, statDenied = 0;

static std::mt19937_64 rng((uint64_t)std::chrono::high_resolution_clock::now().time_since_epoch().count());

static int64_t nowSec() {
    return (int64_t)std::chrono::duration_cast<std::chrono::seconds>(
        std::chrono::system_clock::now().time_since_epoch()).count();
}
static std::string newNonce() {
    char buf[33];
    for (int i = 0; i < 32; i++) buf[i] = "0123456789abcdef"[rng() & 15];
    buf[32] = 0;
    return std::string(buf);
}

static bool checkCredential(const std::string& user, uint8_t keyOut[16]) {
    size_t colon = user.find(':');
    if (colon == std::string::npos) return false;
    int64_t expiry = 0;
    try { expiry = std::stoll(user.substr(0, colon)); } catch (...) { return false; }
    if (nowSec() >= expiry) return false;
    uint8_t mac[20];
    crypto::hmacSha1((const uint8_t*)turnSecret.data(), turnSecret.size(),
                     (const uint8_t*)user.data(), user.size(), mac);
    // password = base64(mac) — validate the MD5 key against that encoding path
    static const char* B64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    std::string pass;
    size_t i = 0;
    for (; i + 3 <= sizeof(mac) - 2; i += 3) {
        uint32_t v = ((uint32_t)mac[i] << 16) | ((uint32_t)mac[i+1] << 8) | mac[i+2];
        pass.push_back(B64[(v >> 18) & 63]); pass.push_back(B64[(v >> 12) & 63]);
        pass.push_back(B64[(v >> 6) & 63]);  pass.push_back(B64[v & 63]);
    }
    // 20 bytes = 6 full groups (18 bytes) + 2 tail bytes -> 3 chars + '='
    uint32_t v = ((uint32_t)mac[i] << 16) | ((uint32_t)mac[i+1] << 8);
    pass.push_back(B64[(v >> 18) & 63]);
    pass.push_back(B64[(v >> 12) & 63]);
    pass.push_back(B64[(v >> 6) & 63]);
    pass.push_back('=');
    std::string seed = user + ':' + realm + ':' + pass;
    uint8_t digest[16];
    crypto::md5((const uint8_t*)seed.data(), seed.size(), digest);
    memcpy(keyOut, digest, 16);
    return true;
}

static bool verifyMI(const uint8_t* raw, size_t n, const Parsed& p, const uint8_t key[16]) {
    (void)n;
    if (!p.miOffset) return false;
    // HMAC covers the message up to (not including) the MI attribute, with the
    // header length adjusted to account for the 24-byte MI attribute (RFC 5389
    // §15.4 as implemented by pion/coturn).
    std::vector<uint8_t> hdr(raw, raw + p.miOffset);
    hdr[2] = (uint8_t)((p.miOffset - 20 + 24) >> 8);
    hdr[3] = (uint8_t)(p.miOffset - 20 + 24);
    uint8_t mac[20];
    crypto::hmacSha1(key, 16, hdr.data(), hdr.size(), mac);
    auto it = p.attrs.find(msg::MI);
    if (it == p.attrs.end()) return false;
    const uint8_t* got = it->second.first;
    uint8_t diff = 0;
    for (int i = 0; i < 20; i++) diff |= (uint8_t)(got[i] ^ mac[i]);
    return diff == 0;
}

// ===================== allocation handling =================

static void destroyAlloc(int ep, const std::string& clientKey) {
    auto it = allocations.find(clientKey);
    if (it == allocations.end()) return;
    if (it->second.relayFd >= 0) {
        epoll_ctl(ep, EPOLL_CTL_DEL, it->second.relayFd, nullptr);
        close(it->second.relayFd);
        relayFdOwner.erase(it->second.relayFd);
    }
    allocations.erase(it);
}

static bool makeError(int fd, const sockaddr_in& to, const Parsed& req, uint16_t code,
                      const char* reason, bool withNonce) {
    uint8_t outbuf[512];
    std::string nonce;
    if (withNonce && code == 401) {
        nonce = newNonce();
        issuedNonces.insert(nonce);
    }
    OutBuf r;
    r.putType(decodeMethod(req.type), msg::ERROR);
    r.putTxid(req.txid);
    r.errorCode(code, reason);
    if (withNonce) { r.attrStr(msg::REALM, realm); r.attrStr(msg::NONCE, nonce); }
    size_t n;
    r.finish(outbuf, n);
    sendto(fd, outbuf, n, 0, (const sockaddr*)&to, sizeof(to));
    return true;
}

// Auth: USERNAME+NONCE present, nonce issued by us, credential key derived,
// raw-bytes MESSAGE-INTEGRITY verified. Consumes the nonce on success.
static bool authCheck(const uint8_t* raw, size_t n, const Parsed& p,
                      std::string& userOut, std::string& nonceOut, uint8_t keyOut[16]) {
    auto u = p.attrs.find(msg::USERNAME);
    auto nn = p.attrs.find(msg::NONCE);
    if (u == p.attrs.end() || nn == p.attrs.end()) return false;
    userOut.assign((const char*)u->second.first, u->second.second);
    nonceOut.assign((const char*)nn->second.first, nn->second.second);
    if (!checkCredential(userOut, keyOut)) return false;
    if (issuedNonces.erase(nonceOut) == 0) return false;
    return verifyMI(raw, n, p, keyOut);
}

static void handleStun(int ep, int listenFd, const uint8_t* buf, size_t n,
                       const sockaddr_in& from) {
    std::string clientKey = addrKey(from);
    Parsed p;
    if (!parseMsg(buf, n, p)) { statDenied++; return; }
    uint16_t method = decodeMethod(p.type);

    // ---- Binding (no auth) ----
    if (method == msg::BINDING) {
        uint8_t outbuf[256];
        OutBuf r;
        r.putType(msg::BINDING, msg::SUCCESS);
        r.putTxid(p.txid);
        r.xorAddr(msg::XOR_MAPPED, from);
        size_t len; r.finish(outbuf, len);
        sendto(listenFd, outbuf, len, 0, (const sockaddr*)&from, sizeof(from));
        return;
    }

    // ---- Send indication (no auth; client must hold an allocation) ----
    if (method == msg::SEND) {
        auto sIt = allocations.find(clientKey);
        if (sIt == allocations.end()) { statDenied++; return; }
        Alloc& sa = sIt->second;
        auto dA = p.attrs.find(msg::DATA_PAYLOAD);
        auto xa = p.attrs.find(msg::XOR_PEER);
        if (dA == p.attrs.end() || xa == p.attrs.end() || xa->second.second < 8) return;
        const uint8_t* d = xa->second.first;
        if (d[1] != 0x01) return;
        sockaddr_in peer{};
        peer.sin_family = AF_INET;
        peer.sin_port = htons(((uint16_t)d[2] << 8 | d[3]) ^ (uint16_t)(MAGIC >> 16));
        uint32_t ip = (((uint32_t)d[4] << 24) | ((uint32_t)d[5] << 16) | ((uint32_t)d[6] << 8) | (uint32_t)d[7]) ^ MAGIC;
        peer.sin_addr.s_addr = htonl(ip);
        sendto(sa.relayFd, dA->second.first, dA->second.second, 0, (sockaddr*)&peer, sizeof(peer));
        statPackets++; statBytes += dA->second.second;
        return;
    }

    // ---- auth-gated methods ----
    std::string user, nonce; uint8_t key[16];
    bool authed = authCheck(buf, n, p, user, nonce, key);
    if (!authed) {
        makeError(listenFd, from, p, 401, "Unauthorized", true);
        statDenied++;
        return;
    }

    if (method == msg::ALLOCATE) {
        auto rt = p.attrs.find(msg::REQUESTED_TRANSPORT);
        if (rt == p.attrs.end() || rt->second.second != 1 || rt->second.first[0] != 17) {
            makeError(listenFd, from, p, 442, "Unsupported Transport Address", false);
            return;
        }
        if (allocations.count(clientKey) > 0) { destroyAlloc(ep, clientKey); }
        if ((int)allocations.size() >= maxAlloc) {
            makeError(listenFd, from, p, 508, "Allocation Quota Reached", false);
            return;
        }
        int relayFd = socket(AF_INET, SOCK_DGRAM, 0);
        if (relayFd < 0) return;
        int flags = fcntl(relayFd, F_GETFL, 0);
        fcntl(relayFd, F_SETFL, flags | O_NONBLOCK);
        sockaddr_in bindAddr{};
        bindAddr.sin_family = AF_INET;
        bindAddr.sin_port = 0;
        inet_pton(AF_INET, publicIP.c_str(), &bindAddr.sin_addr);
        if (bind(relayFd, (sockaddr*)&bindAddr, sizeof(bindAddr)) < 0) { close(relayFd); return; }
        epoll_event ev{};
        ev.events = EPOLLIN;
        ev.data.fd = relayFd;
        if (epoll_ctl(ep, EPOLL_CTL_ADD, relayFd, &ev) < 0) { close(relayFd); return; }

        Alloc a;
        a.user = user;
        a.relayFd = relayFd;
        socklen_t sl = sizeof(a.relayAddr);
        getsockname(relayFd, (sockaddr*)&a.relayAddr, &sl);
        a.expiresAt = nowSec() + defaultLifetime;
        allocations.emplace(clientKey, a);
        relayFdOwner[relayFd] = clientKey;

        uint8_t outbuf[256];
        OutBuf r;
        r.putType(msg::ALLOCATE, msg::SUCCESS);
        r.putTxid(p.txid);
        r.xorAddr(msg::XOR_RELAYED, a.relayAddr);
        r.xorAddr(msg::XOR_MAPPED, from);
        r.attrU32(msg::LIFETIME, defaultLifetime);
        r.finishMI(key);
        size_t len = r.b.size();
        memcpy(outbuf, r.b.data(), len);
        sendto(listenFd, outbuf, len, 0, (const sockaddr*)&from, sizeof(from));
        return;
    }

    auto aIt = allocations.find(clientKey);
    if (aIt == allocations.end()) {
        makeError(listenFd, from, p, 437, "Allocation Mismatch", false);
        statDenied++;
        return;
    }
    Alloc& a = aIt->second;

    if (method == msg::REFRESH) {
        uint32_t lifetime = defaultLifetime;
        auto lt = p.attrs.find(msg::LIFETIME);
        if (lt != p.attrs.end() && lt->second.second >= 4)
            lifetime = ((uint32_t)lt->second.first[0] << 24) | ((uint32_t)lt->second.first[1] << 16) |
                       ((uint32_t)lt->second.first[2] << 8) | (uint32_t)lt->second.first[3];
        uint8_t outbuf[256];
        OutBuf r;
        r.putType(msg::REFRESH, msg::SUCCESS);
        r.putTxid(p.txid);
        r.attrU32(msg::LIFETIME, lifetime);
        r.finishMI(key);
        size_t len = r.b.size();
        memcpy(outbuf, r.b.data(), len);
        sendto(listenFd, outbuf, len, 0, (const sockaddr*)&from, sizeof(from));
        if (lifetime == 0) destroyAlloc(ep, clientKey);
        else a.expiresAt = nowSec() + lifetime;
        return;
    }

    if (method == msg::CREATE_PERM) {
        auto xa = p.attrs.find(msg::XOR_PEER);
        if (xa == p.attrs.end() || xa->second.second < 8 || xa->second.first[1] != 0x01) {
            makeError(listenFd, from, p, 400, "Bad Request", false);
            return;
        }
        sockaddr_in peer{};
        peer.sin_family = AF_INET;
        const uint8_t* d = xa->second.first;
        peer.sin_port = htons(((uint16_t)d[2] << 8 | d[3]) ^ (uint16_t)(MAGIC >> 16));
        uint32_t ip = (((uint32_t)d[4] << 24) | ((uint32_t)d[5] << 16) | ((uint32_t)d[6] << 8) | d[7]) ^ MAGIC;
        peer.sin_addr.s_addr = htonl(ip);
        a.permissions[addrKey(peer)] = true;
        uint8_t outbuf[256];
        OutBuf r;
        r.putType(msg::CREATE_PERM, msg::SUCCESS);
        r.putTxid(p.txid);
        r.finishMI(key);
        size_t len = r.b.size();
        memcpy(outbuf, r.b.data(), len);
        sendto(listenFd, outbuf, len, 0, (const sockaddr*)&from, sizeof(from));
        return;
    }

    if (method == msg::CHANNEL_BIND) {
        auto cn = p.attrs.find(msg::CHANNEL_NUMBER);
        auto xa = p.attrs.find(msg::XOR_PEER);
        if (cn == p.attrs.end() || cn->second.second < 4 ||
            xa == p.attrs.end() || xa->second.second < 8 || xa->second.first[1] != 0x01) {
            makeError(listenFd, from, p, 400, "Bad Request", false);
            return;
        }
        uint16_t ch = ((uint16_t)cn->second.first[0] << 8) | cn->second.first[1];
        if (ch < 0x4000 || ch > 0x7FFF) {
            makeError(listenFd, from, p, 400, "Bad Request", false);
            return;
        }
        sockaddr_in peer{};
        peer.sin_family = AF_INET;
        const uint8_t* d = xa->second.first;
        peer.sin_port = htons(((uint16_t)d[2] << 8 | d[3]) ^ (uint16_t)(MAGIC >> 16));
        uint32_t ip = (((uint32_t)d[4] << 24) | ((uint32_t)d[5] << 16) | ((uint32_t)d[6] << 8) | (uint32_t)d[7]) ^ MAGIC;
        peer.sin_addr.s_addr = htonl(ip);
        a.channels[ch] = peer;
        a.channelOf[addrKey(peer)] = ch;
        a.permissions[addrKey(peer)] = true;
        uint8_t outbuf[256];
        OutBuf r;
        r.putType(msg::CHANNEL_BIND, msg::SUCCESS);
        r.putTxid(p.txid);
        r.finishMI(key);
        size_t len = r.b.size();
        memcpy(outbuf, r.b.data(), len);
        sendto(listenFd, outbuf, len, 0, (const sockaddr*)&from, sizeof(from));
        return;
    }

    // unknown methods: error 420; DATA indications from clients are dropped
    if (method == msg::DATA) return;
    makeError(listenFd, from, p, 420, "Unknown STUN Method", false);
}

static void handleChannelData(const sockaddr_in& from, const uint8_t* buf, size_t n) {
    if (n < 4) return;
    uint16_t ch = ((uint16_t)buf[0] << 8) | buf[1];
    if (ch < 0x4000 || ch > 0x7FFF) return;
    uint16_t len = ((uint16_t)buf[2] << 8) | buf[3];
    if (4 + (size_t)len > n) return;
    std::string clientKey = addrKey(from);
    auto it = allocations.find(clientKey);
    if (it == allocations.end()) return;
    Alloc& a = it->second;
    auto chIt = a.channels.find(ch);
    if (chIt == a.channels.end()) return;
    sendto(a.relayFd, buf + 4, len, 0, (sockaddr*)&chIt->second, sizeof(chIt->second));
    statPackets++; statBytes += len;
}

// ===================== control server =================

static void controlResponse(int fd) {
    char req[4096]; ssize_t r = recv(fd, req, sizeof(req) - 1, 0);
    if (r <= 0) { close(fd); return; }
    req[r] = 0;
    bool ok = false, health = false;
    if (strstr(req, "GET /health")) health = true;
    if (!health) {
        const char* auth = strstr(req, "Authorization: Bearer ");
        if (auth) {
            std::string given;
            const char* p = auth + 22;
            while (*p && *p != '\r' && *p != '\n') given += *p++;
            ok = !sfuSecret.empty() && given == sfuSecret;
        }
    }
    const char* body;
    char statbuf[512];
    if (health) body = "{\"ok\":true}";
    else if (!ok) body = "{\"error\":\"unauthorized\"}";
    else {
        snprintf(statbuf, sizeof(statbuf),
            "{\"allocations\":%zu,\"packets\":%llu,\"bytes\":%llu,\"denied\":%llu}",
            allocations.size(), (unsigned long long)statPackets,
            (unsigned long long)statBytes, (unsigned long long)statDenied);
        body = statbuf;
    }
    char resp[1024];
    int code = (!health && !ok) ? 401 : 200;
    int len = snprintf(resp, sizeof(resp),
        "HTTP/1.1 %d %s\r\nContent-Type: application/json\r\nContent-Length: %zu\r\nConnection: close\r\n\r\n%s",
        code, code == 200 ? "OK" : "Unauthorized", strlen(body), body);
    send(fd, resp, (size_t)len, 0);
    close(fd);
}

// ===================== main =================

int main() {
    turnSecret = getenv("TURN_SECRET") ? getenv("TURN_SECRET") : "";
    sfuSecret = getenv("SFU_SECRET") ? getenv("SFU_SECRET") : "";
    realm = getenv("REALM") ? getenv("REALM") : "chatapp";
    publicIP = getenv("PUBLIC_IP") ? getenv("PUBLIC_IP") : "127.0.0.1";
    const char* v;
    turnListen = (v = getenv("TURN_LISTEN")) && *v ? atoi(v) : 3479;
    controlPort = (v = getenv("CONTROL_PORT")) && *v ? atoi(v) : 8099;
    maxAlloc = (v = getenv("MAX_ALLOCATIONS")) && *v ? atoi(v) : 4096;
    if ((v = getenv("LIFETIME_S")) && *v) defaultLifetime = (uint32_t)atoi(v);
    if (turnSecret.empty()) {
        fprintf(stderr, "FATAL: TURN_SECRET is required\n");
        return 1;
    }

    int listenFd = socket(AF_INET, SOCK_DGRAM, 0);
    if (listenFd < 0) { perror("socket"); return 1; }
    int one = 1;
    setsockopt(listenFd, SOL_SOCKET, SO_REUSEADDR, &one, sizeof(one));
    sockaddr_in addr{};
    addr.sin_family = AF_INET;
    addr.sin_port = htons((uint16_t)turnListen);
    inet_pton(AF_INET, publicIP.c_str(), &addr.sin_addr);
    if (bind(listenFd, (sockaddr*)&addr, sizeof(addr)) < 0) { perror("bind TURN"); return 1; }

    int ctrlFd = socket(AF_INET, SOCK_STREAM, 0);
    setsockopt(ctrlFd, SOL_SOCKET, SO_REUSEADDR, &one, sizeof(one));
    sockaddr_in caddr{};
    caddr.sin_family = AF_INET;
    caddr.sin_port = htons((uint16_t)controlPort);
    caddr.sin_addr.s_addr = htonl(INADDR_ANY);
    if (bind(ctrlFd, (sockaddr*)&caddr, sizeof(caddr)) < 0 || listen(ctrlFd, 64) < 0) {
        perror("bind control"); return 1;
    }

    int ep = epoll_create1(0);
    if (ep < 0) { perror("epoll"); return 1; }
    epoll_event ev{};
    ev.events = EPOLLIN; ev.data.fd = listenFd;
    epoll_ctl(ep, EPOLL_CTL_ADD, listenFd, &ev);
    ev.events = EPOLLIN; ev.data.fd = ctrlFd;
    epoll_ctl(ep, EPOLL_CTL_ADD, ctrlFd, &ev);

    fprintf(stderr, "sfu-forwarder: TURN relay on %s:%d, control :%d (max %d allocations)\n",
            publicIP.c_str(), turnListen, controlPort, maxAlloc);

    for (;;) {
        epoll_event events[256];
        int n = epoll_wait(ep, events, 256, 1000);
        int64_t now = nowSec();
        // expire allocations
        std::vector<std::string> dead;
        for (auto& kv : allocations) if (kv.second.expiresAt <= now) dead.push_back(kv.first);
        for (auto& k : dead) destroyAlloc(ep, k);
        if (n < 0) { if (errno != EINTR) perror("epoll_wait"); continue; }

        for (int i = 0; i < n; i++) {
            int fd = events[i].data.fd;
            if (fd == ctrlFd) {
                int c = accept(ctrlFd, nullptr, nullptr);
                if (c >= 0) controlResponse(c);
                continue;
            }
            if (fd == listenFd) {
                uint8_t buf[4096];
                sockaddr_in from{};
                socklen_t fl = sizeof(from);
                ssize_t rn = recvfrom(listenFd, buf, sizeof(buf), 0, (sockaddr*)&from, &fl);
                if (rn <= 0) continue;
                if (rn >= 4 && (buf[0] & 0xC0) == 0x40) {
                    handleChannelData(from, buf, (size_t)rn);
                    continue;
                }
                if (rn >= 20 && ((uint32_t)buf[4] << 24 | (uint32_t)buf[5] << 16 |
                                 (uint32_t)buf[6] << 8 | buf[7]) == MAGIC) {
                    handleStun(ep, listenFd, buf, (size_t)rn, from);
                    continue;
                }
                statDenied++;
                continue;
            }
            // relayed socket: peer packet
            auto ownerIt = relayFdOwner.find(fd);
            if (ownerIt == relayFdOwner.end()) continue;
            uint8_t buf[4096];
            sockaddr_in from{};
            socklen_t fl = sizeof(from);
            ssize_t rn = recvfrom(fd, buf, sizeof(buf), 0, (sockaddr*)&from, &fl);
            if (rn <= 0) continue;
            auto aIt = allocations.find(ownerIt->second);
            if (aIt == allocations.end()) continue;
            Alloc& a = aIt->second;
            std::string pk = addrKey(from);
            // client address recovered from its stored textual key
            unsigned cip = 0, cport = 0;
            sscanf(ownerIt->second.c_str(), "%u:%u", &cip, &cport);
            sockaddr_in ca{};
            ca.sin_family = AF_INET;
            ca.sin_addr.s_addr = htonl(cip);
            ca.sin_port = htons((uint16_t)cport);
            // channel fast path
            auto chIt = a.channelOf.find(pk);
            if (chIt != a.channelOf.end()) {
                uint8_t frame[4100];
                frame[0] = (uint8_t)(chIt->second >> 8);
                frame[1] = (uint8_t)chIt->second;
                frame[2] = (uint8_t)(rn >> 8);
                frame[3] = (uint8_t)rn;
                memcpy(frame + 4, buf, (size_t)rn);
                size_t total = 4 + (size_t)rn;
                while (total % 4) { frame[total++] = 0; }
                sendto(listenFd, frame, total, 0, (sockaddr*)&ca, sizeof(ca));
                statPackets++; statBytes += (uint64_t)rn;
                continue;
            }
            if (!a.permissions[pk]) { statDenied++; continue; }
            OutBuf r;
            r.putType(msg::DATA, msg::INDICATION);
            uint8_t rnd[12]; for (int i = 0; i < 12; i++) rnd[i] = (uint8_t)(rng() >> 8);
            r.putTxid(rnd);
            r.xorAddr(msg::XOR_PEER, from);
            r.attr(msg::DATA_PAYLOAD, buf, (uint16_t)rn);
            r.b[2] = (uint8_t)((r.b.size() - 20) >> 8);
            r.b[3] = (uint8_t)(r.b.size() - 20);
            sendto(listenFd, r.b.data(), r.b.size(), 0, (sockaddr*)&ca, sizeof(ca));
            statPackets++; statBytes += (uint64_t)rn;
        }
    }
}
