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

    let token: String?

    init(token: String? = nil) {
        self.token = token
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
