import Foundation
import Security

/// Tokens live in the iOS Keychain (kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly),
/// never in UserDefaults or files.
final class SessionStore: ObservableObject {
    @Published var accessToken: String? {
        didSet { Self.save(key: "access", value: accessToken) }
    }
    @Published var refreshToken: String? {
        didSet { Self.save(key: "refresh", value: refreshToken) }
    }
    @Published var userId: String? {
        didSet { Self.save(key: "uid", value: userId) }
    }

    init() {
        accessToken = Self.read(key: "access")
        refreshToken = Self.read(key: "refresh")
        userId = Self.read(key: "uid")
    }

    func logout() {
        accessToken = nil
        refreshToken = nil
        userId = nil
    }

    private static let service = "com.chatapp.ios.session"

    private static func save(key: String, value: String?) {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: key,
        ]
        SecItemDelete(query as CFDictionary)
        guard let value, let data = value.data(using: .utf8) else { return }
        var attrs = query
        attrs[kSecValueData as String] = data
        attrs[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        SecItemAdd(attrs as CFDictionary, nil)
    }

    private static func read(key: String) -> String? {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: key,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
        ]
        var item: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &item) == errSecSuccess,
              let data = item as? Data else { return nil }
        return String(data: data, encoding: .utf8)
    }
}
