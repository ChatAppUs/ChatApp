// ChatApp realtime relay — C++17 epoll WebSocket fanout edge.
//
// This is the ultra-low-latency sibling of the Go API's in-process hub:
// the Go API stays the control plane (persists messages, authorizes), and
// publishes fanout events here over an internal HTTP channel; the relay
// owns the hot data plane — 100k+ sockets per node, microsecond fanout,
// no GC pauses.
//
// Client side:  GET /ws?token=<JWT>  (RFC 6455, HS256 JWT verified locally)
// Control side: POST /publish   {"user_ids":[...],"payload":{...}}
//               POST /publish_all {"payload":{...}}
//               POST /counter/incr {"key":"...","n":1}
//               GET  /counter?key=...
//               GET  /health
// Control requests require  Authorization: Bearer <CLUSTER_SECRET>.
//
// Env:
//   WS_PORT        client WebSocket port, default 8300
//   CONTROL_PORT   internal control port, default 8301
//   JWT_SECRET     user-token verification key (required)
//   CLUSTER_SECRET control-plane bearer (required)

#include <arpa/inet.h>
#include <fcntl.h>
#include <netinet/in.h>
#include <netinet/tcp.h>
#include <sys/epoll.h>
#include <sys/socket.h>
#include <unistd.h>

#include <cerrno>
#include <chrono>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <ctime>
#include <iostream>
#include <string>
#include <unordered_map>
#include <unordered_set>
#include <vector>

#include "sha.h"

static std::string getenvOr(const char* k, const char* def) {
    const char* v = getenv(k);
    return (v && *v) ? v : def;
}

// ---- minimal JSON string extraction (control payloads have fixed shape) ----

static std::string jsonString(const std::string& js, const std::string& key) {
    std::string pat = "\"" + key + "\"";
    auto k = js.find(pat);
    if (k == std::string::npos) return "";
    auto colon = js.find(':', k + pat.size());
    if (colon == std::string::npos) return "";
    auto q1 = js.find('"', colon + 1);
    if (q1 == std::string::npos) return "";
    std::string val;
    for (size_t i = q1 + 1; i < js.size(); i++) {
        char c = js[i];
        if (c == '\\' && i + 1 < js.size()) {
            char n = js[i + 1];
            if (n == 'n') val += '\n';
            else if (n == 't') val += '\t';
            else if (n == 'r') val += '\r';
            else val += n;
            i++;
            continue;
        }
        if (c == '"') break;
        val += c;
    }
    return val;
}

// Extract the raw JSON value (object/array) starting at key.
static std::string jsonRaw(const std::string& js, const std::string& key) {
    std::string pat = "\"" + key + "\"";
    auto k = js.find(pat);
    if (k == std::string::npos) return "";
    auto colon = js.find(':', k + pat.size());
    if (colon == std::string::npos) return "";
    size_t i = js.find_first_not_of(" \t\r\n", colon + 1);
    if (i == std::string::npos) return "";
    char open = js[i];
    if (open != '{' && open != '[') return "";
    char close = open == '{' ? '}' : ']';
    int depth = 0;
    bool inStr = false, esc = false;
    for (size_t j = i; j < js.size(); j++) {
        char c = js[j];
        if (esc) { esc = false; continue; }
        if (c == '\\' && inStr) { esc = true; continue; }
        if (c == '"') { inStr = !inStr; continue; }
        if (inStr) continue;
        if (c == open) depth++;
        else if (c == close) { depth--; if (depth == 0) return js.substr(i, j - i + 1); }
    }
    return "";
}

static std::vector<std::string> jsonStringArray(const std::string& js, const std::string& key) {
    std::vector<std::string> out;
    std::string arr = jsonRaw(js, key);
    if (arr.empty()) return out;
    size_t i = 0;
    while ((i = arr.find('"', i)) != std::string::npos) {
        std::string val;
        size_t j = i + 1;
        for (; j < arr.size(); j++) {
            if (arr[j] == '\\' && j + 1 < arr.size()) { val += arr[j + 1]; j++; continue; }
            if (arr[j] == '"') break;
            val += arr[j];
        }
        out.push_back(val);
        i = j + 1;
    }
    return out;
}

// ---- JWT HS256 verification (same contract as the Go API) ----

static bool constantTimeEq(const std::string& a, const std::string& b) {
    if (a.size() != b.size()) return false;
    uint8_t diff = 0;
    for (size_t i = 0; i < a.size(); i++) diff |= (uint8_t)a[i] ^ (uint8_t)b[i];
    return diff == 0;
}

static std::string jwtVerifyHS256(const std::string& token, const std::string& secret) {
    auto d1 = token.find('.');
    auto d2 = d1 == std::string::npos ? std::string::npos : token.find('.', d1 + 1);
    if (d1 == std::string::npos || d2 == std::string::npos) return "";
    std::string body = token.substr(0, d2);
    std::string sig;
    if (!b64urlDecode(token.substr(d2 + 1), sig)) return "";
    std::string expected = hmacSha256(secret, body);
    if (!constantTimeEq(sig, expected)) return "";
    std::string payload;
    if (!b64urlDecode(token.substr(d1 + 1, d2 - d1 - 1), payload)) return "";
    // alg-confusion guard: header must declare HS256.
    std::string header;
    if (!b64urlDecode(token.substr(0, d1), header) || header.find("HS256") == std::string::npos) return "";
    std::string typ = jsonString(payload, "typ");
    if (typ != "access") return "";
    std::string scope = jsonString(payload, "scope");
    if (scope == "admin") return ""; // admin plane never rides the user edge
    std::string expRaw = jsonString(payload, "exp");
    // exp is a JSON number, not a string — parse it directly.
    auto ek = payload.find("\"exp\"");
    if (ek == std::string::npos) return "";
    auto colon = payload.find(':', ek);
    if (colon == std::string::npos) return "";
    long long exp = atoll(payload.c_str() + colon + 1);
    if (exp <= (long long)time(nullptr)) return "";
    return jsonString(payload, "sub");
}

// ---- sockets ----

static int listenOn(int port) {
    int fd = socket(AF_INET6, SOCK_STREAM, 0);
    if (fd < 0) fd = socket(AF_INET, SOCK_STREAM, 0);
    if (fd < 0) return -1;
    int one = 1;
    setsockopt(fd, SOL_SOCKET, SO_REUSEADDR, &one, sizeof(one));
    struct sockaddr_in6 addr6{};
    addr6.sin6_family = AF_INET6;
    addr6.sin6_addr = in6addr_any;
    addr6.sin6_port = htons((uint16_t)port);
    if (bind(fd, (struct sockaddr*)&addr6, sizeof(addr6)) != 0) {
        struct sockaddr_in addr4{};
        addr4.sin_family = AF_INET;
        addr4.sin_addr.s_addr = INADDR_ANY;
        addr4.sin_port = htons((uint16_t)port);
        if (bind(fd, (struct sockaddr*)&addr4, sizeof(addr4)) != 0) { close(fd); return -1; }
    }
    if (listen(fd, 4096) != 0) { close(fd); return -1; }
    int flags = fcntl(fd, F_GETFL, 0);
    fcntl(fd, F_SETFL, flags | O_NONBLOCK);
    return fd;
}

static void setNonblock(int fd) {
    int flags = fcntl(fd, F_GETFL, 0);
    fcntl(fd, F_SETFL, flags | O_NONBLOCK);
    int one = 1;
    setsockopt(fd, IPPROTO_TCP, TCP_NODELAY, &one, sizeof(one));
}

// ---- WebSocket framing ----

enum class ConnKind { PendingHandshake, WebSocket, ControlHttp };

struct Conn {
    int fd = -1;
    ConnKind kind = ConnKind::PendingHandshake;
    bool isControl = false;
    std::string userID;          // set after WS auth
    std::string inbuf;
    std::string outbuf;          // pending bytes for nonblocking write
    bool closing = false;
    std::chrono::steady_clock::time_point started = std::chrono::steady_clock::now();
};

static std::unordered_map<int, Conn> conns;
static std::unordered_map<std::string, std::unordered_set<int>> byUser;
static std::unordered_map<std::string, long long> counters;

static void dropConn(int ep, int fd) {
    auto it = conns.find(fd);
    if (it != conns.end()) {
        if (!it->second.userID.empty()) {
            auto uit = byUser.find(it->second.userID);
            if (uit != byUser.end()) {
                uit->second.erase(fd);
                if (uit->second.empty()) byUser.erase(uit);
            }
        }
        conns.erase(it);
    }
    epoll_ctl(ep, EPOLL_CTL_DEL, fd, nullptr);
    close(fd);
}

// Queue bytes for a connection; flush what we can immediately.
static void queueOut(int ep, Conn& c, const std::string& data) {
    c.outbuf += data;
    size_t off = 0;
    while (off < c.outbuf.size()) {
        ssize_t n = send(c.fd, c.outbuf.data() + off, c.outbuf.size() - off, MSG_NOSIGNAL);
        if (n < 0) {
            if (errno == EAGAIN || errno == EWOULDBLOCK) break;
            c.closing = true;
            break;
        }
        off += (size_t)n;
    }
    c.outbuf.erase(0, off);
    if (!c.outbuf.empty()) {
        epoll_event ev{};
        ev.events = EPOLLIN | EPOLLOUT | EPOLLET;
        ev.data.fd = c.fd;
        epoll_ctl(ep, EPOLL_CTL_MOD, c.fd, &ev);
    } else if (c.closing) {
        dropConn(ep, c.fd);
    }
}

static std::string wsFrame(const std::string& payload) {
    std::string f;
    f += (char)0x81; // FIN + text
    size_t n = payload.size();
    if (n < 126) {
        f += (char)n;
    } else if (n <= 0xFFFF) {
        f += (char)126;
        f += (char)(n >> 8);
        f += (char)(n & 0xFF);
    } else {
        f += (char)127;
        for (int i = 7; i >= 0; i--) f += (char)((n >> (i * 8)) & 0xFF);
    }
    f += payload;
    return f;
}

// Deliver a JSON payload to one user's live connections.
static void fanoutUser(int ep, const std::string& userID, const std::string& payload) {
    auto it = byUser.find(userID);
    if (it == byUser.end()) return;
    std::string frame = wsFrame(payload);
    for (int fd : it->second) {
        auto cit = conns.find(fd);
        if (cit != conns.end()) queueOut(ep, cit->second, frame);
    }
}

// ---- HTTP handling (handshake + control plane) ----

static std::string headerValue(const std::string& req, const std::string& name) {
    std::string needle = "\r\n" + name + ":";
    auto i = req.find(needle);
    if (i == std::string::npos) return "";
    size_t start = i + needle.size();
    while (start < req.size() && req[start] == ' ') start++;
    auto end = req.find("\r\n", start);
    return req.substr(start, end - start);
}

// JSON number field (unquoted).
static long long jsonNumber(const std::string& js, const std::string& key, long long def) {
    std::string pat = "\"" + key + "\"";
    auto k = js.find(pat);
    if (k == std::string::npos) return def;
    auto colon = js.find(':', k + pat.size());
    if (colon == std::string::npos) return def;
    return atoll(js.c_str() + colon + 1);
}

static void httpRespond(int ep, Conn& c, int status, const std::string& body) {
    const char* text = status == 200 ? "OK" : status == 401 ? "Unauthorized" :
                       status == 403 ? "Forbidden" : status == 404 ? "Not Found" : "Bad Request";
    std::string resp = "HTTP/1.1 " + std::to_string(status) + " " + text + "\r\n"
        "Content-Type: application/json\r\n"
        "Content-Length: " + std::to_string(body.size()) + "\r\n"
        "Connection: close\r\n\r\n" + body;
    queueOut(ep, c, resp);
    c.closing = true;
}

static std::string g_jwtSecret, g_clusterSecret;

static void handleHandshake(int ep, Conn& c) {
    std::string& req = c.inbuf;
    // Path + query: GET /ws?token=...
    auto sp1 = req.find(' ');
    auto sp2 = req.find(' ', sp1 + 1);
    if (sp1 == std::string::npos || sp2 == std::string::npos) { c.closing = true; return; }
    std::string target = req.substr(sp1 + 1, sp2 - sp1 - 1);
    if (target.rfind("/ws", 0) != 0) {
        httpRespond(ep, c, 404, "{\"error\":\"not found\"}");
        return;
    }
    std::string token;
    auto q = target.find("token=");
    if (q != std::string::npos) {
        token = target.substr(q + 6);
        auto amp = token.find('&');
        if (amp != std::string::npos) token = token.substr(0, amp);
    }
    // Also accept the Authorization header (native clients).
    if (token.empty()) {
        std::string auth = headerValue(req, "Authorization");
        if (auth.rfind("Bearer ", 0) == 0) token = auth.substr(7);
    }
    std::string uid = jwtVerifyHS256(token, g_jwtSecret);
    if (uid.empty()) {
        httpRespond(ep, c, 401, "{\"error\":\"invalid token\"}");
        return;
    }
    std::string key = headerValue(req, "Sec-WebSocket-Key");
    if (key.empty()) {
        httpRespond(ep, c, 400, "{\"error\":\"missing websocket key\"}");
        return;
    }
    std::string accept = b64Encode(sha1Digest(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"));
    std::string resp = "HTTP/1.1 101 Switching Protocols\r\n"
        "Upgrade: websocket\r\nConnection: Upgrade\r\n"
        "Sec-WebSocket-Accept: " + accept + "\r\n\r\n";
    c.userID = uid;
    c.kind = ConnKind::WebSocket;
    byUser[uid].insert(c.fd);
    c.inbuf.clear();
    queueOut(ep, c, resp);
}

// Control-plane handler; auth is captured before the buffer clears.
static void handleControlFull(int ep, Conn& c) {
    std::string& req = c.inbuf;
    std::string auth = headerValue(req, "Authorization");
    if (auth != "Bearer " + g_clusterSecret) {
        httpRespond(ep, c, 401, "{\"error\":\"unauthorized\"}");
        req.clear();
        return;
    }
    std::string clStr = headerValue(req, "Content-Length");
    size_t bodyStart = req.find("\r\n\r\n");
    bodyStart += 4;
    size_t contentLen = clStr.empty() ? 0 : (size_t)atoll(clStr.c_str());
    std::string body = req.substr(bodyStart, contentLen);
    auto sp1 = req.find(' ');
    auto sp2 = req.find(' ', sp1 + 1);
    std::string method = req.substr(0, sp1);
    std::string target = sp1 == std::string::npos ? "" : req.substr(sp1 + 1, sp2 - sp1 - 1);
    req.clear();

    if (method == "POST" && target == "/publish") {
        std::string payload = jsonRaw(body, "payload");
        if (payload.empty()) { httpRespond(ep, c, 400, "{\"error\":\"payload required\"}"); return; }
        for (const auto& uid : jsonStringArray(body, "user_ids")) fanoutUser(ep, uid, payload);
        httpRespond(ep, c, 200, "{\"delivered\":true}");
        return;
    }
    if (method == "POST" && target == "/publish_all") {
        std::string payload = jsonRaw(body, "payload");
        if (payload.empty()) { httpRespond(ep, c, 400, "{\"error\":\"payload required\"}"); return; }
        std::string frame = wsFrame(payload);
        long long delivered = 0;
        for (auto& [fd, conn] : conns) {
            if (conn.kind == ConnKind::WebSocket) { queueOut(ep, conn, frame); delivered++; }
        }
        httpRespond(ep, c, 200, "{\"delivered\":" + std::to_string(delivered) + "}");
        return;
    }
    if (method == "POST" && target == "/counter/incr") {
        std::string key = jsonString(body, "key");
        long long n = jsonNumber(body, "n", 1);
        if (key.empty()) { httpRespond(ep, c, 400, "{\"error\":\"key required\"}"); return; }
        counters[key] += n;
        httpRespond(ep, c, 200, "{\"value\":" + std::to_string(counters[key]) + "}");
        return;
    }
    if (method == "GET" && target.rfind("/counter", 0) == 0) {
        auto q = target.find("key=");
        std::string key = q == std::string::npos ? "" : target.substr(q + 4);
        httpRespond(ep, c, 200, "{\"value\":" + std::to_string(counters[key]) + "}");
        return;
    }
    httpRespond(ep, c, 404, "{\"error\":\"not found\"}");
}

// ---- WebSocket frame parsing (client -> relay: ping/pong/close only; the
// API is the write path, so upstream data frames are dropped) ----

static void handleWsData(int ep, Conn& c) {
    std::string& buf = c.inbuf;
    for (;;) {
        if (buf.size() < 2) return;
        uint8_t b0 = (uint8_t)buf[0], b1 = (uint8_t)buf[1];
        uint8_t opcode = b0 & 0x0F;
        bool masked = b1 & 0x80;
        uint64_t len = b1 & 0x7F;
        size_t hdr = 2;
        if (len == 126) {
            if (buf.size() < 4) return;
            len = (uint8_t)buf[2] << 8 | (uint8_t)buf[3];
            hdr = 4;
        } else if (len == 127) {
            if (buf.size() < 10) return;
            len = 0;
            for (int i = 0; i < 8; i++) len = (len << 8) | (uint8_t)buf[2 + i];
            hdr = 10;
        }
        if (len > (1u << 20)) { c.closing = true; return; } // 1 MiB frame cap
        size_t maskOff = hdr;
        if (masked) hdr += 4;
        if (buf.size() < hdr + len) return;
        std::string payload = buf.substr(hdr, len);
        if (masked) {
            const uint8_t* mask = (const uint8_t*)buf.data() + maskOff;
            for (size_t i = 0; i < payload.size(); i++)
                payload[i] = (char)((uint8_t)payload[i] ^ mask[i % 4]);
        }
        buf.erase(0, hdr + len);
        if (opcode == 0x8) { c.closing = true; queueOut(ep, c, std::string("\x88\x00", 2)); return; }
        if (opcode == 0x9) { // ping -> pong
            std::string pong;
            pong += (char)0x8A;
            pong += (char)payload.size();
            pong += payload;
            queueOut(ep, c, pong);
        }
        // 0x1/0x2 data frames from clients are ignored: all writes flow
        // through the API control plane, which is the system of record.
    }
}

// ---- epoll loop ----

int main() {
    g_jwtSecret = getenvOr("JWT_SECRET", "");
    g_clusterSecret = getenvOr("CLUSTER_SECRET", "");
    if (g_jwtSecret.empty() || g_clusterSecret.empty()) {
        std::cerr << "FATAL: JWT_SECRET and CLUSTER_SECRET are required\n";
        return 1;
    }
    int wsPort = atoi(getenvOr("WS_PORT", "8300").c_str());
    int ctrlPort = atoi(getenvOr("CONTROL_PORT", "8301").c_str());
    int wsFd = listenOn(wsPort);
    int ctrlFd = listenOn(ctrlPort);
    if (wsFd < 0 || ctrlFd < 0) {
        std::cerr << "FATAL: cannot bind ports " << wsPort << "/" << ctrlPort << "\n";
        return 1;
    }

    int ep = epoll_create1(0);
    if (ep < 0) { std::cerr << "FATAL: epoll_create1\n"; return 1; }
    epoll_event lev{};
    lev.events = EPOLLIN;
    lev.data.fd = wsFd;
    epoll_ctl(ep, EPOLL_CTL_ADD, wsFd, &lev);
    lev.data.fd = ctrlFd;
    epoll_ctl(ep, EPOLL_CTL_ADD, ctrlFd, &lev);

    std::cout << "realtime relay: ws=" << wsPort << " control=" << ctrlPort << "\n";
    std::vector<epoll_event> events(1024);

    for (;;) {
        int n = epoll_wait(ep, events.data(), (int)events.size(), 5000);
        if (n < 0) {
            if (errno == EINTR) continue;
            break;
        }
        for (int i = 0; i < n; i++) {
            int fd = events[i].data.fd;
            if (fd == wsFd || fd == ctrlFd) {
                for (;;) {
                    int cfd = accept(fd, nullptr, nullptr);
                    if (cfd < 0) break;
                    setNonblock(cfd);
                    Conn c;
                    c.fd = cfd;
                    c.isControl = (fd == ctrlFd);
                    c.kind = c.isControl ? ConnKind::ControlHttp : ConnKind::PendingHandshake;
                    conns[cfd] = std::move(c);
                    epoll_event cev{};
                    cev.events = EPOLLIN | EPOLLET;
                    cev.data.fd = cfd;
                    epoll_ctl(ep, EPOLL_CTL_ADD, cfd, &cev);
                }
                continue;
            }
            auto it = conns.find(fd);
            if (it == conns.end()) continue;
            Conn& c = it->second;

            if (events[i].events & EPOLLOUT) {
                // Flush backlog, then go back to read-only interest.
                size_t off = 0;
                while (off < c.outbuf.size()) {
                    ssize_t sent = send(fd, c.outbuf.data() + off, c.outbuf.size() - off, MSG_NOSIGNAL);
                    if (sent < 0) {
                        if (errno != EAGAIN && errno != EWOULDBLOCK) c.closing = true;
                        break;
                    }
                    off += (size_t)sent;
                }
                c.outbuf.erase(0, off);
                if (c.outbuf.empty()) {
                    epoll_event cev{};
                    cev.events = EPOLLIN | EPOLLET;
                    cev.data.fd = fd;
                    epoll_ctl(ep, EPOLL_CTL_MOD, fd, &cev);
                }
            }
            if (c.closing) { dropConn(ep, fd); continue; }

            if (events[i].events & (EPOLLIN | EPOLLRDHUP | EPOLLHUP | EPOLLERR)) {
                char buf[65536];
                for (;;) {
                    ssize_t r = recv(fd, buf, sizeof(buf), 0);
                    if (r < 0) {
                        if (errno == EAGAIN || errno == EWOULDBLOCK) break;
                        c.closing = true;
                        break;
                    }
                    if (r == 0) { c.closing = true; break; }
                    c.inbuf.append(buf, (size_t)r);
                    if (c.inbuf.size() > (4u << 20)) { c.closing = true; break; }
                }
                if (!c.closing) {
                    switch (c.kind) {
                        case ConnKind::PendingHandshake:
                            if (c.inbuf.find("\r\n\r\n") != std::string::npos) handleHandshake(ep, c);
                            // Handshake deadline: 10s.
                            if (std::chrono::steady_clock::now() - c.started > std::chrono::seconds(10))
                                c.closing = true;
                            break;
                        case ConnKind::WebSocket:
                            handleWsData(ep, c);
                            break;
                        case ConnKind::ControlHttp: {
                            auto hdrEnd = c.inbuf.find("\r\n\r\n");
                            if (hdrEnd == std::string::npos) break;
                            std::string pathLine = c.inbuf.substr(0, c.inbuf.find("\r\n"));
                            if (pathLine.find("/health") != std::string::npos) {
                                httpRespond(ep, c, 200, "{\"status\":\"ok\",\"users\":" +
                                    std::to_string(byUser.size()) + ",\"connections\":" +
                                    std::to_string(conns.size()) + "}");
                                break;
                            }
                            // Need full body before dispatch.
                            std::string clStr = headerValue(c.inbuf, "Content-Length");
                            size_t want = hdrEnd + 4 + (clStr.empty() ? 0 : (size_t)atoll(clStr.c_str()));
                            if (c.inbuf.size() >= want) handleControlFull(ep, c);
                            break;
                        }
                    }
                }
                if (c.closing && c.outbuf.empty()) dropConn(ep, fd);
                else if (c.closing) {
                    epoll_event cev{};
                    cev.events = EPOLLOUT | EPOLLET;
                    cev.data.fd = fd;
                    epoll_ctl(ep, EPOLL_CTL_MOD, fd, &cev);
                }
            }
        }
    }
    return 0;
}
