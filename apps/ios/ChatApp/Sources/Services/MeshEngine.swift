import Foundation
import CryptoKit
import Security

// MeshEngine.swift — transport-agnostic mesh logic for the iOS client.
//
// Mirrors services/mesh (packet.go, routing.go, storeforward.go, reliability.go,
// priority.go, pfifo.go, fragment.go, groupkey.go, groupack.go, revocdist.go)
// and the Android MeshEngine.kt so a device behaves identically whichever
// radio is carrying packets: the same versioned packet envelope, kind
// validation, TTL/dedup/replay rules, priority queueing, reliable ACKed
// transfers with bounded retries, MTU fragmentation, rotating group keys with
// per-member acknowledgements, and network-wide revocation distribution.
//
// Identity and crypto (MeshIdentity.swift): discovery beacons are Ed25519-
// signed and advertise an X25519 key-agreement key; unicast payloads are
// sealed with a per-peer AES-256 session key derived via ECDH + HKDF.

/// A mesh packet, byte-compatible with the Go engine's Packet envelope.
struct MeshPacket {
    let id: String
    let src: String
    var dst: String
    let groupId: String?
    let kind: String
    var ttl: Int
    var hops: Int
    let payload: Data
    let nonce: Data
    let createdAt: Int64
    let seq: Int64
    var version: Int = MeshEngine.meshVersion
    var hopSrc: String? = nil
    var ackFor: String? = nil
    var xfer: String? = nil
    var fragIndex: Int = 0
    var fragTotal: Int = 0
    var fragId: String? = nil
    var fragSum: Data? = nil
}

/// A known peer on the mesh.
struct MeshNeighbor {
    let deviceId: String
    let addr: String
    let transport: String
    let relayOk: Bool
    let lastSeen: Date
}

/// Owns the routing table, dedup cache, priority queue, reliable transfers,
/// group keys and revocation store.
final class MeshEngine {

    static let defaultMaxHops = 64
    private static let maxSeen = 10_000

    /// Envelope version this engine stamps; newer formats are rejected.
    static let meshVersion = 1

    /// Fragment payload ceiling, matching services/mesh/fragment.go.
    static let defaultMaxPayload = 512

    /// Reliable-transfer ceiling, matching services/mesh/reliability.go.
    static let maxReliablePayload = 64 * 1024

    /// Forwarding-buffer byte bound, matching services/mesh/pfifo.go.
    static let defaultQueueMaxBytes = 8 * 1024 * 1024

    static let kindMessage = "message"
    static let kindGroupMessage = "group_message"
    static let kindVoiceMessage = "voice_message"
    static let kindCallSignal = "call_signal"
    static let kindAck = "ack"

    /// Kinds the engine accepts, matching services/mesh Validate().
    static let validKinds: Set<String> = [kindMessage, kindGroupMessage, kindVoiceMessage, kindCallSignal, kindAck]

    private let deviceId: String
    private let key: SymmetricKey
    private let maxHops: Int
    private let maxQueue: Int
    private let maxAgeMs: Int64

    private let lock = NSLock()
    private var links: [MeshLink] = []
    private var neighbors: [String: MeshNeighbor] = [:]
    private var seen: [String: Int64] = [:]
    private let pfifo = MeshPriorityQueue(maxPackets: 1000, maxBytes: MeshEngine.defaultQueueMaxBytes, maxAgeMs: 7 * 24 * 3600 * 1000)

    /// Device identity: Ed25519 signing + X25519 key agreement.
    let identity = MeshIdentity()

    /// Pinned device id -> Ed25519 public key (trust-on-first-use).
    private var pinnedKeys: [String: Data] = [:]

    /// Revoked device id -> revoked Ed25519 public key.
    private var revokedKeys: [String: Data] = [:]

    /// Peer device id -> advertised X25519 key-agreement key + epoch.
    private var peerKEM: [String: (Data, Int64)] = [:]

    /// Per-group sender keys and rotation (see MeshGroups.swift).
    let groupKeys = GroupKeyManager()

    /// Reliable unicast transfers (see MeshReliability.swift).
    let transfers = TransferTracker()

    /// Per-member group acknowledgements (see MeshGroups.swift).
    let groupAcks = GroupAckTracker()

    /// MTU-bounded fragment reassembly (see MeshFragments.swift).
    private let frags = FragmentAssembler()

    /// Flood-deduplicated revocation notices (see MeshRevocation.swift).
    private let revStore = RevocationStore()

    /// Group id -> member roster recorded by the application.
    private var groupMembers: [String: [String]] = [:]

    /// Per-source anti-replay windows.
    private let replay = ReplayFilter()

    /// Monotonic per-sender sequence number.
    private var seqCtr: Int64 = 0

    /// Application-layer delivery callback (reassembled packet + plaintext).
    var delivered: ((MeshPacket, Data?) -> Void)?

    /// The transport currently carrying traffic, or "none".
    private(set) var activeTransport: String = "none"

    /// Whether this device consents to relay other devices' packets.
    var relayOk: Bool = true

    init(deviceId: String, key: Data, maxHops: Int = MeshEngine.defaultMaxHops,
         maxQueue: Int = 1000, maxAgeMs: Int64 = 7 * 24 * 3600 * 1000) {
        self.deviceId = deviceId
        self.key = SymmetricKey(data: key)
        self.maxHops = maxHops
        self.maxQueue = maxQueue
        self.maxAgeMs = maxAgeMs
    }

    private func keyData() -> Data { key.withUnsafeBytes { Data($0) } }
    private func nowMs() -> Int64 { Int64(Date().timeIntervalSince1970 * 1000) }

    // MARK: transport chain

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

    func upsertNeighbor(deviceId: String, addr: String, transport: String, relayOk: Bool = true, now: Date = Date()) {
        lock.lock(); defer { lock.unlock() }
        neighbors[deviceId] = MeshNeighbor(deviceId: deviceId, addr: addr,
                                           transport: transport, relayOk: relayOk, lastSeen: now)
    }

    func neighborList() -> [MeshNeighbor] {
        lock.lock(); defer { lock.unlock() }
        return Array(neighbors.values)
    }

    /// Serializes a signed presence beacon carrying routing metadata and this
    /// device's X25519 key-agreement advertisement.
    func beacon(seq: Int64) -> Data {
        identity.signBeacon(deviceId: deviceId,
                            kind: relayOk ? "relay" : "member",
                            transport: activeTransport,
                            addr: links.first?.kind ?? "",
                            seq: seq)
    }

    // MARK: sending

    /// Encrypts a payload and queues a packet for `dst`. Returns the packet id.
    @discardableResult
    func send(kind: String, dst: String, plaintext: Data, groupId: String? = nil) -> String {
        let sessionKey = sessionKeyFor(dst)
        let nonce = MeshCrypto.randomNonce()
        let sealed = MeshCrypto.encrypt(key: SymmetricKey(data: sessionKey), plaintext: plaintext, nonce: nonce)
        let p = MeshPacket(id: MeshCrypto.randomId(), src: deviceId, dst: dst, groupId: groupId, kind: kind,
                           ttl: maxHops, hops: 0, payload: sealed, nonce: nonce,
                           createdAt: nowMs(), seq: nextSeq())
        lock.lock()
        seen[p.id] = nowMs()
        lock.unlock()
        enqueue(p)
        _ = flush()
        return p.id
    }

    /// Sends a reliable unicast transfer: the payload carries a transfer id and
    /// the destination acknowledges it end-to-end. Retry with tick(); the
    /// delivery state is user-visible (see TransferTracker).
    @discardableResult
    func sendReliable(kind: String, dst: String, plaintext: Data) -> String {
        if plaintext.count > MeshEngine.maxReliablePayload {
            return sendLarge(kind: kind, target: dst, plaintext: plaintext, group: false)
        }
        let tr = transfers.create(dst: dst, kind: kind, payload: plaintext)
        // Drive the first attempt through the SAME accounting the retry loop
        // uses, so the transfer's state reflects a real transmission now.
        tick()
        return tr.id
    }

    /// Sends a group message sealed under the group's per-group sender key.
    /// Members acknowledge per-member (see GroupAckTracker) when
    /// trackGroupMembers recorded the roster.
    @discardableResult
    func sendGroup(kind: String, groupId: String, plaintext: Data) -> String {
        if plaintext.count > MeshEngine.defaultMaxPayload {
            return sendLarge(kind: kind, target: groupId, plaintext: plaintext, group: true)
        }
        let gk = groupKeys.keyFor(groupId)
        let nonce = MeshCrypto.randomNonce()
        let sealed = MeshCrypto.encrypt(key: SymmetricKey(data: gk.key), plaintext: plaintext, nonce: nonce)
        let xferId = MeshCrypto.randomId()
        let p = MeshPacket(id: MeshCrypto.randomId(), src: deviceId, dst: "", groupId: groupId, kind: kind,
                           ttl: maxHops, hops: 0, payload: sealed, nonce: nonce,
                           createdAt: nowMs(), seq: nextSeq(), xfer: xferId)
        if let members = groupMembers[groupId], !members.isEmpty {
            groupAcks.create(xferId, groupId: groupId, members: members)
            _ = transfers.createForForeign(id: xferId, groupId: groupId, kind: kind, payload: plaintext)
        }
        lock.lock()
        seen[p.id] = nowMs()
        lock.unlock()
        enqueue(p)
        _ = flush()
        return xferId
    }

    /// Splits a payload that exceeds one radio datagram into MTU-bounded,
    /// individually encrypted fragments and enqueues every one. Returns the
    /// fragment-group id, or the packet id when the payload fit unfragmented.
    @discardableResult
    func sendLarge(kind: String, target: String, plaintext: Data, group: Bool) -> String {
        let parts = meshSplitPayload(plaintext, maxPayload: MeshEngine.defaultMaxPayload)
        guard let fragId = parts.fragId else {
            return group ? sendGroup(kind: kind, groupId: target, plaintext: plaintext)
                         : send(kind: kind, dst: target, plaintext: plaintext)
        }
        let encKey = group ? groupKeys.keyFor(target).key : sessionKeyFor(target)
        for (i, chunk) in parts.chunks.enumerated() {
            let nonce = MeshCrypto.randomNonce()
            let sealed = MeshCrypto.encrypt(key: SymmetricKey(data: encKey), plaintext: chunk, nonce: nonce)
            let p = MeshPacket(id: MeshCrypto.randomId(), src: deviceId,
                               dst: group ? "" : target,
                               groupId: group ? target : nil,
                               kind: kind, ttl: maxHops, hops: 0,
                               payload: sealed, nonce: nonce,
                               createdAt: nowMs(), seq: nextSeq(),
                               fragIndex: i, fragTotal: parts.chunks.count,
                               fragId: fragId, fragSum: parts.digest)
            lock.lock()
            seen[p.id] = nowMs()
            lock.unlock()
            enqueue(p)
        }
        _ = flush()
        return fragId
    }

    /// Records a group's member roster so group sends track per-member acks.
    func trackGroupMembers(groupId: String, members: [String]) {
        lock.lock(); defer { lock.unlock() }
        groupMembers[groupId] = members.filter { $0 != deviceId }
    }

    /// Advances the retry state machine; retransmits every due transfer.
    @discardableResult
    func tick(nowMs: Int64? = nil) -> Int {
        var sent = 0
        for tr in transfers.tick(nowMs) {
            transmit(transferId: tr.id)
            sent += 1
        }
        return sent
    }

    /// Transmits one attempt of a reliable transfer (fresh packet id + nonce).
    private func transmit(transferId: String) {
        guard let tr = transfers.get(transferId), tr.state != .acked && tr.state != .expired && tr.state != .deadLetter,
              let payload = transfers.payload(transferId), !payload.isEmpty else { return }
        let nonce = MeshCrypto.randomNonce()
        let sealed = MeshCrypto.encrypt(key: SymmetricKey(data: sessionKeyFor(tr.dst)), plaintext: payload, nonce: nonce)
        let p = MeshPacket(id: MeshCrypto.randomId(), src: deviceId, dst: tr.dst, groupId: nil, kind: tr.kind,
                           ttl: maxHops, hops: 0, payload: sealed, nonce: nonce,
                           createdAt: nowMs(), seq: nextSeq(), xfer: transferId)
        lock.lock()
        seen[p.id] = nowMs()
        lock.unlock()
        transfers.notePacket(transferId, packetId: p.id)
        enqueue(p)
        _ = flush()
    }

    func enqueue(_ p: MeshPacket) {
        _ = pfifo.enqueue(p, nowMs: nowMs())
    }

    /// Packets still valid (unexpired, TTL remaining), in priority order.
    func pending() -> [MeshPacket] { pfifo.pending(nowMs()) }

    func dequeue(_ id: String) { pfifo.remove(id) }

    func queueSize() -> Int { pfifo.size() }

    // MARK: receiving

    /// Handles a raw inbound datagram: a signed beacon registers a neighbour, a
    /// revocation notice is verified and flooded, and a packet is deduplicated,
    /// delivered locally when addressed to us (unicast or group), or forwarded.
    @discardableResult
    func handleInbound(addr: String, data: Data, now: Date = Date()) -> MeshPacket? {
        let nowM = nowMs()
        // A network-wide revocation notice: verify it against the pinned
        // identities and apply it if valid, so a revocation propagates beyond
        // the node that issued it.
        if let r = MeshRevocationNotice.unmarshal(data) {
            handleRevocation(r, now: nowM)
            return nil
        }
        // Signed beacon: verify the signature and pinned-key binding before
        // trusting the advertised route. A revoked identity is rejected.
        if let b = identity.verifySignedBeacon(data: data, pinned: &pinnedKeys, revoked: revokedKeys) {
            upsertNeighbor(deviceId: b.deviceId, addr: addr, transport: b.transport,
                           relayOk: b.kind == "relay", now: now)
            // Pin the peer's advertised key-agreement key (signed, so it is
            // bound to the verified device identity). A newer epoch replaces
            // the stored key after a peer's rotation.
            if b.kemPub.count == 32 {
                lock.lock()
                if let prev = peerKEM[b.deviceId] {
                    if b.kemEpoch >= prev.1 { peerKEM[b.deviceId] = (b.kemPub, b.kemEpoch) }
                } else {
                    peerKEM[b.deviceId] = (b.kemPub, b.kemEpoch)
                }
                lock.unlock()
            }
            return nil
        }
        guard let p = MeshPacketCodec.decode(data) else { return nil }

        lock.lock()
        if seen[p.id] != nil { lock.unlock(); return nil }
        seen[p.id] = nowM
        if seen.count > MeshEngine.maxSeen { pruneSeenLocked(nowM) }
        lock.unlock()

        if p.dst == deviceId || (p.dst.isEmpty && p.groupId != nil) {
            dequeue(p.id)
            deliverLocal(p)
            return p
        }

        if p.ttl <= 0 { return nil }
        var forwarded = p
        forwarded.ttl -= 1
        forwarded.hops += 1
        forwarded.hopSrc = deviceId
        enqueue(forwarded)
        _ = flush()
        return forwarded
    }

    /// Deliver-local path: acks are consumed; data is reassembled and delivered exactly once.
    private func deliverLocal(_ p: MeshPacket) {
        // An acknowledgement settles a reliable transfer and is consumed here.
        if p.kind == MeshEngine.kindAck {
            guard let proof = decryptPayload(p) else { return }
            if let groupId = p.groupId {
                guard String(data: proof, encoding: .utf8) == "chatapp-mesh-groupack-v1:\(groupId):\(p.ackFor ?? "")" else { return }
                if p.seq != 0 && !replay.check(src: p.src, seq: p.seq) { return }
                if let ackFor = p.ackFor {
                    _ = groupAcks.ack(ackFor, member: p.src)
                    _ = transfers.ack(ackFor)
                }
                return
            }
            guard String(data: proof, encoding: .utf8) == "chatapp-mesh-ack-v1:\(p.ackFor ?? "")" else { return }
            if p.seq != 0 && !replay.check(src: p.src, seq: p.seq) { return }
            if let ackFor = p.ackFor { _ = transfers.ack(ackFor) }
            return
        }
        guard let pt = decryptPayload(p) else { return }
        // Anti-replay runs AFTER the payload authenticates: a forged packet
        // must never be able to advance or poison the replay window.
        if p.seq != 0 && !replay.check(src: p.src, seq: p.seq) { return }
        // A fragmented payload is withheld from the application until every
        // part has arrived and the digest verifies.
        guard let full = frags.add(p, pt) else { return }
        // A reliable transfer delivers exactly once no matter how many copies
        // were retransmitted, and EVERY copy is acknowledged so a lost ACK
        // converges.
        if let xferId = p.xfer, !xferId.isEmpty {
            if transfers.markDelivered(xferId) {
                delivered?(p, full)
            }
            if let groupId = p.groupId {
                sendGroupAck(dst: p.src, groupId: groupId, transferId: xferId)
            } else {
                sendAck(dst: p.src, transferId: xferId)
            }
            return
        }
        // Best-effort packet: handleInbound's dedup cache guarantees the
        // first (and only) copy is the one being delivered here.
        delivered?(p, full)
    }

    /// Returns an acknowledgement for a settled unicast transfer to its origin.
    private func sendAck(dst: String, transferId: String) {
        if dst.isEmpty || transferId.isEmpty { return }
        let proof = Data("chatapp-mesh-ack-v1:\(transferId)".utf8)
        let nonce = MeshCrypto.randomNonce()
        let sealed = MeshCrypto.encrypt(key: SymmetricKey(data: sessionKeyFor(dst)), plaintext: proof, nonce: nonce)
        let p = MeshPacket(id: MeshCrypto.randomId(), src: deviceId, dst: dst, groupId: nil,
                           kind: MeshEngine.kindAck, ttl: maxHops, hops: 0,
                           payload: sealed, nonce: nonce, createdAt: nowMs(), seq: nextSeq(),
                           ackFor: transferId, xfer: transferId)
        lock.lock()
        seen[p.id] = nowMs()
        lock.unlock()
        enqueue(p)
        _ = flush()
    }

    /// Returns a per-member acknowledgement for a group transfer to its origin.
    private func sendGroupAck(dst: String, groupId: String, transferId: String) {
        if dst.isEmpty || groupId.isEmpty || transferId.isEmpty { return }
        let proof = Data("chatapp-mesh-groupack-v1:\(groupId):\(transferId)".utf8)
        let nonce = MeshCrypto.randomNonce()
        let sealed = MeshCrypto.encrypt(key: SymmetricKey(data: sessionKeyFor(dst)), plaintext: proof, nonce: nonce)
        let p = MeshPacket(id: MeshCrypto.randomId(), src: deviceId, dst: dst, groupId: groupId,
                           kind: MeshEngine.kindAck, ttl: maxHops, hops: 0,
                           payload: sealed, nonce: nonce, createdAt: nowMs(), seq: nextSeq(),
                           ackFor: transferId, xfer: transferId)
        lock.lock()
        seen[p.id] = nowMs()
        lock.unlock()
        enqueue(p)
        _ = flush()
    }

    /// Attempts to hand queued packets to a known neighbour. Packets stay queued
    /// when no link can carry them yet — that is the store-and-forward path.
    @discardableResult
    func flush() -> Int {
        guard let target = links.first(where: { $0.isAvailable() }) else { return 0 }
        var sent = 0
        for p in pending() {
            for nb in neighborList() {
                if !relayOk && nb.deviceId != p.dst { continue }
                if target.send(addr: nb.addr, data: MeshPacketCodec.encode(p)) {
                    dequeue(p.id)
                    sent += 1
                    break
                }
            }
        }
        return sent
    }

    // MARK: revocation

    /// Locally revokes a pinned device key and floods a signed, network-wide
    /// notice so every relay applies the same decision. The expected public key
    /// must match the pinned key so a device id alone cannot revoke an
    /// unrelated identity.
    @discardableResult
    func revokePeer(deviceId: String, expectedPub: Data) -> Data? {
        lock.lock()
        guard let pinned = pinnedKeys[deviceId], pinned == expectedPub else {
            lock.unlock(); return nil
        }
        lock.unlock()
        guard let r = MeshRevocationNotice.sign(identity: identity, revoker: self.deviceId,
                                                deviceId: deviceId, publicKey: expectedPub) else { return nil }
        _ = revStore.apply(r)
        applyRevocation(r)
        floodRevocation(r, exclude: nil)
        return r.marshal()
    }

    /// Verifies and applies a flooded revocation notice, then propagates it.
    private func handleRevocation(_ r: MeshRevocationNotice, now: Int64) {
        lock.lock()
        let known = pinnedKeys
        lock.unlock()
        guard r.verify(known: known) else { return }
        guard revStore.apply(r) else { return }
        applyRevocation(r)
        floodRevocation(r, exclude: r.revoker)
    }

    private func floodRevocation(_ r: MeshRevocationNotice, exclude: String?) {
        let data = r.marshal()
        for nb in neighborList() where nb.relayOk && nb.deviceId != exclude {
            let flood = MeshPacket(id: MeshCrypto.randomId(), src: deviceId, dst: nb.deviceId,
                                   groupId: nil, kind: MeshEngine.kindMessage,
                                   ttl: maxHops, hops: 0, payload: data, nonce: Data(),
                                   createdAt: nowMs(), seq: nextSeq())
            enqueue(flood)
        }
        _ = flush()
    }

    /// Applies a verified revocation locally.
    private func applyRevocation(_ r: MeshRevocationNotice) {
        lock.lock()
        revokedKeys[r.deviceId] = r.publicKey
        peerKEM.removeValue(forKey: r.deviceId)
        neighbors.removeValue(forKey: r.deviceId)
        lock.unlock()
        transfers.dropTo(r.deviceId)
    }

    /// Whether this node has revoked a peer identity (locally or flooded).
    func isPeerRevoked(deviceId: String) -> Bool {
        lock.lock(); defer { lock.unlock() }
        return revokedKeys[deviceId] != nil
    }

    // MARK: group keys

    /// Installs a group key received from the group owner (epoch-monotonic).
    @discardableResult
    func adoptGroupKey(groupId: String, key: Data, epoch: Int64) -> Bool {
        groupKeys.adoptKey(groupId, key: key, epoch: epoch)
    }

    /// Forces a rotation for a group (evicting a member is done by NOT telling them).
    func rotateGroupKey(groupId: String) -> GroupKey {
        groupKeys.rotateGroup(groupId)
    }

    // MARK: identity / session-key helpers

    /// Selects the AEAD key for a payload: the per-peer ECDH session key when
    /// the destination has advertised a key-agreement key (the hardened path),
    /// otherwise the pre-shared identity key (legacy/native fallback until the
    /// peer advertises a KEM key).
    private func sessionKeyFor(_ dst: String) -> Data {
        if dst.isEmpty { return keyData() }
        lock.lock()
        let adv = peerKEM[dst]
        lock.unlock()
        guard let adv else { return keyData() }
        return identity.sessionKey(localId: deviceId, remotePub: adv.0, remoteId: dst) ?? keyData()
    }

    /// Opens a delivered packet: the group key first (group traffic), then the
    /// per-peer session key, falling back to the pre-shared identity key
    /// (interop with legacy/native senders).
    private func decryptPayload(_ p: MeshPacket) -> Data? {
        if let groupId = p.groupId, p.dst.isEmpty {
            let gk = groupKeys.keyFor(groupId)
            if let pt = MeshCrypto.decrypt(key: SymmetricKey(data: gk.key), ciphertext: p.payload, nonce: p.nonce) {
                return pt
            }
        }
        if p.dst == deviceId {
            lock.lock()
            let adv = peerKEM[p.src]
            lock.unlock()
            if let adv, let sk = identity.sessionKey(localId: deviceId, remotePub: adv.0, remoteId: p.src) {
                if let pt = MeshCrypto.decrypt(key: SymmetricKey(data: sk), ciphertext: p.payload, nonce: p.nonce) {
                    return pt
                }
            }
        }
        return MeshCrypto.decrypt(key: key, ciphertext: p.payload, nonce: p.nonce)
    }

    /// Regenerates this device's key-agreement pair and bumps the epoch.
    func rotateSessions() { identity.rotate() }

    private func nextSeq() -> Int64 {
        lock.lock(); defer { lock.unlock() }
        seqCtr += 1
        return seqCtr
    }

    private func pruneSeenLocked(_ now: Int64) {
        let cutoff = now - maxAgeMs
        seen = seen.filter { $0.value >= cutoff }
    }
}

/// Packet wire codec — JSON, matching the Go engine's marshal layout.
enum MeshPacketCodec {

    static func encode(_ p: MeshPacket) -> Data {
        var o: [String: Any] = [
            "id": p.id, "v": p.version, "src": p.src, "hop_src": p.hopSrc ?? NSNull(),
            "dst": p.dst, "group_id": p.groupId ?? NSNull(), "kind": p.kind,
            "ttl": p.ttl, "hops": p.hops,
            "payload": p.payload.base64EncodedString(),
            "nonce": p.nonce.base64EncodedString(),
            "created_at": p.createdAt, "seq": p.seq,
            "frag_index": p.fragIndex, "frag_total": p.fragTotal,
            "frag_id": p.fragId ?? NSNull(),
            "frag_sum": p.fragSum?.base64EncodedString() ?? NSNull(),
            "ack_for": p.ackFor ?? NSNull(), "xfer": p.xfer ?? NSNull(),
        ]
        _ = o // silence unused-var in release builds
        return try! JSONSerialization.data(withJSONObject: o)
    }

    static func decode(_ data: Data) -> MeshPacket? {
        guard let o = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return nil }
        // Reject a future envelope format before any other parsing work.
        let version = (o["v"] as? NSNumber)?.intValue ?? 0
        guard version <= MeshEngine.meshVersion else { return nil }
        guard let id = o["id"] as? String, !id.isEmpty,
              let src = o["src"] as? String, !src.isEmpty else { return nil }
        let kind = (o["kind"] as? String) ?? MeshEngine.kindMessage
        let fragTotal = (o["frag_total"] as? NSNumber)?.intValue ?? 0
        let fragIndex = (o["frag_index"] as? NSNumber)?.intValue ?? 0
        let fragId = (o["frag_id"] as? String).flatMap { $0.isEmpty || $0 == "null" ? nil : $0 }
        guard MeshEngine.validKinds.contains(kind) else { return nil }
        if fragTotal > 1 && (fragId == nil || fragIndex < 0 || fragIndex >= fragTotal) { return nil }
        let payload = Data(base64Encoded: (o["payload"] as? String) ?? "") ?? Data()
        let nonce = Data(base64Encoded: (o["nonce"] as? String) ?? "") ?? Data()
        let fragSum = (o["frag_sum"] as? String).flatMap { $0.isEmpty || $0 == "null" ? nil : Data(base64Encoded: $0) }
        return MeshPacket(
            id: id, src: src,
            dst: (o["dst"] as? String) ?? "",
            groupId: (o["group_id"] as? String).flatMap { $0.isEmpty || $0 == "null" ? nil : $0 },
            kind: kind,
            ttl: (o["ttl"] as? NSNumber)?.intValue ?? MeshEngine.defaultMaxHops,
            hops: (o["hops"] as? NSNumber)?.intValue ?? 0,
            payload: payload, nonce: nonce,
            createdAt: (o["created_at"] as? NSNumber)?.int64Value ?? Int64(Date().timeIntervalSince1970 * 1000),
            seq: (o["seq"] as? NSNumber)?.int64Value ?? 0,
            version: version,
            hopSrc: (o["hop_src"] as? String).flatMap { $0.isEmpty || $0 == "null" ? nil : $0 },
            ackFor: (o["ack_for"] as? String).flatMap { $0.isEmpty || $0 == "null" ? nil : $0 },
            xfer: (o["xfer"] as? String).flatMap { $0.isEmpty || $0 == "null" ? nil : $0 },
            fragIndex: fragIndex, fragTotal: fragTotal,
            fragId: fragId, fragSum: fragSum
        )
    }
}

extension MeshRevocationNotice {
    /// Signs a revocation notice with the device's Ed25519 identity key.
    static func sign(identity: MeshIdentity, revoker: String, deviceId: String, publicKey: Data) -> MeshRevocationNotice? {
        guard !revoker.isEmpty, !deviceId.isEmpty, publicKey.count == 32 else { return nil }
        let r = MeshRevocationNotice(revoker: revoker, deviceId: deviceId, publicKey: publicKey,
                                     issuedAt: Int64(Date().timeIntervalSince1970 * 1000), signature: Data())
        guard let sig = identity.signBytes(r.payload()) else { return nil }
        return MeshRevocationNotice(revoker: revoker, deviceId: deviceId, publicKey: publicKey,
                                    issuedAt: r.issuedAt, signature: sig)
    }
}

/// Authenticated encryption shared by Go, Android and iOS mesh clients.
enum MeshCrypto {

    static func b64(_ d: Data) -> String { d.base64EncodedString() }

    static func unB64(_ s: String) -> Data { Data(base64Encoded: s) ?? Data() }

    /// Verifies an Ed25519 signature over data (revocation notices).
    static func verifyEd25519(pub: Data, data: Data, sig: Data) -> Bool {
        guard let key = try? Curve25519.Signing.PublicKey(rawRepresentation: pub) else { return false }
        return key.isValidSignature(sig, for: data)
    }

    static func randomNonce() -> Data {
        var bytes = [UInt8](repeating: 0, count: 12)
        _ = SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes)
        return Data(bytes)
    }

    static func randomBytes(_ count: Int) -> Data {
        var bytes = [UInt8](repeating: 0, count: count)
        _ = SecRandomCopyBytes(kSecRandomDefault, count, &bytes)
        return Data(bytes)
    }

    static func randomId() -> String {
        var bytes = [UInt8](repeating: 0, count: 16)
        _ = SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes)
        return bytes.map { String(format: "%02x", $0) }.joined()
    }

    static func encrypt(key: SymmetricKey, plaintext: Data, nonce: Data) -> Data {
        guard let n = try? AES.GCM.Nonce(data: nonce),
              let sealed = try? AES.GCM.seal(plaintext, using: key, nonce: n) else { return Data() }
        return sealed.ciphertext + sealed.tag
    }

    /// Returns the plaintext, or nil when the key is wrong or data was tampered.
    static func decrypt(key: SymmetricKey, ciphertext: Data, nonce: Data) -> Data? {
        guard let box = try? AES.GCM.SealedBox(combined: nonce + ciphertext) else { return nil }
        return try? AES.GCM.open(box, using: key)
    }
}
