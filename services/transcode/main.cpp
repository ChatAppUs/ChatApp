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

// ---- richer JSON extraction for job params ----

// Extract a raw JSON object value for a top-level key (brace-aware).
static std::string jsonObject(const std::string& js, const std::string& key) {
    std::string pat = "\"" + key + "\"";
    auto k = js.find(pat);
    if (k == std::string::npos) return "";
    auto colon = js.find(':', k + pat.size());
    if (colon == std::string::npos) return "";
    auto open = js.find('{', colon + 1);
    if (open == std::string::npos) return "";
    int depth = 0; bool inStr = false, esc = false;
    for (size_t i = open; i < js.size(); i++) {
        char c = js[i];
        if (esc) { esc = false; continue; }
        if (c == '\\') { esc = true; continue; }
        if (c == '"') inStr = !inStr;
        if (inStr) continue;
        if (c == '{') depth++;
        if (c == '}') { depth--; if (depth == 0) return js.substr(open, i - open + 1); }
    }
    return "";
}

// Extract string values of a string-array field from a JSON object.
static std::vector<std::string> jsonStringArray(const std::string& js, const std::string& key) {
    std::vector<std::string> out;
    std::string pat = "\"" + key + "\"";
    auto k = js.find(pat);
    if (k == std::string::npos) return out;
    auto open = js.find('[', k + pat.size());
    if (open == std::string::npos) return out;
    auto end = js.find(']', open + 1);
    if (end == std::string::npos) return out;
    std::string arr = js.substr(open + 1, end - open - 1);
    size_t pos = 0;
    for (;;) {
        auto q1 = arr.find('"', pos);
        if (q1 == std::string::npos) break;
        auto q2 = arr.find('"', q1 + 1);
        if (q2 == std::string::npos) break;
        out.push_back(arr.substr(q1 + 1, q2 - q1 - 1));
        pos = q2 + 1;
    }
    return out;
}

static double jsonNumber(const std::string& js, const std::string& key, double def) {
    std::string pat = "\"" + key + "\"";
    auto k = js.find(pat);
    if (k == std::string::npos) return def;
    auto colon = js.find(':', k + pat.size());
    if (colon == std::string::npos) return def;
    size_t i = colon + 1;
    while (i < js.size() && js[i] == ' ') i++;
    if (i >= js.size()) return def;
    return atof(js.c_str() + i);
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

struct LadderEntry {
    std::string name;
    int width, height, vkbps, akbps;
};

// ---- compositor jobs: duet (side-by-side), stitch (concat), trim, mix ----
//
// All produce a single 720p HLS rendition + master playlist + thumbnail,
// matching the VOD layout the API reports back (master flagged entry).

static bool runCompositor(const std::string& ffmpeg, const std::string& kind,
                          const std::string& claimBody, const std::string& outDir,
                          const std::string& mediaID, std::ostringstream& ladder,
                          std::string& errMsg) {
    std::string params = jsonObject(claimBody, "params");
    std::vector<std::string> sources = jsonStringArray(params, "sources");
    std::string source = jsonString(params, "source_url");
    std::string out = outDir + "/720p.m3u8";
    std::vector<std::string> args = {ffmpeg, "-y"};

    if (kind == "duet") {
        if (sources.size() < 2) { errMsg = "duet needs 2 sources"; return false; }
        args.insert(args.end(), {"-i", sources[0], "-i", sources[1],
            "-filter_complex",
            "[0:v]scale=640:1280:force_original_aspect_ratio=increase,crop=640:1280[l];"
            "[1:v]scale=640:1280:force_original_aspect_ratio=increase,crop=640:1280[r];"
            "[l][r]hstack=inputs=2[v];[0:a][1:a]amerge=inputs=2[a]",
            "-map", "[v]", "-map", "[a]"});
    } else if (kind == "stitch") {
        if (sources.size() < 2) { errMsg = "stitch needs 2+ sources"; return false; }
        if (sources.size() > 4) sources.resize(4);
        for (auto& s : sources) args.insert(args.end(), {"-i", s});
        std::ostringstream fc;
        for (size_t i = 0; i < sources.size(); i++) fc << "[" << i << ":v][" << i << ":a]";
        fc << "concat=n=" << sources.size() << ":v=1:a=1[v][a]";
        args.insert(args.end(), {"-filter_complex", fc.str(), "-map", "[v]", "-map", "[a]"});
    } else if (kind == "trim") {
        if (source.empty()) { errMsg = "trim needs source_url"; return false; }
        double ss = jsonNumber(params, "start_s", 0);
        double dur = jsonNumber(params, "duration_s", 0);
        if (dur <= 0) { errMsg = "duration_s required"; return false; }
        std::ostringstream secs;
        args.insert(args.end(), {"-ss", std::to_string(ss), "-t", std::to_string(dur), "-i", source});
    } else { // mix: sources[0]=video, sources[1]=voiceover/sound
        if (sources.size() < 2) { errMsg = "mix needs video+audio sources"; return false; }
        args.insert(args.end(), {"-i", sources[0], "-i", sources[1],
            "-filter_complex", "[0:a]volume=1.0[a0];[1:a]volume=1.6[a1];[a0][a1]amix=inputs=2:duration=first[a]",
            "-map", "0:v", "-map", "[a]"});
    }
    args.insert(args.end(), {"-c:v", "libx264", "-preset", "veryfast", "-crf", "23",
        "-maxrate", "2500k", "-bufsize", "5000k",
        "-c:a", "aac", "-b:a", "128k",
        "-hls_time", "4", "-hls_playlist_type", "vod",
        "-hls_segment_filename", outDir + "/720p_%05d.ts", out});
    if (runCmd(args) != 0) { errMsg = kind + " ffmpeg failed"; return false; }
    const std::string thumbSrc = !source.empty() ? source : (!sources.empty() ? sources[0] : "");
    if (!thumbSrc.empty()) {
        runCmd({ffmpeg, "-y", "-ss", "1", "-i", thumbSrc,
                "-frames:v", "1", "-q:v", "3", outDir + "/thumb.jpg"});
    }
    std::ofstream master(outDir + "/master.m3u8");
    master << "#EXTM3U\n#EXT-X-VERSION:3\n"
           << "#EXT-X-STREAM-INF:BANDWIDTH=2628000,RESOLUTION=1280x720\n720p.m3u8\n";
    master.close();
    ladder << "{\"name\":\"720p\",\"width\":1280,\"height\":720,\"url\":\""
           << jsonEscape("/hls/" + mediaID + "/720p.m3u8") << "\"},"
           << "{\"name\":\"master\",\"master\":true,\"url\":\""
           << jsonEscape("/hls/" + mediaID + "/master.m3u8") << "\"}";
    return true;
}

// ---- live ingest: the worker IS the RTMP endpoint (ffmpeg -listen) ----
//
// Publishers (OBS/mobile) push to rtmp://<worker-host>:$RTMP_LISTEN_PORT/live/<key>;
// the stream key minted by the API gates ingest. Segments roll into a
// persistent event playlist in the shared media volume, so viewership is
// served by the C++ media edge/CDN — unlimited viewers off the WebRTC path.

static bool runLiveIngest(const std::string& ffmpeg, const std::string& claimBody,
                          const std::string& mediaID, const std::string& outDir,
                          std::ostringstream& ladder, std::string& errMsg) {
    std::string params = jsonObject(claimBody, "params");
    std::string key = jsonString(params, "stream_key");
    if (key.empty()) { errMsg = "live needs a stream_key"; return false; }
    std::string port = getenvOr("RTMP_LISTEN_PORT", "1935");
    std::string url = "rtmp://0.0.0.0:" + port + "/live/" + key;
    std::string out = outDir + "/live.m3u8";
    int rc = runCmd({ffmpeg, "-y", "-listen", "1", "-i", url,
                     "-c:v", "libx264", "-preset", "veryfast", "-crf", "23",
                     "-c:a", "aac", "-b:a", "128k",
                     "-hls_time", "4", "-hls_playlist_type", "event",
                     "-hls_segment_filename", outDir + "/live_%05d.ts", out});
    if (rc != 0) { errMsg = "live ingest ended with error"; return false; }
    // play_url from the API is master.m3u8 — emit it as a single-variant
    // master pointing at the event playlist.
    std::ofstream master(outDir + "/master.m3u8");
    master << "#EXTM3U\n#EXT-X-VERSION:3\n"
           << "#EXT-X-STREAM-INF:BANDWIDTH=2628000\nlive.m3u8\n";
    master.close();
    ladder << "{\"name\":\"live\",\"url\":\"" << jsonEscape("/hls/" + mediaID + "/live.m3u8")
           << "\"},{\"name\":\"master\",\"master\":true,\"url\":\""
           << jsonEscape("/hls/" + mediaID + "/master.m3u8") << "\"}";
    return true;
}

// ABR ladder tuned for reels/stories: 1080p down to 360p.
static const LadderEntry kLadder[] = {
    {"1080p", 1920, 1080, 4500, 160},
    {"720p",  1280, 720,  2500, 128},
    {"480p",  854,  480,  1200, 96},
    {"360p",  640,  360,  700,  64},
};


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

        if (kind == "duet" || kind == "stitch" || kind == "trim" || kind == "mix") {
            ok = runCompositor(ffmpeg, kind, claim.body, outDir, mediaID, ladder, errMsg);
        } else if (kind == "live") {
            ok = runLiveIngest(ffmpeg, claim.body, mediaID, outDir, ladder, errMsg);
        } else if (kind == "audio") {
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
