import Foundation
import CommonCrypto
import Security

// MeshEngine.swift — native iOS mesh runtime.
//
// Mirrors services/mesh and the Android Engine.kt byte-for-byte so every
// client behaves identically: same envelope (JSON, version=1), same
// TTL/dedup/ack semantics, same store-and-forward queue, same transport
// chain (AWDL → local Wi‑Fi → Bluetooth).
//
// Identity and crypto (MeshIdentity.swift): Ed25519-signed beacons + X25519
// key-agreement advertisement. Unicast payloads are sealed with a per-peer
// AES‑256 session key via ECDH + HKDF. TOFU pinning, local + network‑wide
// revocation via signed flooded notices, per-source sequence numbers against
// a sliding replay window.
//
// Delivery guarantees: reliable end‑to‑end transfers with transfer ids,
// bounded exponential retries and user-visible delivery state; MTU-bounded
// encrypted fragmentation + SHA‑256 reassembly; priority forwarding buffer
// (control first, media droppable); per-group sender keys with per-member
// acknowledgements.

// MARK: - Packet & Neighbour types --------------------------------------------------

struct MeshPacket: Codable {
    let id: String
    let src: String
    let dst: String
    let groupId: String?
    let kind: String
    var ttl: Int
    var hops: Int = 0
    let payload: Data
    let nonce: Data
    let createdAt: UInt64
    let seq: UInt64
    let version: Int
    var hopSrc: String?
    let ackFor: String?
    let xfer: String?
    let fragIndex: Int
    let fragTotal: Int
    let fragId: String?
    let fragSum: Data?

    enum CodingKeys: String, CodingKey {
        case id = "id"
        case src = "src"
        case dst = "dst"
        case groupId = "group_id"
        case kind = "kind"
        case ttl = "ttl"
        case hops = "hops"
        case payload = "payload"
        case nonce = "nonce"
        case createdAt = "created_at"
        case seq = "seq"
        case version = "v"
        case hopSrc = "hop_src"
        case ackFor = "ack_for"
        case xfer = "xfer"
        case fragIndex = "frag_index"
        case fragTotal = "frag_total"
        case fragId = "frag_id"
        case fragSum = "frag_sum"
    }
}

struct MeshNeighbor {
    let deviceId: String
    let addr: String
    let transport: String
    let relayOk: Bool
    let lastSeen: UInt64
}

// MARK: - Engine --------------------------------------------------------------------

final class MeshEngine {
    let deviceId: String
    private let key: Data
    private let maxHops: Int
    private let maxQueue: Int
    private let maxAgeMs: UInt64

    private var neighbors: [String: MeshNeighbor] = [:]
    private var seen: [String: UInt64] = [:]
    private let pfifo = MeshPriorityQueue()
    private let neighborLock = NSLock()
    private let seenLock = NSLock()

    let identity = MeshIdentity()
    private var pinnedKeys: [String: Data] = [:]
    private var revokedKeys: [String: Data] = [:]
    private var peerKEM: [String: (key: Data, epoch: UInt64)] = [:]

    let groupKeys = GroupKeyManager()
    let transfers = TransferTracker()
    let groupAcks = GroupAckTracker()
    private let frags = FragmentAssembler()
    private let revStore = RevocationStore()

    // ---- Seven modules mirroring Go node.go wiring ----------------------------
    let relayLimiter = RelayLimiter()
    let congestion = CongestionController()
    let repairSM = RepairStateMachine()
    let linkFeedback = LinkFeedback()
    let multipath = MultipathSelector()
    let powerManager = PowerManager()
    func scaledMaxHops() -> Int { MeshScale.hopsForDevices(neighbors.count) }

    private var groupMembers: [String: [String]] = [:]
    private let replay = ReplayFilter()
    private var seqCtr: UInt64 = 0
    private let seqLock = NSLock()

    private var links: [MeshLink] = []
    private let linksLock = NSLock()

    private(set) var activeTransport: String = "none"
    var relayOk: Bool = true

    var delivered: ((MeshPacket, Data?) -> Void)?

    init(deviceId: String, key: Data, maxHops: Int = MeshEngine.defaultMaxHops,
         maxQueue: Int = 1000, maxAgeMs: UInt64 = 7 * 24 * 60 * 60 * 1000) {
        self.deviceId = deviceId
        self.key = key
        self.maxHops = maxHops
        self.maxQueue = maxQueue
        self.maxAgeMs = maxAgeMs
    }

    // MARK: Transport chain ---------------------------------------------------------

    func attach(_ transportChain: MeshLink...) {
        linksLock.lock(); defer { linksLock.unlock() }
        links = transportChain
        for link in links where link.isAvailable() {
            link.start()
            if activeTransport == "none" { activeTransport = link.kind }
        }
    }

    func refreshTransport() {
        linksLock.lock(); defer { linksLock.unlock() }
        let usable = links.filter { $0.isAvailable() }
        activeTransport = usable.first?.kind ?? "none"
    }

    func stop() {
        linksLock.lock(); defer { linksLock.unlock() }
        for link in links { link.stop() }
        activeTransport = "none"
    }

    // MARK: Discovery ---------------------------------------------------------------

    func upsertNeighbor(deviceId: String, addr: String, transport: String,
                        relayOk: Bool = true, now: UInt64 = machAbsoluteToMilliseconds()) {
        neighborLock.lock(); defer { neighborLock.unlock() }
        pruneNeighborsLocked(now: now)
        neighbors[deviceId] = MeshNeighbor(deviceId: deviceId, addr: addr,
                                           transport: transport, relayOk: relayOk,
                                           lastSeen: now)
    }

    func neighborList() -> [MeshNeighbor] {
        neighborLock.lock(); defer { neighborLock.unlock() }
        _ = pruneNeighborsLocked(now: machAbsoluteToMilliseconds())
        return Array(neighbors.values)
    }

    private func pruneNeighborsLocked(now: UInt64) -> [String: MeshNeighbor] {
        neighbors = neighbors.filter { now - $0.value.lastSeen <= MeshEngine.neighbourTTLMs }
        return neighbors
    }

    private func relayCandidates(_ p: MeshPacket) -> [MeshNeighbor] {
        neighborLock.lock(); defer { neighborLock.unlock() }
        let now = machAbsoluteToMilliseconds()
        return neighbors.values
            .filter { now - $0.lastSeen <= MeshEngine.neighbourTTLMs }
            .sorted {
                let aTarget = $0.relayOk || $0.deviceId == p.dst
                let bTarget = $1.relayOk || $1.deviceId == p.dst
                if aTarget != bTarget { return aTarget }
                return $0.lastSeen > $1.lastSeen
            }
    }

    func beacon(seq: UInt64) -> Data {
        return identity.signBeacon(deviceId: deviceId,
                                   kind: relayOk ? "relay" : "member",
                                   transport: activeTransport,
                                   addr: links.first?.kind ?? "",
                                   seq: 0)
    }

    // MARK: Sending -----------------------------------------------------------------

    func send(kind: String, dst: String, plaintext: Data, groupId: String? = nil) -> String {
        let sk = sessionKeyFor(dst: dst)
        guard let sealed = MeshCrypto.encrypt(key: sk, plaintext: plaintext) else { return "" }
        let p = MeshPacket(
            id: newId(), src: deviceId, dst: dst, groupId: groupId, kind: kind,
            ttl: maxHops, payload: sealed.ciphertext, nonce: sealed.nonce,
            createdAt: machAbsoluteToMilliseconds(), seq: nextSeq(),
            version: MeshEngine.meshVersion, hopSrc: nil, ackFor: nil,
            xfer: nil, fragIndex: 0, fragTotal: 0, fragId: nil, fragSum: nil)
        markSeen(id: p.id, now: machAbsoluteToMilliseconds())
        enqueue(p)
        flush()
        return p.id
    }

    func sendReliable(kind: String, dst: String, plaintext: Data) -> String {
        if plaintext.count > MeshEngine.maxReliablePayload {
            return sendLarge(kind: kind, target: dst, plaintext: plaintext, group: false)
        }
        let tr = transfers.create(dst: dst, kind: kind, plaintext: plaintext)
        tick()
        return tr.id
    }

    func sendGroup(kind: String, groupId: String, plaintext: Data) -> String {
        if plaintext.count > MeshEngine.defaultMaxPayload {
            return sendLarge(kind: kind, target: groupId, plaintext: plaintext, group: true)
        }
        let gk = groupKeys.keyFor(groupId: groupId)
        guard let sealed = MeshCrypto.encrypt(key: gk.key, plaintext: plaintext) else { return "" }
        let xferId = MeshEngine.newTransferId()
        let members = groupMembers[groupId]
        if let members = members, !members.isEmpty {
            groupAcks.create(transferId: xferId, groupId: groupId, members: members)
            transfers.createForForeign(transferId: xferId, groupId: groupId, kind: kind, plaintext: plaintext)
        }
        let p = MeshPacket(
            id: newId(), src: deviceId, dst: "", groupId: groupId, kind: kind,
            ttl: maxHops, payload: sealed.ciphertext, nonce: sealed.nonce,
            createdAt: machAbsoluteToMilliseconds(), seq: nextSeq(),
            version: MeshEngine.meshVersion, hopSrc: nil, ackFor: nil,
            xfer: xferId, fragIndex: 0, fragTotal: 0, fragId: nil, fragSum: nil)
        markSeen(id: p.id, now: machAbsoluteToMilliseconds())
        enqueue(p)
        flush()
        return xferId
    }

    func sendLarge(kind: String, target: String, plaintext: Data, group: Bool) -> String {
        let parts = splitPayload(plaintext, maxChunk: MeshEngine.defaultMaxPayload)
        guard let fragId = parts.fragId else {
            return group ? sendGroup(kind: kind, groupId: target, plaintext: plaintext)
                         : send(kind: kind, dst: target, plaintext: plaintext, groupId: nil)
        }
        let encKey: Data = {
            if group { return groupKeys.keyFor(groupId: target).key }
            return sessionKeyFor(dst: target)
        }()
        for (i, chunk) in parts.chunks.enumerated() {
            guard let sealed = MeshCrypto.encrypt(key: encKey, plaintext: chunk) else { continue }
            let p = MeshPacket(
                id: newId(), src: deviceId, dst: group ? "" : target,
                groupId: group ? target : nil, kind: kind, ttl: maxHops, hops: 0,
                payload: sealed.ciphertext, nonce: sealed.nonce,
                createdAt: machAbsoluteToMilliseconds(), seq: nextSeq(),
                version: MeshEngine.meshVersion, hopSrc: nil, ackFor: nil,
                xfer: nil, fragIndex: i, fragTotal: parts.chunks.count,
                fragId: fragId, fragSum: parts.digest)
            markSeen(id: p.id, now: machAbsoluteToMilliseconds())
            enqueue(p)
        }
        flush()
        return fragId
    }

    func trackGroupMembers(groupId: String, members: [String]) {
        groupMembers[groupId] = members.filter { $0 != deviceId }
    }

    @discardableResult
    func tick(now: UInt64 = machAbsoluteToMilliseconds()) -> Int {
        var sent = 0
        for tr in transfers.tick(now: now) {
            transmit(transferId: tr.id, now: now)
            sent += 1
        }
        return sent
    }

    private func transmit(transferId: String, now: UInt64) {
        guard let tr = transfers.get(transferId: transferId), !tr.state.isTerminal else { return }
        guard let payload = transfers.payload(transferId: transferId), !payload.isEmpty else { return }
        guard let sealed = MeshCrypto.encrypt(key: sessionKeyFor(dst: tr.dst), plaintext: payload) else { return }
        let p = MeshPacket(
            id: newId(), src: deviceId, dst: tr.dst, groupId: nil, kind: tr.kind,
            ttl: maxHops, hops: 0, payload: sealed.ciphertext, nonce: sealed.nonce,
            createdAt: now, seq: nextSeq(), version: MeshEngine.meshVersion,
            hopSrc: nil, ackFor: nil, xfer: transferId, fragIndex: 0, fragTotal: 0,
            fragId: nil, fragSum: nil)
        markSeen(id: p.id, now: now)
        transfers.notePacket(transferId: transferId, packetId: p.id)
        enqueue(p)
        flush()
    }

    func enqueue(_ p: MeshPacket) { pfifo.enqueue(p, nowMs: machAbsoluteToMilliseconds()) }
    func pending(now: UInt64 = machAbsoluteToMilliseconds()) -> [MeshPacket] { pfifo.pending(now: now) }
    func dequeue(id: String) { pfifo.remove(id: id) }
    func queueSize() -> Int { pfifo.size() }

    // MARK: Receiving ---------------------------------------------------------------

    @discardableResult
    func handleInbound(addr: String, data: Data, now: UInt64 = machAbsoluteToMilliseconds()) -> MeshPacket? {
        // Revocation notice
        if let r = MeshRevocation.unmarshal(data) {
            if !r.revoker.isEmpty, !r.deviceId.isEmpty, !r.sig.isEmpty {
                handleRevocation(r, now: now)
                return nil
            }
        }
        // Signed beacon
        if let b = identity.verifySignedBeacon(data, pinnedKeys: pinnedKeys, revokedKeys: revokedKeys) {
            upsertNeighbor(deviceId: b.deviceId, addr: addr, transport: b.transport,
                           relayOk: b.kind == "relay", now: now)
            if b.kemPub.count == MeshIdentity.keySize {
                let prev = peerKEM[b.deviceId]
                if prev == nil || b.kemEpoch >= prev!.epoch {
                    peerKEM[b.deviceId] = (b.kemPub, b.kemEpoch)
                }
            }
            return nil
        }
        guard var p = MeshPacketCodec.decode(data) else { return nil }
        guard validatePacket(p) else { return nil }

        if seen[p.id] != nil { return nil }
        markSeen(id: p.id, now: now)
        if seen.count > MeshEngine.maxSeen { pruneSeen() }

        if p.dst == deviceId || (p.dst.isEmpty && p.groupId != nil) {
            dequeue(id: p.id)
            deliverLocal(p, now: now)
            return p
        }

        guard p.ttl > 0 else { return nil }
        p.ttl -= 1
        p.hops += 1
        p.hopSrc = deviceId
        enqueue(p)
        flush()
        return p
    }

    private func validatePacket(_ p: MeshPacket) -> Bool {
        guard !p.id.isEmpty, p.id.count <= 128, !p.src.isEmpty, p.src.count <= 256 else { return false }
        guard p.dst.count <= 256, !(p.dst.isEmpty && p.groupId == nil) else { return false }
        guard (p.groupId?.count ?? 0) <= 256, (0...1024).contains(p.ttl), (0...1024).contains(p.hops) else { return false }
        if !p.payload.isEmpty, p.nonce.count != 12 { return false }
        if p.fragTotal > 1 {
            guard let fid = p.fragId, !fid.isEmpty,
                  (0..<p.fragTotal).contains(p.fragIndex),
                  p.fragSum?.count == 32 else { return false }
        }
        if p.fragTotal <= 1, p.fragIndex != 0 || p.fragTotal < 0 { return false }
        if p.kind == MeshEngine.kindAck, (p.ackFor == nil || p.dst.isEmpty) { return false }
        return true
    }

    private func deliverLocal(_ p: MeshPacket, now: UInt64) {
        if p.kind == MeshEngine.kindAck {
            guard let proof = decryptPayload(p) else { return }
            let proofStr = String(data: proof, encoding: .utf8) ?? ""
            if p.groupId != nil {
                let expected = "chatapp-mesh-groupack-v1:\(p.groupId!):\(p.ackFor ?? "")"
                guard proofStr == expected else { return }
                if p.seq != 0, !replay.check(src: p.src, seq: p.seq) { return }
                if let ackFor = p.ackFor {
                    groupAcks.ack(transferId: ackFor, memberId: p.src)
                    transfers.ack(transferId: ackFor)
                }
                return
            }
            let expected = "chatapp-mesh-ack-v1:\(p.ackFor ?? "")"
            guard proofStr == expected else { return }
            if p.seq != 0, !replay.check(src: p.src, seq: p.seq) { return }
            if let ackFor = p.ackFor { transfers.ack(transferId: ackFor) }
            return
        }

        guard let pt = decryptPayload(p) else { return }
        if p.seq != 0, !replay.check(src: p.src, seq: p.seq) { return }

        guard let full = frags.add(packet: p, payload: pt) else { return }

        if let xferId = p.xfer, !xferId.isEmpty {
            if transfers.markDelivered(transferId: xferId) {
                delivered?(p, full)
            }
            if p.groupId != nil {
                sendGroupAck(dst: p.src, groupId: p.groupId!, transferId: xferId)
            } else {
                sendAck(dst: p.src, transferId: xferId)
            }
            return
        }
        delivered?(p, full)
    }

    private func sendAck(dst: String, transferId: String) {
        guard !dst.isEmpty, !transferId.isEmpty else { return }
        let proof = "chatapp-mesh-ack-v1:\(transferId)".data(using: .utf8)!
        guard let sealed = MeshCrypto.encrypt(key: sessionKeyFor(dst: dst), plaintext: proof) else { return }
        let p = MeshPacket(
            id: newId(), src: deviceId, dst: dst, groupId: nil,
            kind: MeshEngine.kindAck, ttl: maxHops,
            payload: sealed.ciphertext, nonce: sealed.nonce,
            createdAt: machAbsoluteToMilliseconds(), seq: nextSeq(),
            version: MeshEngine.meshVersion, hopSrc: nil,
            ackFor: transferId, xfer: transferId,
            fragIndex: 0, fragTotal: 0, fragId: nil, fragSum: nil)
        markSeen(id: p.id, now: machAbsoluteToMilliseconds())
        enqueue(p)
        flush()
    }

    private func sendGroupAck(dst: String, groupId: String, transferId: String) {
        guard !dst.isEmpty, !groupId.isEmpty, !transferId.isEmpty else { return }
        let proof = "chatapp-mesh-groupack-v1:\(groupId):\(transferId)".data(using: .utf8)!
        guard let sealed = MeshCrypto.encrypt(key: sessionKeyFor(dst: dst), plaintext: proof) else { return }
        let p = MeshPacket(
            id: newId(), src: deviceId, dst: dst, groupId: groupId,
            kind: MeshEngine.kindAck, ttl: maxHops,
            payload: sealed.ciphertext, nonce: sealed.nonce,
            createdAt: machAbsoluteToMilliseconds(), seq: nextSeq(),
            version: MeshEngine.meshVersion, hopSrc: nil,
            ackFor: transferId, xfer: transferId,
            fragIndex: 0, fragTotal: 0, fragId: nil, fragSum: nil)
        markSeen(id: p.id, now: machAbsoluteToMilliseconds())
        enqueue(p)
        flush()
    }

    @discardableResult
    func flush() -> Int {
        var sent = 0
        linksLock.lock(); defer { linksLock.unlock() }
        guard let target = links.first(where: { $0.isAvailable() }) else { return 0 }
        for p in pending() {
            for nb in relayCandidates(p) {
                if !relayOk && nb.deviceId != p.dst { continue }
                guard let wire = MeshPacketCodec.encode(p) else { continue }
                if target.send(addr: nb.addr, data: wire) {
                    dequeue(id: p.id)
                    sent += 1
                    break
                }
            }
        }
        return sent
    }

    // MARK: Revocation --------------------------------------------------------------

    func revokePeer(deviceId: String, expectedPub: Data) -> Data? {
        guard let pinned = pinnedKeys[deviceId], pinned == expectedPub else { return nil }
        let r = MeshRevocation.sign(identity: identity, revoker: self.deviceId,
                                    deviceId: deviceId, publicKey: expectedPub,
                                    timestamp: machAbsoluteToMilliseconds())
        applyRevocation(r)
        let data = MeshRevocation.marshal(r)
        for nb in neighbors.values where nb.relayOk {
            let flood = MeshPacket(
                id: newId(), src: self.deviceId, dst: nb.deviceId,
                groupId: nil, kind: MeshEngine.kindMessage, ttl: maxHops,
                payload: data, nonce: Data(), createdAt: machAbsoluteToMilliseconds(),
                seq: nextSeq(), version: MeshEngine.meshVersion, hopSrc: nil,
                ackFor: nil, xfer: nil, fragIndex: 0, fragTotal: 0, fragId: nil, fragSum: nil)
            enqueue(flood)
        }
        flush()
        return data
    }

    private func handleRevocation(_ r: RevocationNotice, now: UInt64) {
        guard let verified = MeshRevocation.verify(r, pinnedKeys: pinnedKeys) else { return }
        guard revStore.apply(verified) else { return }
        applyRevocation(verified)
        let data = MeshRevocation.marshal(verified)
        for nb in neighbors.values where nb.relayOk && nb.deviceId != verified.revoker {
            let flood = MeshPacket(
                id: newId(), src: deviceId, dst: nb.deviceId,
                groupId: nil, kind: MeshEngine.kindMessage, ttl: maxHops,
                payload: data, nonce: Data(), createdAt: now,
                seq: nextSeq(), version: MeshEngine.meshVersion, hopSrc: nil,
                ackFor: nil, xfer: nil, fragIndex: 0, fragTotal: 0, fragId: nil, fragSum: nil)
            enqueue(flood)
        }
        flush()
    }

    private func applyRevocation(_ r: RevocationNotice) {
        revokedKeys[r.deviceId] = r.publicKey
        peerKEM.removeValue(forKey: r.deviceId)
        neighborLock.lock(); defer { neighborLock.unlock() }
        neighbors.removeValue(forKey: r.deviceId)
        transfers.dropTo(dst: r.deviceId)
    }

    func isPeerRevoked(deviceId: String) -> Bool { revokedKeys[deviceId] != nil }

    // MARK: Group keys --------------------------------------------------------------

    func adoptGroupKey(groupId: String, key: Data, epoch: UInt64) -> Bool {
        return groupKeys.adoptKey(groupId: groupId, key: key, epoch: epoch)
    }

    func rotateGroupKey(groupId: String) -> GroupKey { groupKeys.rotateGroup(groupId: groupId) }

    // MARK: Identity / session-key helpers ------------------------------------------

    private func sessionKeyFor(dst: String) -> Data {
        guard !dst.isEmpty else { return key }
        guard let adv = peerKEM[dst] else { return key }
        return identity.sessionKey(myId: deviceId, theirPub: adv.key, theirId: dst) ?? key
    }

    private func decryptPayload(_ p: MeshPacket) -> Data? {
        if let gid = p.groupId, p.dst.isEmpty {
            let gk = groupKeys.keyFor(groupId: gid)
            if let pt = MeshCrypto.decrypt(key: gk.key, ciphertext: p.payload, nonce: p.nonce) { return pt }
        }
        if p.dst == deviceId, let adv = peerKEM[p.src] {
            if let sk = identity.sessionKey(myId: deviceId, theirPub: adv.key, theirId: p.src) {
                if let pt = MeshCrypto.decrypt(key: sk, ciphertext: p.payload, nonce: p.nonce) { return pt }
            }
        }
        return MeshCrypto.decrypt(key: key, ciphertext: p.payload, nonce: p.nonce)
    }

    func rotateSessions() { identity.rotate() }

    // MARK: Helpers -----------------------------------------------------------------

    private func nextSeq() -> UInt64 {
        seqLock.lock(); defer { seqLock.unlock() }
        seqCtr += 1; return seqCtr
    }

    private func markSeen(id: String, now: UInt64) {
        seenLock.lock(); defer { seenLock.unlock() }
        seen[id] = now
    }

    private func pruneSeen() {
        seenLock.lock(); defer { seenLock.unlock() }
        let cutoff = machAbsoluteToMilliseconds() - maxAgeMs
        seen = seen.filter { $0.value >= cutoff }
    }

    private func newId() -> String {
        var bytes = [UInt8](repeating: 0, count: 16)
        SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes)
        return bytes.map { String(format: "%02x", $0) }.joined()
    }
}

// MARK: - Constants ---------------------------------------------------------------

extension MeshEngine {
    static let defaultMaxHops = 64
    static let maxSeen = 10000
    static let neighbourTTLMs: UInt64 = 3 * 60 * 1000
    static let meshVersion = 1
    static let defaultMaxPayload = 512
    static let maxReliablePayload = 64 * 1024
    static let defaultQueueMaxBytes = 8 * 1024 * 1024

    static let kindMessage = "message"
    static let kindGroupMessage = "group_message"
    static let kindVoiceMessage = "voice_message"
    static let kindCallSignal = "call_signal"
    static let kindCallMedia = "call_media"
    static let kindCallFEC = "call_fec"
    static let kindCallPing = "call_ping"
    static let kindCallPong = "call_pong"
    static let kindCallBye = "call_bye"
    static let kindAck = "ack"

    static let validKinds: Set<String> = [
        kindMessage, kindGroupMessage, kindVoiceMessage, kindCallSignal,
        kindCallMedia, kindCallFEC, kindCallPing, kindCallPong, kindCallBye, kindAck,
    ]

    static func newTransferId() -> String {
        var bytes = [UInt8](repeating: 0, count: 16)
        SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes)
        return bytes.map { String(format: "%02x", $0) }.joined()
    }
}

// MARK: - Codec -------------------------------------------------------------------

enum MeshPacketCodec {
    private static let encoder: JSONEncoder = {
        let e = JSONEncoder()
        e.outputFormatting = .sortedKeys
        return e
    }()
    private static let decoder = JSONDecoder()

    static func encode(_ p: MeshPacket) -> Data? {
        guard let json = try? encoder.encode(CodablePacket(from: p)) else { return nil }
        return json
    }

    static func decode(_ data: Data) -> MeshPacket? {
        guard let cp = try? decoder.decode(CodablePacket.self, from: data) else { return nil }
        guard cp.v <= MeshEngine.meshVersion else { return nil }
        guard MeshEngine.validKinds.contains(cp.kind) else { return nil }
        let payload = Data(base64Encoded: cp.payload) ?? Data()
        let nonce = Data(base64Encoded: cp.nonce) ?? Data()
        let fragSum = cp.frag_sum.flatMap { Data(base64Encoded: $0) }
        return MeshPacket(
            id: cp.id, src: cp.src, dst: cp.dst, groupId: cp.group_id,
            kind: cp.kind, ttl: cp.ttl, hops: cp.hops,
            payload: payload, nonce: nonce,
            createdAt: cp.created_at, seq: cp.seq,
            version: cp.v, hopSrc: cp.hop_src,
            ackFor: cp.ack_for, xfer: cp.xfer,
            fragIndex: cp.frag_index, fragTotal: cp.frag_total,
            fragId: cp.frag_id, fragSum: fragSum)
    }
}

private struct CodablePacket: Codable {
    let id: String; let v: Int; let src: String; let hop_src: String?
    let dst: String; let group_id: String?; let kind: String
    let ttl: Int; let hops: Int
    let payload: String; let nonce: String
    let created_at: UInt64; let seq: UInt64
    let frag_index: Int; let frag_total: Int
    let frag_id: String?; let frag_sum: String?
    let ack_for: String?; let xfer: String?
}

// MARK: - Crypto ------------------------------------------------------------------

enum MeshCrypto {
    struct Sealed { let ciphertext: Data; let nonce: Data }

    static func encrypt(key: Data, plaintext: Data) -> Sealed? {
        guard key.count == kCCKeySizeAES256 else { return nil }
        var nonce = Data(count: 12)
        _ = nonce.withUnsafeMutableBytes { SecRandomCopyBytes(kSecRandomDefault, 12, $0.baseAddress!) }
        var outLen = 0
        var out = Data(count: plaintext.count + 16)
        let status = key.withUnsafeBytes { keyPtr in
            nonce.withUnsafeBytes { noncePtr in
                plaintext.withUnsafeBytes { ptPtr in
                    out.withUnsafeMutableBytes { outPtr in
                        CCCrypt(CCOperation(kCCEncrypt), CCAlgorithm(kCCAlgorithmAES),
                                CCOptions(kCCModeGCM), keyPtr.baseAddress, key.count,
                                noncePtr.baseAddress, ptPtr.baseAddress, plaintext.count,
                                outPtr.baseAddress, out.count, &outLen)
                    }
                }
            }
        }
        guard status == kCCSuccess else { return nil }
        return Sealed(ciphertext: out.prefix(outLen), nonce: nonce)
    }

    static func decrypt(key: Data, ciphertext: Data, nonce: Data) -> Data? {
        guard key.count == kCCKeySizeAES256, nonce.count == 12 else { return nil }
        var outLen = 0
        var out = Data(count: ciphertext.count + 16)
        let status = key.withUnsafeBytes { keyPtr in
            nonce.withUnsafeBytes { noncePtr in
                ciphertext.withUnsafeBytes { ctPtr in
                    out.withUnsafeMutableBytes { outPtr in
                        CCCrypt(CCOperation(kCCDecrypt), CCAlgorithm(kCCAlgorithmAES),
                                CCOptions(kCCModeGCM), keyPtr.baseAddress, key.count,
                                noncePtr.baseAddress, ctPtr.baseAddress, ciphertext.count,
                                outPtr.baseAddress, out.count, &outLen)
                    }
                }
            }
        }
        guard status == kCCSuccess else { return nil }
        return out.prefix(outLen)
    }
}

// MARK: - Helpers ----------------------------------------------------------------

private func machAbsoluteToMilliseconds() -> UInt64 {
    var tb = mach_timebase_info_data_t()
    mach_timebase_info(&tb)
    let elapsed = mach_absolute_time()
    return elapsed * UInt64(tb.numer) / UInt64(tb.denom) / 1_000_000
}

func splitPayload(_ data: Data, maxChunk: Int) -> (fragId: String?, chunks: [Data], digest: Data) {
    if data.count <= maxChunk { return (nil, [data], Data()) }
    var digest = [UInt8](repeating: 0, count: Int(CC_SHA256_DIGEST_LENGTH))
    _ = data.withUnsafeBytes { CC_SHA256($0.baseAddress, CC_LONG(data.count), &digest) }
    let fragId = MeshEngine.newTransferId()
    let chunks = stride(from: 0, to: data.count, by: maxChunk).map { data.subdata(in: $0..<min($0 + maxChunk, data.count)) }
    return (fragId, chunks, Data(digest))
}