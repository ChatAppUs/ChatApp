// ChatApp media edge service: ultra-low-latency upload & streaming.
// - POST /upload?filename=clip.mp4  (raw body) -> stores file, returns URL
// - GET  /media/{id}               -> streams with HTTP Range support (206)
// - GET  /health
// Optional signed-URL enforcement via the Rust security service when
// SECURITY_SERVICE_URL is set (e.g. http://security:8090).

#include <algorithm>
#include <cctype>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <fstream>
#include <iostream>
#include <map>
#include <random>
#include <sstream>
#include <string>
#include <thread>
#include <vector>

#include <arpa/inet.h>
#include <fcntl.h>
#include <netinet/in.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <unistd.h>

static std::string g_uploadDir = "/data/uploads";
static std::string g_securityURL; // empty = signature check disabled

static const std::map<std::string, std::string> kMime = {
    {"mp4", "video/mp4"},  {"webm", "video/webm"}, {"mov", "video/quicktime"},
    {"mp3", "audio/mpeg"}, {"aac", "audio/aac"},   {"ogg", "audio/ogg"},
    {"jpg", "image/jpeg"}, {"jpeg", "image/jpeg"}, {"png", "image/png"},
    {"gif", "image/gif"},  {"webp", "image/webp"},
};

static std::string mimeFor(const std::string& path) {
    auto dot = path.find_last_of('.');
    if (dot == std::string::npos) return "application/octet-stream";
    std::string ext = path.substr(dot + 1);
    std::transform(ext.begin(), ext.end(), ext.begin(), ::tolower);
    auto it = kMime.find(ext);
    return it != kMime.end() ? it->second : "application/octet-stream";
}

static std::string randomId() {
    static thread_local std::mt19937_64 rng{std::random_device{}()};
    std::uniform_int_distribution<uint64_t> dist;
    std::ostringstream oss;
    oss << std::hex << dist(rng) << dist(rng);
    return oss.str();
}

static bool safeName(const std::string& name) {
    if (name.empty() || name.size() > 128) return false;
    for (char c : name) {
        if (!(std::isalnum(c) || c == '.' || c == '-' || c == '_')) return false;
    }
    return name.find("..") == std::string::npos;
}

static void respond(int fd, int code, const std::string& status,
                    const std::string& body, const std::string& ctype = "application/json",
                    const std::string& extraHeaders = "") {
    std::ostringstream h;
    h << "HTTP/1.1 " << code << " " << status << "\r\n"
      << "Content-Type: " << ctype << "\r\n"
      << "Content-Length: " << body.size() << "\r\n"
      << "Access-Control-Allow-Origin: *\r\n"
      << extraHeaders << "Connection: close\r\n\r\n" << body;
    auto s = h.str();
    ::send(fd, s.data(), s.size(), MSG_NOSIGNAL);
}

// Minimal blocking HTTP client for security-service verification.
static std::string httpPost(const std::string& host, int port, const std::string& path,
                            const std::string& body) {
    int fd = ::socket(AF_INET, SOCK_STREAM, 0);
    if (fd < 0) return "";
    sockaddr_in addr{};
    addr.sin_family = AF_INET;
    addr.sin_port = htons(port);
    if (::inet_pton(AF_INET, host.c_str(), &addr.sin_addr) != 1) { ::close(fd); return ""; }
    if (::connect(fd, reinterpret_cast<sockaddr*>(&addr), sizeof(addr)) != 0) { ::close(fd); return ""; }
    std::ostringstream req;
    req << "POST " << path << " HTTP/1.1\r\nHost: " << host << "\r\n"
        << "Content-Type: application/json\r\nContent-Length: " << body.size()
        << "\r\nConnection: close\r\n\r\n" << body;
    auto s = req.str();
    if (::send(fd, s.data(), s.size(), MSG_NOSIGNAL) <= 0) { ::close(fd); return ""; }
    std::string resp;
    char buf[4096];
    ssize_t n;
    while ((n = ::recv(fd, buf, sizeof(buf), 0)) > 0) resp.append(buf, n);
    ::close(fd);
    return resp;
}

static bool verifySignature(const std::string& payload, const std::string& expires,
                            const std::string& sig) {
    if (g_securityURL.empty()) return true; // enforcement disabled
    // host:port from http://host:port
    std::string hp = g_securityURL.substr(g_securityURL.find("://") + 3);
    auto colon = hp.find(':');
    std::string host = colon == std::string::npos ? hp : hp.substr(0, colon);
    int port = colon == std::string::npos ? 80 : std::stoi(hp.substr(colon + 1));
    std::string body = "{\"payload\":\"" + payload + "\",\"expires\":\"" + expires +
                       "\",\"signature\":\"" + sig + "\"}";
    std::string resp = httpPost(host, port, "/verify", body);
    return resp.find("\"valid\":true") != std::string::npos;
}

static std::string queryParam(const std::string& target, const std::string& key) {
    auto q = target.find('?');
    if (q == std::string::npos) return "";
    std::string qs = target.substr(q + 1);
    std::istringstream ss(qs);
    std::string kv;
    while (std::getline(ss, kv, '&')) {
        auto eq = kv.find('=');
        if (eq != std::string::npos && kv.substr(0, eq) == key) return kv.substr(eq + 1);
    }
    return "";
}

static void handleClient(int fd) {
    std::string req;
    req.reserve(8192);
    char buf[8192];
    ssize_t n;
    // read headers
    while (req.find("\r\n\r\n") == std::string::npos && req.size() < 65536) {
        n = ::recv(fd, buf, sizeof(buf), 0);
        if (n <= 0) { ::close(fd); return; }
        req.append(buf, n);
    }
    auto headerEnd = req.find("\r\n\r\n");
    std::string headers = req.substr(0, headerEnd);
    std::string body = req.substr(headerEnd + 4);

    std::istringstream hs(headers);
    std::string requestLine;
    std::getline(hs, requestLine);
    std::istringstream rl(requestLine);
    std::string method, target;
    rl >> method >> target;

    std::map<std::string, std::string> hdr;
    std::string line;
    while (std::getline(hs, line) && line != "\r") {
        auto colon = line.find(':');
        if (colon != std::string::npos) {
            std::string k = line.substr(0, colon), v = line.substr(colon + 1);
            while (!v.empty() && (v.front() == ' ' || v.front() == '\t')) v.erase(v.begin());
            while (!v.empty() && (v.back() == '\r' || v.back() == ' ')) v.pop_back();
            std::transform(k.begin(), k.end(), k.begin(), ::tolower);
            hdr[k] = v;
        }
    }

    std::string path = target.substr(0, target.find('?'));

    if (method == "GET" && path == "/health") {
        respond(fd, 200, "OK", "{\"status\":\"ok\"}");
    } else if (method == "POST" && path == "/upload") {
        std::string filename = queryParam(target, "filename");
        if (!safeName(filename)) {
            respond(fd, 400, "Bad Request", "{\"error\":\"invalid filename\"}");
        } else {
            size_t contentLength = 0;
            if (hdr.count("content-length")) contentLength = std::stoull(hdr["content-length"]);
            if (contentLength == 0 || contentLength > (2ull << 30)) { // 2 GiB cap
                respond(fd, 400, "Bad Request", "{\"error\":\"bad content length\"}");
            } else {
                std::string id = randomId();
                std::string ext;
                auto dot = filename.find_last_of('.');
                if (dot != std::string::npos) ext = filename.substr(dot);
                std::string stored = id + ext;
                std::string full = g_uploadDir + "/" + stored;
                std::ofstream out(full, std::ios::binary | std::ios::trunc);
                if (!out) {
                    respond(fd, 500, "Internal Server Error", "{\"error\":\"storage unavailable\"}");
                } else {
                    out.write(body.data(), static_cast<std::streamsize>(body.size()));
                    size_t written = body.size();
                    while (written < contentLength) {
                        n = ::recv(fd, buf, std::min(sizeof(buf), contentLength - written), 0);
                        if (n <= 0) break;
                        out.write(buf, n);
                        written += n;
                    }
                    out.close();
                    if (written != contentLength) {
                        std::remove(full.c_str());
                        respond(fd, 400, "Bad Request", "{\"error\":\"incomplete upload\"}");
                    } else {
                        respond(fd, 201, "Created",
                                "{\"url\":\"/media/" + stored + "\",\"bytes\":" +
                                    std::to_string(written) + "}");
                    }
                }
            }
        }
    } else if (method == "GET" && path.rfind("/media/", 0) == 0) {
        std::string name = path.substr(7);
        if (!safeName(name)) {
            respond(fd, 400, "Bad Request", "{\"error\":\"invalid name\"}");
        } else {
            std::string sig = queryParam(target, "sig");
            std::string exp = queryParam(target, "exp");
            if (!g_securityURL.empty() && !verifySignature("/media/" + name, exp, sig)) {
                respond(fd, 403, "Forbidden", "{\"error\":\"invalid or expired signature\"}");
            } else {
                std::string full = g_uploadDir + "/" + name;
                int fileFd = ::open(full.c_str(), O_RDONLY);
                if (fileFd < 0) {
                    respond(fd, 404, "Not Found", "{\"error\":\"not found\"}");
                } else {
                    struct stat st{};
                    ::fstat(fileFd, &st);
                    off_t size = st.st_size;
                    off_t start = 0, end = size - 1;
                    bool partial = false;
                    if (hdr.count("range")) {
                        std::string r = hdr["range"]; // bytes=start-end
                        if (r.rfind("bytes=", 0) == 0) {
                            auto dash = r.find('-', 6);
                            std::string s0 = r.substr(6, dash - 6), s1 = r.substr(dash + 1);
                            if (!s0.empty()) start = std::stoll(s0);
                            if (!s1.empty()) end = std::min<off_t>(std::stoll(s1), size - 1);
                            if (start >= 0 && start <= end && start < size) partial = true;
                        }
                    }
                    off_t len = end - start + 1;
                    std::ostringstream h;
                    h << "HTTP/1.1 " << (partial ? 206 : 200) << " "
                      << (partial ? "Partial Content" : "OK") << "\r\n"
                      << "Content-Type: " << mimeFor(name) << "\r\n"
                      << "Accept-Ranges: bytes\r\n"
                      << "Access-Control-Allow-Origin: *\r\n"
                      << "Content-Length: " << len << "\r\n";
                    if (partial) h << "Content-Range: bytes " << start << "-" << end << "/" << size << "\r\n";
                    h << "Connection: close\r\n\r\n";
                    auto hs2 = h.str();
                    ::send(fd, hs2.data(), hs2.size(), MSG_NOSIGNAL);
                    ::lseek(fileFd, start, SEEK_SET);
                    off_t remaining = len;
                    while (remaining > 0) {
                        n = ::read(fileFd, buf, std::min<off_t>(sizeof(buf), remaining));
                        if (n <= 0) break;
                        ::send(fd, buf, n, MSG_NOSIGNAL);
                        remaining -= n;
                    }
                    ::close(fileFd);
                }
            }
        }
    } else if (method == "OPTIONS") {
        respond(fd, 204, "No Content", "", "text/plain",
                "Access-Control-Allow-Methods: GET,POST,OPTIONS\r\nAccess-Control-Allow-Headers: Content-Type\r\n");
    } else {
        respond(fd, 404, "Not Found", "{\"error\":\"not found\"}");
    }
    ::shutdown(fd, SHUT_RDWR);
    ::close(fd);
}

int main() {
    if (const char* d = std::getenv("UPLOAD_DIR")) g_uploadDir = d;
    if (const char* s = std::getenv("SECURITY_SERVICE_URL")) g_securityURL = s;
    ::mkdir(g_uploadDir.c_str(), 0755);

    int port = 8100;
    if (const char* p = std::getenv("MEDIA_PORT")) port = std::stoi(p);

    int srv = ::socket(AF_INET, SOCK_STREAM, 0);
    int opt = 1;
    ::setsockopt(srv, SOL_SOCKET, SO_REUSEADDR, &opt, sizeof(opt));
    sockaddr_in addr{};
    addr.sin_family = AF_INET;
    addr.sin_addr.s_addr = INADDR_ANY;
    addr.sin_port = htons(port);
    if (::bind(srv, reinterpret_cast<sockaddr*>(&addr), sizeof(addr)) != 0 || ::listen(srv, 256) != 0) {
        std::cerr << "bind/listen failed on port " << port << "\n";
        return 1;
    }
    std::cout << "chatapp-media listening on :" << port
              << (g_securityURL.empty() ? " (signature check disabled)" : " (signed URLs enforced)")
              << "\n";
    for (;;) {
        int fd = ::accept(srv, nullptr, nullptr);
        if (fd < 0) continue;
        std::thread(handleClient, fd).detach();
    }
}
