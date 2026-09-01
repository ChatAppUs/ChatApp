// ChatApp counters engine — C++17 hot in-memory counters.
//
// Owns the write-hot counter paths that would otherwise be one SQL UPDATE
// per event: hashtag trending (sliding 24h window), post view counts and
// live-room viewer counts. The Go API forwards events here over a
// bearer-authenticated internal channel; the engine answers the cheap read
// queries (trending top-N, current viewers) from memory and periodically
// flushes aggregated deltas back through the API's internal flush endpoint
// (one transaction per cycle) so Postgres stays the durable source of
// truth without the per-event write amplification.
//
// Endpoints (control port, Authorization: Bearer <COUNTERS_SECRET>):
//   GET  /health
//   POST /incr   {"kind":"hashtag|view","key":"<id or tag>","delta":1}
//   POST /viewer {"op":"join|leave","room":"<room id>"}
//   GET  /top/hashtags?limit=20
//   GET  /viewers?room=<id>            -> {"room":..,"viewers":N,"peak":N}
//   GET  /count?kind=view&key=<id>     -> pending delta since last flush
//
// Flush: every FLUSH_INTERVAL_MS the pending deltas are swapped out and
// POSTed to FLUSH_URL with Bearer FLUSH_SECRET as
//   {"hashtags":[{"tag":..,"delta":N}],"views":[{"id":..,"delta":N}],
//    "peaks":[{"room":..,"peak":N,"viewers":N}]}
// Unacknowledged deltas are merged back, so an API outage never loses
// counts.
//
// Env:
//   COUNTERS_PORT        listen port, default 8600
//   COUNTERS_SECRET      control-plane bearer (required; refuses to boot empty)
//   FLUSH_URL            API internal flush endpoint (empty = flush disabled)
//   FLUSH_SECRET         bearer for FLUSH_URL, defaults to CLUSTER_SECRET
//   FLUSH_INTERVAL_MS    default 15000

#include <arpa/inet.h>
#include <netdb.h>
#include <netinet/in.h>
#include <sys/socket.h>
#include <unistd.h>

#include <algorithm>
#include <chrono>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <iostream>
#include <mutex>
#include <string>
#include <thread>
#include <unordered_map>
#include <utility>
#include <vector>

static std::string getenvOr(const char* k, const char* def) {
    const char* v = getenv(k);
    return (v && *v) ? v : def;
}

// ---- minimal fixed-shape JSON helpers (same convention as services/realtime) ----

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
            val += js[i + 1];
            i++;
            continue;
        }
        if (c == '"') break;
        val += c;
    }
    return val;
}

static long long jsonNumber(const std::string& js, const std::string& key, long long def) {
    std::string pat = "\"" + key + "\"";
    auto k = js.find(pat);
    if (k == std::string::npos) return def;
    auto colon = js.find(':', k + pat.size());
    if (colon == std::string::npos) return def;
    auto s = js.find_first_of("-0123456789", colon + 1);
    if (s == std::string::npos) return def;
    return std::strtoll(js.c_str() + s, nullptr, 10);
}

static std::string jsonEscape(const std::string& s) {
    std::string out;
    out.reserve(s.size() + 8);
    for (char c : s) {
        unsigned char u = static_cast<unsigned char>(c);
        if (c == '"' || c == '\\') { out += '\\'; out += c; }
        else if (u < 0x20) { char buf[8]; snprintf(buf, sizeof buf, "\\u%04x", u); out += buf; }
        else out += c;
    }
    return out;
}

// ---- state ----

static const int WINDOW = 24; // hourly buckets, sliding one-day horizon

struct HashCounter {
    long long pending = 0;              // not yet flushed
    long long buckets[WINDOW] = {};     // ring indexed by epoch hour
    long long seenHour[WINDOW] = {};    // epoch hour each slot holds
};

struct ViewerCount {
    long long viewers = 0;
    long long peak = 0;
};

static std::mutex gMu;
static std::unordered_map<std::string, HashCounter> gHashtags;
static std::unordered_map<std::string, long long> gViews; // pending deltas
static std::unordered_map<std::string, ViewerCount> gRooms;

static long long epochHour() {
    return std::chrono::duration_cast<std::chrono::hours>(
               std::chrono::system_clock::now().time_since_epoch()).count();
}

static void incrHashtag(const std::string& tag, long long d) {
    std::lock_guard<std::mutex> lk(gMu);
    auto& h = gHashtags[tag];
    h.pending += d;
    long long now = epochHour();
    int slot = static_cast<int>(now % WINDOW);
    if (h.seenHour[slot] != now) { h.buckets[slot] = 0; h.seenHour[slot] = now; }
    h.buckets[slot] += d;
    // Hard memory bound: trending only needs the surviving head.
    if (gHashtags.size() > 1'000'000) {
        for (auto it = gHashtags.begin(); it != gHashtags.end();) {
            bool cold = it->second.pending == 0;
            for (int i = 0; i < WINDOW && cold; i++)
                cold = it->second.buckets[i] == 0;
            if (cold) it = gHashtags.erase(it); else ++it;
        }
    }
}

static void incrView(const std::string& id, long long d) {
    std::lock_guard<std::mutex> lk(gMu);
    gViews[id] += d;
    if (gViews.size() > 2'000'000) {
        for (auto it = gViews.begin(); it != gViews.end();)
            if (it->second == 0) it = gViews.erase(it); else ++it;
    }
}

static void viewerOp(const std::string& room, bool join) {
    std::lock_guard<std::mutex> lk(gMu);
    auto& v = gRooms[room];
    if (join) {
        v.viewers++;
        if (v.viewers > v.peak) v.peak = v.viewers;
    } else if (v.viewers > 0) {
        v.viewers--;
    }
}

// ---- flush ----

static std::string gFlushURL, gFlushSecret;

// Swap pending deltas out of the maps; the caller re-merges them on failure.
static void snapshotPending(std::vector<std::pair<std::string, long long>>& tags,
                            std::vector<std::pair<std::string, long long>>& views,
                            std::vector<std::pair<std::string, ViewerCount>>& rooms) {
    std::lock_guard<std::mutex> lk(gMu);
    for (auto it = gHashtags.begin(); it != gHashtags.end();) {
        bool cold = it->second.pending == 0;
        for (int i = 0; i < WINDOW && cold; i++)
            cold = it->second.buckets[i] == 0;
        if (cold) { it = gHashtags.erase(it); continue; }
        if (it->second.pending != 0) {
            tags.emplace_back(it->first, it->second.pending);
            it->second.pending = 0;
        }
        ++it;
    }
    for (auto it = gViews.begin(); it != gViews.end();) {
        if (it->second == 0) { it = gViews.erase(it); continue; }
        views.emplace_back(it->first, it->second);
        it->second = 0;
        ++it;
    }
    for (auto it = gRooms.begin(); it != gRooms.end();) {
        auto v = it->second;
        if (v.viewers == 0 && v.peak == 0) { it = gRooms.erase(it); continue; }
        rooms.emplace_back(it->first, v);
        ++it;
    }
}

static void restoreFailed(const std::vector<std::pair<std::string, long long>>& tags,
                          const std::vector<std::pair<std::string, long long>>& views,
                          const std::vector<std::pair<std::string, ViewerCount>>& rooms) {
    std::lock_guard<std::mutex> lk(gMu);
    for (auto& [tag, d] : tags) gHashtags[tag].pending += d;
    for (auto& [id, d] : views) gViews[id] += d;
    for (auto& [room, v] : rooms)
        if (v.viewers > 0 || v.peak > 0) gRooms[room] = v;
}

// Minimal blocking HTTP client to the API internal flush endpoint.
static bool httpPost(const std::string& url, const std::string& bearer, const std::string& body) {
    std::string host = url, path = "/";
    auto p0 = url.find("://");
    if (p0 != std::string::npos) host = url.substr(p0 + 3);
    auto slash = host.find('/');
    if (slash != std::string::npos) { path = host.substr(slash); host = host.substr(0, slash); }
    int port = 80;
    auto colon = host.find(':');
    if (colon != std::string::npos) { port = atoi(host.c_str() + colon + 1); host = host.substr(0, colon); }

    addrinfo hints{}, *res = nullptr;
    hints.ai_family = AF_UNSPEC;
    hints.ai_socktype = SOCK_STREAM;
    if (getaddrinfo(host.c_str(), nullptr, &hints, &res) != 0 || !res) return false;
    sockaddr_storage addr{};
    memcpy(&addr, res->ai_addr, res->ai_addrlen);
    freeaddrinfo(res);
    if (addr.ss_family == AF_INET) reinterpret_cast<sockaddr_in*>(&addr)->sin_port = htons(port);
    else if (addr.ss_family == AF_INET6) reinterpret_cast<sockaddr_in6*>(&addr)->sin6_port = htons(port);
    else return false;

    int fd = socket(addr.ss_family, SOCK_STREAM, 0);
    if (fd < 0) return false;
    struct timeval tv{3, 0};
    setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &tv, sizeof tv);
    setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, &tv, sizeof tv);
    socklen_t alen = addr.ss_family == AF_INET ? sizeof(sockaddr_in) : sizeof(sockaddr_in6);
    if (connect(fd, reinterpret_cast<sockaddr*>(&addr), alen) != 0) { close(fd); return false; }

    std::string req = "POST " + path + " HTTP/1.1\r\nHost: " + host +
        "\r\nAuthorization: Bearer " + bearer +
        "\r\nContent-Type: application/json\r\nContent-Length: " + std::to_string(body.size()) +
        "\r\nConnection: close\r\n\r\n" + body;
    size_t off = 0;
    while (off < req.size()) {
        ssize_t n = send(fd, req.data() + off, req.size() - off, 0);
        if (n <= 0) { close(fd); return false; }
        off += static_cast<size_t>(n);
    }
    char buf[1024];
    ssize_t n = recv(fd, buf, sizeof buf, 0);
    close(fd);
    if (n <= 0) return false;
    std::string resp(buf, static_cast<size_t>(n));
    return resp.find(" 200") != std::string::npos || resp.find(" 201") != std::string::npos;
}

static void flushLoop(int intervalMs) {
    for (;;) {
        std::this_thread::sleep_for(std::chrono::milliseconds(intervalMs));
        if (gFlushURL.empty()) continue;
        std::vector<std::pair<std::string, long long>> tags, views;
        std::vector<std::pair<std::string, ViewerCount>> rooms;
        snapshotPending(tags, views, rooms);
        bool anyRoom = false;
        for (auto& [room, v] : rooms) anyRoom = anyRoom || v.peak > 0 || v.viewers > 0;
        if (tags.empty() && views.empty() && !anyRoom) continue;

        std::string body = "{";
        body += "\"hashtags\":[";
        for (size_t i = 0; i < tags.size(); i++) {
            if (i) body += ',';
            body += "{\"tag\":\"" + jsonEscape(tags[i].first) + "\",\"delta\":" + std::to_string(tags[i].second) + "}";
        }
        body += "],\"views\":[";
        for (size_t i = 0; i < views.size(); i++) {
            if (i) body += ',';
            body += "{\"id\":\"" + jsonEscape(views[i].first) + "\",\"delta\":" + std::to_string(views[i].second) + "}";
        }
        body += "],\"peaks\":[";
        bool firstRoom = true;
        for (auto& [room, v] : rooms) {
            if (v.peak == 0 && v.viewers == 0) continue;
            if (!firstRoom) body += ',';
            firstRoom = false;
            body += "{\"room\":\"" + jsonEscape(room) + "\",\"peak\":" + std::to_string(v.peak) +
                    ",\"viewers\":" + std::to_string(v.viewers) + "}";
        }
        body += "]}";
        if (!httpPost(gFlushURL, gFlushSecret, body)) {
            restoreFailed(tags, views, rooms);
        }
    }
}

// ---- HTTP control server ----

static std::string gSecret;

static bool constantTimeEq(const std::string& a, const std::string& b) {
    if (a.size() != b.size()) return false;
    unsigned char diff = 0;
    for (size_t i = 0; i < a.size(); i++) diff |= static_cast<unsigned char>(a[i] ^ b[i]);
    return diff == 0;
}

static void respond(int fd, int status, const std::string& body) {
    const char* text = status == 200 ? "OK" : status == 201 ? "Created"
                     : status == 400 ? "Bad Request" : status == 401 ? "Unauthorized"
                     : status == 404 ? "Not Found" : "Internal Server Error";
    std::string out = "HTTP/1.1 " + std::to_string(status) + " " + text +
        "\r\nContent-Type: application/json\r\nContent-Length: " + std::to_string(body.size()) +
        "\r\nCache-Control: no-store\r\nConnection: close\r\n\r\n" + body;
    send(fd, out.data(), out.size(), MSG_NOSIGNAL);
}

static void handleClient(int fd) {
    char buf[16384];
    std::string req;
    struct timeval tv{5, 0};
    setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &tv, sizeof tv);
    size_t need = SIZE_MAX;
    for (;;) {
        if (req.size() >= need) break;
        ssize_t n = recv(fd, buf, sizeof buf, 0);
        if (n <= 0) { close(fd); return; }
        req.append(buf, static_cast<size_t>(n));
        if (req.size() > 16384) {
            respond(fd, 400, "{\"error\":\"request too large\"}");
            close(fd);
            return;
        }
        auto hdrs = req.find("\r\n\r\n");
        if (hdrs == std::string::npos) continue;
        need = hdrs + 4;
        auto cl = req.find("Content-Length:");
        if (cl != std::string::npos && cl < hdrs) {
            auto s = req.find_first_of("0123456789", cl + 15);
            if (s != std::string::npos && s < hdrs)
                need = hdrs + 4 + std::strtoull(req.c_str() + s, nullptr, 10);
        }
    }

    auto sp1 = req.find(' ');
    auto sp2 = req.find(' ', sp1 + 1);
    if (sp1 == std::string::npos || sp2 == std::string::npos) {
        respond(fd, 400, "{\"error\":\"bad request\"}");
        close(fd);
        return;
    }
    std::string method = req.substr(0, sp1);
    std::string target = req.substr(sp1 + 1, sp2 - sp1 - 1);
    std::string path = target, query;
    auto q = target.find('?');
    if (q != std::string::npos) { path = target.substr(0, q); query = target.substr(q + 1); }
    auto hdrEnd = req.find("\r\n\r\n");
    std::string body = hdrEnd == std::string::npos ? "" : req.substr(hdrEnd + 4);

    if (path == "/health") {
        respond(fd, 200, "{\"ok\":true}");
        close(fd);
        return;
    }

    // Everything else is behind the control-plane bearer (constant-time).
    std::string auth;
    auto a = req.find("Authorization: Bearer ");
    if (a != std::string::npos && a < hdrEnd) {
        auto e = req.find("\r\n", a + 22);
        if (e != std::string::npos) auth = req.substr(a + 22, e - a - 22);
    }
    if (!constantTimeEq(auth, gSecret)) {
        respond(fd, 401, "{\"error\":\"unauthorized\"}");
        close(fd);
        return;
    }

    if (method == "POST" && path == "/incr") {
        std::string kind = jsonString(body, "kind");
        std::string key = jsonString(body, "key");
        long long delta = jsonNumber(body, "delta", 0);
        if (key.empty() || delta == 0 || delta < -1e9 || delta > 1e9 ||
            (kind != "hashtag" && kind != "view")) {
            respond(fd, 400, "{\"error\":\"kind must be hashtag|view with non-empty key and non-zero delta\"}");
        } else {
            if (kind == "hashtag") incrHashtag(key, delta); else incrView(key, delta);
            respond(fd, 200, "{\"ok\":true}");
        }
    } else if (method == "POST" && path == "/viewer") {
        std::string room = jsonString(body, "room");
        std::string op = jsonString(body, "op");
        if (room.empty() || (op != "join" && op != "leave")) {
            respond(fd, 400, "{\"error\":\"op must be join|leave with non-empty room\"}");
        } else {
            viewerOp(room, op == "join");
            std::lock_guard<std::mutex> lk(gMu);
            auto it = gRooms.find(room);
            long long viewers = it == gRooms.end() ? 0 : it->second.viewers;
            long long peak = it == gRooms.end() ? 0 : it->second.peak;
            respond(fd, 200, "{\"room\":\"" + jsonEscape(room) + "\",\"viewers\":" +
                std::to_string(viewers) + ",\"peak\":" + std::to_string(peak) + "}");
        }
    } else if (method == "GET" && path == "/top/hashtags") {
        int limit = 20;
        auto lp = query.find("limit=");
        if (lp != std::string::npos) limit = std::min(100, std::max(1, atoi(query.c_str() + lp + 6)));
        std::vector<std::pair<std::string, long long>> ranked;
        {
            std::lock_guard<std::mutex> lk(gMu);
            long long now = epochHour();
            ranked.reserve(gHashtags.size());
            for (auto& [tag, h] : gHashtags) {
                long long total = 0;
                for (int i = 0; i < WINDOW; i++) {
                    if (now - h.seenHour[i] < WINDOW) total += h.buckets[i];
                }
                if (total > 0) ranked.emplace_back(tag, total);
            }
        }
        std::partial_sort(ranked.begin(),
                          ranked.begin() + std::min<size_t>(limit, ranked.size()),
                          ranked.end(),
                          [](const auto& a, const auto& b) { return a.second > b.second; });
        size_t take = std::min<size_t>(limit, ranked.size());
        std::sort(ranked.begin(), ranked.begin() + take,
                  [](const auto& a, const auto& b) { return a.second > b.second; });
        std::string out = "{\"trending\":[";
        for (size_t i = 0; i < take; i++) {
            if (i) out += ',';
            out += "{\"tag\":\"" + jsonEscape(ranked[i].first) + "\",\"count\":" +
                   std::to_string(ranked[i].second) + "}";
        }
        out += "]}";
        respond(fd, 200, out);
    } else if (method == "GET" && path == "/viewers") {
        std::string room;
        auto rp = query.find("room=");
        if (rp != std::string::npos) {
            auto e = query.find('&', rp);
            room = query.substr(rp + 5, e == std::string::npos ? std::string::npos : e - rp - 5);
        }
        if (room.empty()) { respond(fd, 400, "{\"error\":\"room required\"}"); close(fd); return; }
        std::lock_guard<std::mutex> lk(gMu);
        auto it = gRooms.find(room);
        long long viewers = it == gRooms.end() ? 0 : it->second.viewers;
        long long peak = it == gRooms.end() ? 0 : it->second.peak;
        respond(fd, 200, "{\"room\":\"" + jsonEscape(room) + "\",\"viewers\":" +
            std::to_string(viewers) + ",\"peak\":" + std::to_string(peak) + "}");
    } else if (method == "GET" && path == "/count") {
        std::string kind, key;
        auto kp = query.find("kind=");
        if (kp != std::string::npos) {
            auto e = query.find('&', kp);
            kind = query.substr(kp + 5, e == std::string::npos ? std::string::npos : e - kp - 5);
        }
        auto vp = query.find("key=");
        if (vp != std::string::npos) {
            auto e = query.find('&', vp);
            key = query.substr(vp + 4, e == std::string::npos ? std::string::npos : e - vp - 4);
        }
        long long delta = 0;
        {
            std::lock_guard<std::mutex> lk(gMu);
            if (kind == "view") {
                auto it = gViews.find(key);
                if (it != gViews.end()) delta = it->second;
            } else if (kind == "hashtag") {
                auto it = gHashtags.find(key);
                if (it != gHashtags.end()) delta = it->second.pending;
            }
        }
        respond(fd, 200, "{\"kind\":\"" + jsonEscape(kind) + "\",\"key\":\"" + jsonEscape(key) +
            "\",\"delta\":" + std::to_string(delta) + "}");
    } else {
        respond(fd, 404, "{\"error\":\"not found\"}");
    }
    close(fd);
}

int main() {
    int port = atoi(getenvOr("COUNTERS_PORT", "8600").c_str());
    gSecret = getenvOr("COUNTERS_SECRET", "");
    if (gSecret.empty()) {
        std::cerr << "FATAL: COUNTERS_SECRET is required (fail-closed)" << std::endl;
        return 1;
    }
    gFlushURL = getenvOr("FLUSH_URL", "");
    gFlushSecret = getenvOr("FLUSH_SECRET", getenvOr("CLUSTER_SECRET", "").c_str());
    int flushMs = atoi(getenvOr("FLUSH_INTERVAL_MS", "15000").c_str());
    if (flushMs < 1000) flushMs = 1000;

    int srv = socket(AF_INET6, SOCK_STREAM, 0);
    int one = 1;
    setsockopt(srv, SOL_SOCKET, SO_REUSEADDR, &one, sizeof one);
    int v6only = 0; // dual-stack: accept IPv4-mapped too
    setsockopt(srv, IPPROTO_IPV6, IPV6_V6ONLY, &v6only, sizeof v6only);
    sockaddr_in6 addr{};
    addr.sin6_family = AF_INET6;
    addr.sin6_port = htons(static_cast<uint16_t>(port));
    addr.sin6_addr = in6addr_any;
    if (bind(srv, reinterpret_cast<sockaddr*>(&addr), sizeof addr) != 0 ||
        listen(srv, 512) != 0) {
        std::cerr << "FATAL: cannot bind counters port " << port << std::endl;
        return 1;
    }

    std::thread flusher(flushLoop, flushMs);
    flusher.detach();

    std::cout << "counters on :" << port << " (flush " << flushMs << "ms -> "
              << (gFlushURL.empty() ? "disabled" : gFlushURL) << ")" << std::endl;
    for (;;) {
        int fd = accept(srv, nullptr, nullptr);
        if (fd < 0) continue;
        std::thread(handleClient, fd).detach();
    }
}
