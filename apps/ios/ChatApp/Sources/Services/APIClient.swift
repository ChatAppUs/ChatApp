import Foundation

enum APIError: Error, LocalizedError {
    case http(Int, String)
    case transport(Error)

    var errorDescription: String? {
        switch self {
        case .http(let code, let body): return "HTTP \(code): \(body)"
        case .transport(let err): return err.localizedDescription
        }
    }
}

struct APIClient {
    static let baseURL: String = {
        Bundle.main.object(forInfoDictionaryKey: "CHATAPP_API_BASE") as? String
            ?? ProcessInfo.processInfo.environment["CHATAPP_API_BASE"]
            ?? "http://localhost:8080"
    }()

    static let wsBaseURL: String = {
        Bundle.main.object(forInfoDictionaryKey: "CHATAPP_WS_BASE") as? String
            ?? ProcessInfo.processInfo.environment["CHATAPP_WS_BASE"]
            ?? "ws://localhost:8080"
    }()

    static let mediaBaseURL: String = {
        Bundle.main.object(forInfoDictionaryKey: "CHATAPP_MEDIA_BASE") as? String
            ?? ProcessInfo.processInfo.environment["CHATAPP_MEDIA_BASE"]
            ?? "http://localhost:8100"
    }()

    let token: String?

    init(token: String? = nil) {
        self.token = token
    }

    // Signed-grant media upload matching the web/Android flow: fetch a
    // short-lived upload token from the Go API, then POST the raw bytes to
    // the C++ media edge. Returns the absolute media URL.
    func uploadMedia(filename: String, data: Data) async throws -> String {
        var grant = ""
        if let t = try? await post("/api/media/upload-token"),
           let obj = try JSONSerialization.jsonObject(with: t) as? [String: Any],
           let exp = obj["expires"] as? Int, let sig = obj["signature"] as? String {
            let allowed = CharacterSet.urlQueryAllowed
            grant = "&exp=\(exp)&sig=\(sig.addingPercentEncoding(withAllowedCharacters: allowed) ?? sig)"
        }
        let name = filename.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed) ?? filename
        var req = URLRequest(url: URL(string: "\(Self.mediaBaseURL)/upload?filename=\(name)\(grant)")!)
        req.httpMethod = "POST"
        req.setValue("application/octet-stream", forHTTPHeaderField: "Content-Type")
        req.httpBody = data
        let (respData, resp) = try await URLSession.shared.data(for: req)
        guard (200..<300).contains((resp as? HTTPURLResponse)?.statusCode ?? -1),
              let obj = try JSONSerialization.jsonObject(with: respData) as? [String: Any],
              let rel = obj["url"] as? String else {
            throw APIError.http((resp as? HTTPURLResponse)?.statusCode ?? -1, "upload failed")
        }
        return Self.mediaBaseURL + rel
    }

    func get(_ path: String) async throws -> Data {
        try await request(path, method: "GET")
    }

    func post(_ path: String, body: [String: Any] = [:]) async throws -> Data {
        try await request(path, method: "POST", body: body)
    }

    func put(_ path: String, body: [String: Any] = [:]) async throws -> Data {
        try await request(path, method: "PUT", body: body)
    }

    func delete(_ path: String) async throws -> Data {
        try await request(path, method: "DELETE")
    }

    func delete(_ path: String, body: [String: Any]) async throws -> Data {
        try await request(path, method: "DELETE", body: body)
    }

    private func request(_ path: String, method: String, body: [String: Any]? = nil) async throws -> Data {
        var req = URLRequest(url: URL(string: Self.baseURL + path)!)
        req.httpMethod = method
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        if let token { req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        if let body { req.httpBody = try JSONSerialization.data(withJSONObject: body) }
        do {
            let (data, resp) = try await URLSession.shared.data(for: req)
            let status = (resp as? HTTPURLResponse)?.statusCode ?? -1
            guard (200..<300).contains(status) else {
                throw APIError.http(status, String(data: data, encoding: .utf8) ?? "")
            }
            return data
        } catch let error as APIError {
            throw error
        } catch {
            throw APIError.transport(error)
        }
    }
}
