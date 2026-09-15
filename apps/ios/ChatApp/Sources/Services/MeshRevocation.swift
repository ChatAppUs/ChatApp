import Foundation
import CryptoKit

// MeshRevocation.swift — network-wide distribution of revocation notices.
//
// Mirrors services/mesh/revocdist.go and the Android MeshRevocation.kt. When a
// device's identity key must be distrusted, the revoking node signs a notice
// and floods it through the mesh; every node stores one notice per device,
// re-broadcasts it (bounded), pins the revoked key, and tears down sessions
// and routes to it.

struct MeshRevocationNotice {
    let revoker: String
    let deviceId: String
    let publicKey: Data
    let issuedAt: Int64
    let signature: Data

    /// The canonical, signature-covered projection (must byte-match the Go
    /// and Android encodings: compact JSON with fixed key order).
    func payload() -> Data {
        let j = "{\"revoker\":\"\(jsonEscape(revoker))\",\"device_id\":\"\(jsonEscape(deviceId))\","
            + "\"public_key\":\"\(MeshCrypto.b64(publicKey))\",\"issued_at\":\(issuedAt)}"
        return Data(j.utf8)
    }

    func marshal() -> Data {
        let j = payload()
        var o = try! JSONSerialization.jsonObject(with: j) as! [String: Any]
        o["sig"] = MeshCrypto.b64(signature)
        return try! JSONSerialization.data(withJSONObject: o)
    }

    static func unmarshal(_ wire: Data) -> MeshRevocationNotice? {
        guard let o = try? JSONSerialization.jsonObject(with: wire) as? [String: Any],
              let revoker = o["revoker"] as? String, !revoker.isEmpty,
              let deviceId = o["device_id"] as? String, !deviceId.isEmpty,
              let pkB64 = o["public_key"] as? String,
              let publicKey = Data(base64Encoded: pkB64), publicKey.count == 32,
              let issuedAt = o["issued_at"] as? Int64,
              let sigB64 = o["sig"] as? String,
              let signature = Data(base64Encoded: sigB64), signature.count == 64
        else { return nil }
        return MeshRevocationNotice(revoker: revoker, deviceId: deviceId, publicKey: publicKey,
                                    issuedAt: issuedAt, signature: signature)
    }

    /// Signs and builds a notice, mirroring services/mesh SignRevocation.
    static func sign(revoker: String, deviceId: String, publicKey: Data,
                     signer: MeshIdentity) -> MeshRevocationNotice? {
        let draft = MeshRevocationNotice(revoker: revoker, deviceId: deviceId,
                                         publicKey: publicKey, issuedAt: Int64(Date().timeIntervalSince1970 * 1000),
                                         signature: Data())
        guard let sig = signer.signBytes(draft.payload()) else { return nil }
        return MeshRevocationNotice(revoker: revoker, deviceId: deviceId,
                                    publicKey: publicKey, issuedAt: draft.issuedAt, signature: sig)
    }

    /**
     * Verifies the notice against pinned identity keys (device id -> beacon
     * public key), mirroring services/mesh VerifyRevocation: the revoker must
     * be a known peer, the signature must authenticate, and if the revoked
     * device itself is pinned the named key must match the pinned identity.
     */
    func verify(known: [String: Data]) -> Bool {
        guard publicKey.count == 32, !signature.isEmpty else { return false }
        guard let revokerPub = known[revoker] else { return false }
        guard MeshCrypto.verifyEd25519(pub: revokerPub, data: payload(), sig: signature) else { return false }
        if let prev = known[deviceId], prev != publicKey { return false }
        return true
    }

    private func jsonEscape(_ s: String) -> String {
        var out = ""
        for c in s.unicodeScalars {
            switch c {
            case "\"": out += "\\\""
            case "\\": out += "\\\\"
            case "\n": out += "\\n"
            case "\r": out += "\\r"
            case "\t": out += "\\t"
            default:
                if c.value < 0x20 {
                    out += String(format: "\\u%04x", c.value)
                } else {
                    out.unicodeScalars.append(c)
                }
            }
        }
        return out
    }
}

/// Bounded store of revocation notices: one per device, flood-deduplicated.
final class RevocationStore {
    private let maxNotices: Int

    private let lock = NSLock()
    private var notices: [String: MeshRevocationNotice] = [:]

    init(maxNotices: Int = 4096) {
        self.maxNotices = maxNotices
    }

    /// Applies a verified notice; reports whether it is new for this device.
    @discardableResult
    func apply(_ r: MeshRevocationNotice) -> Bool {
        lock.lock(); defer { lock.unlock() }
        if notices[r.deviceId] != nil { return false }
        notices[r.deviceId] = r
        while notices.count > maxNotices {
            guard let oldest = notices.keys.first else { break }
            notices.removeValue(forKey: oldest)
        }
        return true
    }

    /// The stored notice for a device, if any.
    func notice(_ deviceId: String) -> MeshRevocationNotice? {
        lock.lock(); defer { lock.unlock() }
        return notices[deviceId]
    }

    /// All stored notices (for re-broadcast to newly discovered peers).
    func all() -> [MeshRevocationNotice] {
        lock.lock(); defer { lock.unlock() }
        return Array(notices.values)
    }

    /// How many distinct revoked devices this node knows about.
    func count() -> Int {
        lock.lock(); defer { lock.unlock() }
        return notices.count
    }
}
