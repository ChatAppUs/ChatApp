// ChatApp transcode worker — C++17, zero external libraries.
//
// Claims jobs from the Go API's internal control plane (shared-secret
// bearer), renders an HLS adaptive-bitrate ladder + thumbnail with ffmpeg,
// writes the renditions into the media edge's volume, and reports the
// ladder back. Multi-worker safe: the API claims jobs FOR UPDATE SKIP
// LOCKED, and stale claims are requeued by the API scheduler.
//
// Env:
//   API_INTERNAL_URL   default http://localhost:8080
//   CLUSTER_SECRET     shared internal bearer (required)
//   MEDIA_DIR          output volume shared with the media edge (required)
//   FFMPEG_BIN         default ffmpeg
//   POLL_MS            idle poll interval, default 2000

#include <arpa/inet.h>
#include <netdb.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>

#include <cerrno>
#include <chrono>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <iostream>
#include <sstream>
#include <string>
#include <thread>
#include <vector>

static std::string getenvOr(const char* k, const char* def) {
    const char* v = getenv(k);
    return (v && *v) ? v : def;
}

// ---- minimal HTTP client (raw sockets, DNS via getaddrinfo) ----

struct HttpResponse {
    int status = 0;
    std::string body;
};

static bool parseUrl(const std::string& url, std::string& host, int& port, std::string& path) {
    std::string rest = url;
    if (rest.rfind("http://", 0) == 0) {
        rest = rest.substr(7);
        port = 80;
    } else if (rest.rfind("https://", 0) == 0) {
        return false; // internal plane is plain HTTP on the private mesh
    } else {
        return false;
    }
    auto slash = rest.find('/');
    std::string authority = slash == std::string::npos ? rest : rest.substr(0, slash);
    path = slash == std::string::npos ? "/" : rest.substr(slash);
    auto colon = authority.rfind(':');
    if (colon != std::string::npos) {
        host = authority.substr(0, colon);
        port = atoi(authority.substr(colon + 1).c_str());
    } else {
        host = authority;
    }
    return !host.empty() && port > 0;
}

static int connectTo(const std::string& host, int port) {
    struct addrinfo hints{};
    hints.ai_family = AF_UNSPEC;
    hints.ai_socktype = SOCK_STREAM;
    struct addrinfo* res = nullptr;
    char portStr[16];
    snprintf(portStr, sizeof(portStr), "%d", port);
    if (getaddrinfo(host.c_str(), portStr, &hints, &res) != 0) return -1;
    int fd = -1;
    for (auto* rp = res; rp; rp = rp->ai_next) {
        fd = socket(rp->ai_family, rp->ai_socktype, rp->ai_protocol);
        if (fd < 0) continue;
        if (connect(fd, rp->ai_addr, rp->ai_addrlen) == 0) break;
        close(fd);
        fd = -1;
    }
    freeaddrinfo(res);
    return fd;
}

static HttpResponse httpPost(const std::string& url, const std::string& bearer,
                             const std::string& body) {
    HttpResponse out;
    std::string host, path;
    int port = 0;
    if (!parseUrl(url, host, port, path)) return out;
    int fd = connectTo(host, port);
    if (fd < 0) return out;

    std::ostringstream req;
    req << "POST " << path << " HTTP/1.1\r\n"
        << "Host: " << host << "\r\n"
        << "Authorization: Bearer " << bearer << "\r\n"
        << "Content-Type: application/json\r\n"
        << "Content-Length: " << body.size() << "\r\n"
        << "Connection: close\r\n\r\n"
        << body;
    std::string raw = req.str();
    size_t sent = 0;
    while (sent < raw.size()) {
        ssize_t n = send(fd, raw.data() + sent, raw.size() - sent, 0);
        if (n <= 0) { close(fd); return out; }
        sent += (size_t)n;
    }
    std::string resp;
    char buf[8192];
    for (;;) {
        ssize_t n = recv(fd, buf, sizeof(buf), 0);
        if (n <= 0) break;
        resp.append(buf, (size_t)n);
        if (resp.size() > (1u << 22)) break; // 4 MiB cap
    }
    close(fd);
    auto sp = resp.find(' ');
    if (sp != std::string::npos) out.status = atoi(resp.c_str() + sp + 1);
    auto hdrEnd = resp.find("\r\n\r\n");
    if (hdrEnd != std::string::npos) out.body = resp.substr(hdrEnd + 4);
    return out;
}

// ---- targeted JSON field extraction (API response shape is fixed) ----

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
        if (c == '\\' && i + 1 < js.size()) { val += js[i + 1]; i++; continue; }
        if (c == '"') break;
        val += c;
    }
    return val;
}

// ---- ffmpeg execution ----

static int runCmd(const std::vector<std::string>& args) {
    std::string cmd;
    for (const auto& a : args) {
        std::string esc = "'";
        for (char c : a) { if (c == '\'') esc += "'\\''"; else esc += c; }
        esc += "'";
        cmd += esc + " ";
    }
    cmd += ">/dev/null 2>&1";
    int rc = system(cmd.c_str());
    if (rc == -1) return -1;
    if (WIFEXITED(rc)) return WEXITSTATUS(rc);
    return -1;
}

struct LadderEntry {
    std::string name;
    int width, height, vkbps, akbps;
};

// ABR ladder tuned for reels/stories: 1080p down to 360p.
static const LadderEntry kLadder[] = {
    {"1080p", 1920, 1080, 4500, 160},
    {"720p",  1280, 720,  2500, 128},
    {"480p",  854,  480,  1200, 96},
    {"360p",  640,  360,  700,  64},
};

static std::string jsonEscape(const std::string& s) {
    std::string out;
    for (char c : s) {
        switch (c) {
            case '"': out += "\\\""; break;
            case '\\': out += "\\\\"; break;
            case '\n': out += "\\n"; break;
            case '\r': out += "\\r"; break;
            case '\t': out += "\\t"; break;
            default: out += c;
        }
    }
    return out;
}

int main() {
    const std::string apiURL = getenvOr("API_INTERNAL_URL", "http://localhost:8080");
    const std::string secret = getenvOr("CLUSTER_SECRET", "");
    const std::string mediaDir = getenvOr("MEDIA_DIR", "");
    const std::string ffmpeg = getenvOr("FFMPEG_BIN", "ffmpeg");
    const int pollMs = atoi(getenvOr("POLL_MS", "2000").c_str());

    if (secret.empty()) {
        std::cerr << "FATAL: CLUSTER_SECRET is required (internal control plane auth)\n";
        return 1;
    }
    if (mediaDir.empty()) {
        std::cerr << "FATAL: MEDIA_DIR is required (shared volume with the media edge)\n";
        return 1;
    }
    if (runCmd({ffmpeg, "-version"}) != 0) {
        std::cerr << "FATAL: ffmpeg not available at " << ffmpeg << "\n";
        return 1;
    }
    std::cout << "transcode worker online, polling " << apiURL << "\n";

    for (;;) {
        HttpResponse claim = httpPost(apiURL + "/internal/transcode/claim", secret, "{}");
        if (claim.status != 200) {
            std::this_thread::sleep_for(std::chrono::milliseconds(pollMs));
            continue;
        }
        std::string jobID = jsonString(claim.body, "id");
        std::string mediaID = jsonString(claim.body, "media_id");
        std::string sourceURL = jsonString(claim.body, "source_url");
        std::string kind = jsonString(claim.body, "kind");
        if (jobID.empty()) { // no job queued
            std::this_thread::sleep_for(std::chrono::milliseconds(pollMs));
            continue;
        }
        std::cout << "claimed job " << jobID << " media " << mediaID << "\n";

        std::string outDir = mediaDir + "/hls/" + mediaID;
        std::string mkrc = "mkdir -p '" + outDir + "'";
        if (system(mkrc.c_str()) != 0) {
            httpPost(apiURL + "/internal/transcode/complete", secret,
                     "{\"job_id\":\"" + jobID + "\",\"media_id\":\"" + mediaID +
                     "\",\"status\":\"failed\",\"error\":\"output dir unavailable\"}");
            continue;
        }

        bool ok = true;
        std::string errMsg;
        std::ostringstream ladder;
        ladder << "[";
        bool first = true;

        if (kind == "audio") {
            // Audio: normalize loudness, single 128k AAC HLS rendition.
            std::string out = outDir + "/audio.m3u8";
            if (runCmd({ffmpeg, "-y", "-i", sourceURL, "-vn",
                        "-af", "loudnorm=I=-16:TP=-1.5:LRA=11",
                        "-c:a", "aac", "-b:a", "128k",
                        "-hls_time", "4", "-hls_playlist_type", "vod", out}) != 0) {
                ok = false; errMsg = "audio transcode failed";
            } else {
                ladder << "{\"name\":\"audio\",\"url\":\"" << jsonEscape("/hls/" + mediaID + "/audio.m3u8") << "\"}";
                first = false;
            }
        } else {
            for (const auto& lv : kLadder) {
                std::string out = outDir + "/" + lv.name + ".m3u8";
                std::ostringstream scale;
                scale << "scale=w=" << lv.width << ":h=" << lv.height
                      << ":force_original_aspect_ratio=decrease";
                if (runCmd({ffmpeg, "-y", "-i", sourceURL,
                            "-vf", scale.str(),
                            "-c:v", "libx264", "-preset", "veryfast", "-crf", "23",
                            "-maxrate", std::to_string(lv.vkbps) + "k",
                            "-bufsize", std::to_string(lv.vkbps * 2) + "k",
                            "-c:a", "aac", "-b:a", std::to_string(lv.akbps) + "k",
                            "-hls_time", "4", "-hls_playlist_type", "vod",
                            "-hls_segment_filename", outDir + "/" + lv.name + "_%05d.ts",
                            out}) != 0) {
                    ok = false; errMsg = "rendition " + lv.name + " failed";
                    break;
                }
                if (!first) ladder << ",";
                ladder << "{\"name\":\"" << lv.name << "\",\"width\":" << lv.width
                       << ",\"height\":" << lv.height
                       << ",\"url\":\"" << jsonEscape("/hls/" + mediaID + "/" + lv.name + ".m3u8") << "\"}";
                first = false;
            }
            // Thumbnail from the 1-second mark (best-effort).
            if (ok) {
                runCmd({ffmpeg, "-y", "-ss", "1", "-i", sourceURL,
                        "-frames:v", "1", "-q:v", "3", outDir + "/thumb.jpg"});
            }
            // Master playlist referencing every rendition.
            if (ok) {
                std::ofstream master(outDir + "/master.m3u8");
                master << "#EXTM3U\n#EXT-X-VERSION:3\n";
                for (const auto& lv : kLadder) {
                    master << "#EXT-X-STREAM-INF:BANDWIDTH=" << (lv.vkbps + lv.akbps) * 1000
                           << ",RESOLUTION=" << lv.width << "x" << lv.height << "\n"
                           << lv.name << ".m3u8\n";
                }
                master.close();
                ladder << (first ? "" : ",")
                       << "{\"name\":\"master\",\"master\":true,\"url\":\""
                       << jsonEscape("/hls/" + mediaID + "/master.m3u8") << "\"}";
            }
        }
        ladder << "]";

        std::ostringstream done;
        done << "{\"job_id\":\"" << jobID << "\",\"media_id\":\"" << mediaID << "\","
             << "\"status\":\"" << (ok ? "done" : "failed") << "\","
             << "\"ladder\":" << (ok ? ladder.str() : "[]") << ","
             << "\"thumb_url\":\"" << (ok ? jsonEscape("/hls/" + mediaID + "/thumb.jpg") : "") << "\","
             << "\"error\":\"" << jsonEscape(errMsg) << "\"}";
        HttpResponse fin = httpPost(apiURL + "/internal/transcode/complete", secret, done.str());
        std::cout << "job " << jobID << " -> " << (ok ? "done" : "failed")
                  << " (api " << fin.status << ")\n";
    }
}
