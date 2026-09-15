import Foundation
import CryptoKit
import Security

// MeshIdentity.swift — per-peer session keys, signed device identity, key
// revocation and anti-replay for the native iOS mesh client.
//
// This mirrors services/mesh (session.go, signing.go, replay.go) and the
// Android MeshIdentity.kt so a device behaves identically to the Go engine:
// discovery beacons are Ed25519-signed and advertise an X25519 key-agreement
// public key; unicast payloads are sealed with a per-peer AES-256 session key
// derived via ECDH + HKDF-SHA256. A device that never held a peer's private
// key cannot derive the session key. It depends only on Foundation + CryptoKit.
//
// The wire format matches the Go engine's SignedBeacon JSON so a native
// device and a Go node interoperate.

/// A device's mesh identity: an Ed25519 beacon-signing key pair and an X25519
/// key-agreement pair (with an epoch for rotation). The public halves are
/// advertised inside signed beacons; peers derive per-peer session keys from
/// the X25519 pair.
final class MeshIdentity {

    /// Ed25519 beacon-signing key pair (stable for the device lifetime).
    private let signer: Curve25519.Signing.PrivateKey

    /// X25519 key-agreement pair; replaced on `rotate`. Guarded by `lock`.
    private var kem: Curve25519.KeyAgreement.PrivateKey

    /// Rotation epoch; bumped on `rotate`.
    private(set) var epoch: Int64 = 0

    private let lock = NSLock()

    init() {
        signer = Curve25519.Signing.PrivateKey()
        kem = Curve25519.KeyAgreement.PrivateKey()
    }

    /// The Ed25519 public key (raw 32 bytes).
    func signPublic() -> Data { signer.publicKey.rawRepresentation }

    /// The current X25519 public key (raw 32 bytes).
    func kemPublic() -> Data {
        lock.lock(); defer { lock.unlock() }
        return kem.publicKey.rawRepresentation
    }

    /// Regenerates the key-agreement pair and bumps the epoch.
    func rotate() {
        lock.lock(); defer { lock.unlock() }
        kem = Curve25519.KeyAgreement.PrivateKey()
        epoch += 1
    }

    /// Signs a beacon (with this device's key-agreement advertisement) and
    /// returns the SignedBeacon JSON, byte-compatible with the Go engine.
    func signBeacon(deviceId: String, kind: String, transport: String, addr: String, seq: Int64) -> Data {
        let beacon: [String: Any] = [
            "device_id": deviceId,
            "kind": kind,
            "transport": transport,
            "addr": addr,
            "seq": seq,
        ]
        let kemPub = kemPublic()
        let payload = canonicalPayload(beacon: beacon, pubKey: signPublic(), kemPub: kemPub, kemEpoch: epoch)
        let sig = try! signer.signature(for: payload)
        let sb: [String: Any] = [
            "beacon": beacon,
            "pub_key": signPublic().base64EncodedString(),
            "kem_pub": kemPub.base64EncodedString(),
            "kem_epoch": epoch,
            "sig": sig.base64EncodedString(),
        ]
        return (try? JSONSerialization.data(withJSONObject: sb)) ?? Data()
    }

    /// Verifies a signed beacon against the pinned device-key map (TOFU) and
    /// returns the parsed beacon fields, or nil when the signature is invalid,
    /// the key is revoked, or a pinned identity is claimed by a different key.
    func verifySignedBeacon(
        data: Data,
        pinned: inout [String: Data],
        revoked: [String: Data]
    ) -> SignedBeaconInfo? {
        guard let o = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let beacon = o["beacon"] as? [String: Any],
              let deviceId = beacon["device_id"] as? String, !deviceId.isEmpty,
              let pubKey = Data(base64Encoded: (o["pub_key"] as? String) ?? ""),
              let sig = Data(base64Encoded: (o["sig"] as? String) ?? ""),
              pubKey.count == 32, !sig.isEmpty
        else { return nil }
        // Revoked identity may not re-enter.
        if revoked[deviceId] != nil { return nil }
        // Pinned identity may not be claimed by a different key.
        if let prev = pinned[deviceId], prev != pubKey { return nil }
        let kemPub = Data(base64Encoded: (o["kem_pub"] as? String) ?? "") ?? Data()
        let kemEpoch = (o["kem_epoch"] as? NSNumber)?.int64Value ?? 0
        let payload = canonicalPayload(beacon: beacon, pubKey: pubKey, kemPub: kemPub, kemEpoch: kemEpoch)
        guard let pub = try? Curve25519.Signing.PublicKey(rawRepresentation: pubKey),
              pub.isValidSignature(sig, for: payload)
        else { return nil }
        pinned[deviceId] = pubKey
        return SignedBeaconInfo(
            deviceId: deviceId,
            kind: (beacon["kind"] as? String) ?? "member",
            transport: (beacon["transport"] as? String) ?? "local_wifi",
            addr: (beacon["addr"] as? String) ?? "",
            seq: (beacon["seq"] as? NSNumber)?.int64Value ?? 0,
            kemPub: kemPub,
            kemEpoch: kemEpoch
        )
    }

    /// Derives the per-peer AES-256 session key shared with a remote device.
    /// Both sides compute the same key: X25519 is symmetric and the HKDF salt
    /// is the sorted device-id pair, so derivation is order-independent.
    func sessionKey(localId: String, remotePub: Data, remoteId: String) -> Data? {
        guard remotePub.count == 32,
              let remote = try? Curve25519.KeyAgreement.PublicKey(rawRepresentation: remotePub)
        else { return nil }
        lock.lock(); defer { lock.unlock() }
        guard let shared = try? kem.sharedSecretFromKeyAgreement(with: remote) else { return nil }
        let a = localId
        let b = remoteId
        let salt = (a <= b) ? "\(a)|\(b)" : "\(b)|\(a)"
        let info = Data(MeshIdentity.sessionInfo.utf8)
        return MeshIdentity.hkdfSha256(ikm: shared.withUnsafeBytes { Data($0) },
                                       salt: Data(salt.utf8), info: info, length: 32)
    }

    /// Canonical, signature-covered projection of a SignedBeacon.
    private func canonicalPayload(beacon: [String: Any], pubKey: Data, kemPub: Data, kemEpoch: Int64) -> Data {
        let o: [String: Any] = [
            "beacon": beacon,
            "pub_key": pubKey.base64EncodedString(),
            "kem_pub": kemPub.base64EncodedString(),
            "kem_epoch": kemEpoch,
        ]
        return (try? JSONSerialization.data(withJSONObject: o)) ?? Data()
    }

    static let sessionInfo = "chatapp-mesh-session-v2"

    /// HKDF-SHA256 (RFC 5869) with the given salt, info and length.
    static func hkdfSha256(ikm: Data, salt: Data, info: Data, length: Int) -> Data {
        // Extract.
        let prk = HMAC<SHA256>.authenticationCode(for: ikm, using: SymmetricKey(data: salt))
        // Expand.
        var out = Data()
        var t = Data()
        var counter: UInt8 = 1
        while out.count < length {
            var mac = HMAC<SHA256>.init(key: SymmetricKey(data: Data(prk)))
            mac.update(data: t)
            mac.update(data: info)
            mac.update(data: Data([counter]))
            t = Data(mac.finalize())
            out.append(t)
            counter += 1
        }
        return out.prefix(length)
    }
}

/// Parsed, verified signed-beacon fields.
struct SignedBeaconInfo {
    let deviceId: String
    let kind: String
    let transport: String
    let addr: String
    let seq: Int64
    let kemPub: Data
    let kemEpoch: Int64
}

/// Anti-replay protection: a sliding bitmap window per source device id,
/// mirroring services/mesh/replay.go. Sequence numbers are monotonic per
/// sender; anything older than the window or already accepted is a replay.
final class ReplayFilter {

    private final class Window {
        let bits: Int
        var base: Int64 = 1
        var bitmap: [UInt64]

        init(bits: Int) {
            self.bits = bits
            self.bitmap = [UInt64](repeating: 0, count: bits / 64)
        }

        func accept(_ seq: Int64) -> Bool {
            if seq <= 0 || seq < base { return false }
            if seq >= base + Int64(bits) {
                let shift = seq - (base + Int64(bits) - 1)
                if shift >= Int64(bits) {
                    for i in bitmap.indices { bitmap[i] = 0 }
                } else {
                    for i in bitmap.indices {
                        var v: UInt64 = 0
                        let ni = i + Int(shift / 64)
                        if ni < bitmap.count { v = bitmap[ni] >> UInt64(shift % 64) }
                        let ni2 = ni + 1
                        if shift % 64 != 0 && ni2 < bitmap.count {
                            v |= bitmap[ni2] << UInt64(64 - (shift % 64))
                        }
                        bitmap[i] = v
                    }
                }
                base = seq - Int64(bits) + 1
            }
            let idx = Int(seq - base)
            let word = idx / 64
            let bit = idx % 64
            if (bitmap[word] & (1 << UInt64(bit))) != 0 { return false }
            bitmap[word] |= (1 << UInt64(bit))
            return true
        }
    }

    private let windowBits: Int
    private var windows: [String: Window] = [:]
    private let lock = NSLock()

    init(windowBits: Int = 1024) {
        self.windowBits = windowBits
    }

    /// Reports whether (src, seq) is new, recording it when it is.
    func check(src: String, seq: Int64) -> Bool {
        lock.lock(); defer { lock.unlock() }
        let w: Window
        if let existing = windows[src] {
            w = existing
        } else {
            w = Window(bits: windowBits)
            windows[src] = w
        }
        return w.accept(seq)
    }
}
