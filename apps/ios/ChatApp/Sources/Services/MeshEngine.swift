import Foundation
import CryptoKit
import Security

// MeshEngine.swift — transport-agnostic mesh logic for the iOS client.
//
// Mirrors services/mesh (routing.go, storeforward.go, packet.go) and the
// Android MeshEngine.kt so a device behaves identically whichever radio is
// carrying packets: the same packet envelope, TTL/dedup rules, and
// store-and-forward queue. Depends only on Foundation + CryptoKit, so the
// routing and queue logic is unit-testable without radio hardware.

/// A mesh packet, byte-compatible with the Go engine's Packet envelope.
struct MeshPacket {
    let id: String
    let src: String
    let dst: String
    let groupId: String?
    let kind: String
    var ttl: Int
    var hops: Int
    let payload: Data
    let nonce: Data
    let createdAt: Int64
}

/// A known peer on the mesh.
struct MeshNeighbor {
    let deviceId: String
    let addr: String
    let transport: String
    let relayOk: Bool
    let lastSeen: Date
}

/// Owns the routing table, dedup cache, store-and-forward queue and link chain.
final class MeshEngine {

    static let defaultMaxHops = 64
    private static let maxSeen = 10_000

    private let deviceId: String
    private let key: SymmetricKey
    private let maxHops: Int
    private let maxQueue: Int
    private let maxAge: TimeInterval

    private var neighbors: [String: MeshNeighbor] = [:]
    private var seen: [String: Date] = [:]
    private var queue: [MeshPacket] = []
    private let lock = NSLock()

    private var links: [MeshLink] = []

    /// Application-layer delivery callback (decrypted packet).
    var delivered: ((MeshPacket, Data?) -> Void)?

    /// The transport currently carrying traffic, or "none".
    private(set) var activeTransport: String = "none"

    /// Whether this device consents to relay other devices' packets.
    var relayOk: Bool = true

    init(deviceId: String, key: Data, maxHops: Int = MeshEngine.defaultMaxHops,
         maxQueue: Int = 1000, maxAge: TimeInterval = 7 * 24 * 60 * 60) {
        self.deviceId = deviceId
        self.key = SymmetricKey(data: key)
        self.maxHops = maxHops
        self.maxQueue = maxQueue
        self.maxAge = maxAge
    }

    /// Wires the ordered transport chain (Anonymous.md §5.3).
    func attach(_ chain: [MeshLink]) {
        links = chain
        for link in chain where link.isAvailable() {
            link.start()
            if activeTransport == "none" { activeTransport = link.kind }
        }
        if chain.allSatisfy({ !$0.isAvailable() }) { activeTransport = "none" }
    }

    /// Re-evaluates which link is usable (e.g. after radios change state).
    func refreshTransport() {
        activeTransport = links.first(where: { $0.isAvailable() })?.kind ?? "none"
    }

    func stop() {
        links.forEach { $0.stop() }
        activeTransport = "none"
    }

    // MARK: discovery

    func upsertNeighbor(deviceId: String, addr: String, transport: String, now: Date = Date()) {
        lock.lock(); defer { lock.unlock() }
        neighbors[deviceId] = MeshNeighbor(deviceId: deviceId, addr: addr,
                                           transport: transport, relayOk: true, lastSeen: now)
    }

    func neighborList() -> [MeshNeighbor] {
        lock.lock(); defer { lock.unlock() }
        return Array(neighbors.values)
    }

    /// Serializes a presence beacon; routing metadata only, never content.
    func beacon(seq: Int64) -> Data {
        let obj: [String: Any] = [
            "device_id": deviceId,
            "kind": relayOk ? "relay" : "member",
            "transport": activeTransport,
            "addr": links.first?.kind ?? "",
            "seq": seq,
        ]
        return (try? JSONSerialization.data(withJSONObject: obj)) ?? Data()
    }

    // MARK: sending

    /// Encrypts a payload and queues a packet for `dst`. Returns the packet id.
    @discardableResult
    func send(kind: String, dst: String, plaintext: Data, groupId: String? = nil) -> String {
        let nonce = MeshCrypto.randomNonce()
        let sealed = MeshCrypto.encrypt(key: key, plaintext: plaintext, nonce: nonce)
        let p = MeshPacket(id: MeshCrypto.randomId(), src: deviceId, dst: dst, groupId: groupId, kind: kind,
                           ttl: maxHops, hops: 0, payload: sealed, nonce: nonce,
                           createdAt: Int64(Date().timeIntervalSince1970 * 1000))
        lock.lock()
        seen[p.id] = Date()
        lock.unlock()
        enqueue(p)
        _ = flush()
        return p.id
    }

    func enqueue(_ p: MeshPacket) {
        lock.lock(); defer { lock.unlock() }
        queue.append(p)
        while queue.count > maxQueue { queue.removeFirst() }
    }

    /// Packets still valid (unexpired, TTL remaining). Does not remove them.
    func pending(now: Date = Date()) -> [MeshPacket] {
        lock.lock(); defer { lock.unlock() }
        queue.removeAll { now.timeIntervalSince1970 - Double($0.createdAt) / 1000 > maxAge || $0.ttl <= 0 }
        return queue
    }

    func dequeue(_ id: String) {
        lock.lock(); defer { lock.unlock() }
        queue.removeAll { $0.id == id }
    }

    func queueSize() -> Int {
        lock.lock(); defer { lock.unlock() }
        return queue.count
    }

    // MARK: receiving

    /// A beacon registers a neighbour; a packet is deduped, delivered or forwarded.
    @discardableResult
    func handleInbound(addr: String, data: Data, now: Date = Date()) -> MeshPacket? {
        if let b = parseBeacon(data) {
            upsertNeighbor(deviceId: b.0, addr: addr, transport: b.1, now: now)
            return nil
        }
        guard let p = MeshPacketCodec.decode(data) else { return nil }

        lock.lock()
        if seen[p.id] != nil { lock.unlock(); return nil }
        seen[p.id] = now
        if seen.count > MeshEngine.maxSeen {
            let cutoff = now.addingTimeInterval(-maxAge)
            seen = seen.filter { $0.value >= cutoff }
        }
        lock.unlock()

        if p.dst == deviceId {
            dequeue(p.id)
            delivered?(p, MeshCrypto.decrypt(key: key, ciphertext: p.payload, nonce: p.nonce))
            return p
        }

        if p.ttl <= 0 { return nil }
        var forwarded = p
        forwarded.ttl -= 1
        forwarded.hops += 1
        enqueue(forwarded)
        _ = flush()
        return forwarded
    }

    /// Hands queued packets to a known neighbour; anything unroutable stays
    /// queued — that is the store-and-forward path.
    @discardableResult
    func flush() -> Int {
        guard let target = links.first(where: { $0.isAvailable() }) else { return 0 }
        var sent = 0
        for p in pending() {
            for nb in neighborList() where relayOk || nb.deviceId == p.dst {
                if target.send(addr: nb.addr, data: MeshPacketCodec.encode(p)) {
                    dequeue(p.id)
                    sent += 1
                    break
                }
            }
        }
        return sent
    }

    private func parseBeacon(_ data: Data) -> (String, String)? {
        guard let o = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let id = o["device_id"] as? String, !id.isEmpty else { return nil }
        return (id, (o["transport"] as? String) ?? "local_wifi")
    }
}

/// Packet wire codec — JSON, matching the Go engine's marshal layout.
enum MeshPacketCodec {

    static func encode(_ p: MeshPacket) -> Data {
        var obj: [String: Any] = [
            "id": p.id, "src": p.src, "dst": p.dst,
            "kind": p.kind, "ttl": p.ttl, "hops": p.hops,
            "payload": p.payload.base64EncodedString(),
            "nonce": p.nonce.base64EncodedString(),
            "created_at": p.createdAt,
        ]
        if let groupId = p.groupId { obj["group_id"] = groupId }
        return (try? JSONSerialization.data(withJSONObject: obj)) ?? Data()
    }

    static func decode(_ data: Data) -> MeshPacket? {
        guard let o = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let id = o["id"] as? String, let src = o["src"] as? String, let dst = o["dst"] as? String
        else { return nil }
        return MeshPacket(
            id: id, src: src, dst: dst,
            groupId: o["group_id"] as? String,
            kind: (o["kind"] as? String) ?? "message",
            ttl: (o["ttl"] as? Int) ?? MeshEngine.defaultMaxHops,
            hops: (o["hops"] as? Int) ?? 0,
            payload: Data(base64Encoded: (o["payload"] as? String) ?? "") ?? Data(),
            nonce: Data(base64Encoded: (o["nonce"] as? String) ?? "") ?? Data(),
            createdAt: Int64((o["created_at"] as? NSNumber)?.int64Value ?? 0)
        )
    }
}

/// Authenticated encryption shared by Go, Android and iOS mesh clients.
enum MeshCrypto {

    static func randomNonce() -> Data {
        var bytes = [UInt8](repeating: 0, count: 12)
        _ = SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes)
        return Data(bytes)
    }

    static func randomId() -> String {
        var bytes = [UInt8](repeating: 0, count: 16)
        _ = SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes)
        return bytes.map { String(format: "%02x", $0) }.joined()
    }

    static func encrypt(key: SymmetricKey, plaintext: Data, nonce: Data) -> Data {
        guard let nonce = try? AES.GCM.Nonce(data: nonce),
              let sealed = try? AES.GCM.seal(plaintext, using: key, nonce: nonce) else { return Data() }
        return sealed.ciphertext + sealed.tag
    }

    /// Returns the plaintext, or nil when the key is wrong or data was tampered.
    static func decrypt(key: SymmetricKey, ciphertext: Data, nonce: Data) -> Data? {
        guard let box = try? AES.GCM.SealedBox(combined: nonce + ciphertext) else { return nil }
        return try? AES.GCM.open(box, using: key)
    }
}
