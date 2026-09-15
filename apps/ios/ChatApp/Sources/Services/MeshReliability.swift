import Foundation

// MeshReliability.swift — traffic classes, the priority-ordered forwarding
// buffer, and the reliable-delivery state machine.
//
// Mirrors services/mesh (priority.go, pfifo.go, reliability.go) and the
// Android MeshReliability.kt. A relay carries other devices' traffic: with one
// undifferentiated FIFO, a burst of media starves control traffic, so the
// buffer is priority-ordered and byte-bounded. Reliable delivery adds
// end-to-end acknowledgements, bounded exponential retries, and a user-visible
// delivery state including a dead-letter state.

/// Traffic classes for the forwarding buffer; lower drains first.
enum MeshPriority: Int, CaseIterable {
    case control = 0 // acks and call signalling: drained first, dropped last
    case text = 1    // chat and group messages
    case voice = 2   // voice notes
    case media = 3   // bulk media: dropped first under pressure

    /// Maps a packet kind to its class; unknown kinds are media.
    static func forKind(_ kind: String) -> MeshPriority {
        switch kind {
        case MeshEngine.kindAck, MeshEngine.kindCallSignal: return .control
        case MeshEngine.kindMessage, MeshEngine.kindGroupMessage: return .text
        case MeshEngine.kindVoiceMessage: return .voice
        default: return .media
        }
    }
}

/// User-visible state of a reliable mesh transfer (see reliability.go).
enum TransferState: String, CaseIterable {
    case queued       // created, awaiting its first send
    case relaying     // a transmission is in flight, awaiting an ack
    case acked        // the destination acknowledged the transfer
    case expired      // the transfer outlived its lifetime unacked
    case deadLetter = "dead_letter" // the retry budget was exhausted

    var isTerminal: Bool {
        self == .acked || self == .expired || self == .deadLetter
    }
}

/// One reliable delivery and its state.
final class Transfer {
    let id: String
    let dst: String
    let kind: String
    var state: TransferState = .queued
    var attempts = 0
    /// Packet id of the most recent transmission.
    var packetId = ""
    /// Neighbour the most recent attempt was handed to (alternate-path retry).
    var lastHop = ""
    var lastError = ""
    let createdAt: Int64
    var nextAttempt: Int64
    let expiresAt: Int64

    init(id: String, dst: String, kind: String, createdAt: Int64, nextAttempt: Int64, expiresAt: Int64) {
        self.id = id
        self.dst = dst
        self.kind = kind
        self.createdAt = createdAt
        self.nextAttempt = nextAttempt
        self.expiresAt = expiresAt
    }
}

/**
 * Tracks the sender-side reliable-delivery state machine and the receiver-side
 * delivered-transfer set, so retries collapse to exactly-once application
 * delivery while every copy is still acknowledged.
 */
final class TransferTracker {
    private let ackTimeoutMs: Int64
    private let maxAttempts: Int
    private let ttlMs: Int64

    private let lock = NSLock()
    private var active: [String: Transfer] = [:]
    private var payloads: [String: Data] = [:]
    private var delivered: [String: Int64] = [:]

    init(ackTimeoutMs: Int64 = 6_000, maxAttempts: Int = 5, ttlMs: Int64 = 30 * 60 * 1000) {
        self.ackTimeoutMs = ackTimeoutMs > 0 ? ackTimeoutMs : 6_000
        self.maxAttempts = maxAttempts > 0 ? maxAttempts : 5
        self.ttlMs = ttlMs > 0 ? ttlMs : 30 * 60 * 1000
    }

    private func nowMs() -> Int64 { Int64(Date().timeIntervalSince1970 * 1000) }

    /// Registers a transfer and returns it (id assigned).
    func create(dst: String, kind: String, payload: Data) -> Transfer {
        let now = nowMs()
        let tr = Transfer(
            id: MeshCrypto.randomId(), dst: dst, kind: kind,
            createdAt: now, nextAttempt: now, expiresAt: now + ttlMs)
        lock.lock()
        active[tr.id] = tr
        payloads[tr.id] = payload
        evictLocked()
        lock.unlock()
        return tr
    }

    /// Registers a transfer whose id was chosen by the caller (group sends).
    func createForForeign(id: String, groupId: String, kind: String, payload: Data) -> Transfer {
        let now = nowMs()
        let tr = Transfer(
            id: id, dst: groupId, kind: kind,
            createdAt: now, nextAttempt: now, expiresAt: now + ttlMs)
        lock.lock()
        active[id] = tr
        payloads[id] = payload
        evictLocked()
        lock.unlock()
        return tr
    }

    private func evictLocked() {
        while active.count > 4096 {
            guard let victim = active.keys.first else { break }
            active.removeValue(forKey: victim)
            payloads.removeValue(forKey: victim)
        }
    }

    func get(_ id: String) -> Transfer? {
        lock.lock(); defer { lock.unlock() }
        return active[id]
    }

    func payload(_ id: String) -> Data? {
        lock.lock(); defer { lock.unlock() }
        return payloads[id]
    }

    /// Marks a transfer acknowledged; reports whether this is the first ack.
    func ack(_ id: String) -> Bool {
        lock.lock(); defer { lock.unlock() }
        guard let tr = active[id], tr.state != .acked else { return false }
        tr.state = .acked
        tr.nextAttempt = Int64.max
        payloads.removeValue(forKey: id)
        return true
    }

    /**
     * Advances the sender-side state machine and returns the transfers that
     * must be (re)transmitted now. Terminal states: expired (past its
     * lifetime) and dead letter (retry budget exhausted).
     */
    func tick(_ nowMs: Int64? = nil) -> [Transfer] {
        let now = nowMs ?? self.nowMs()
        var due: [Transfer] = []
        lock.lock()
        for tr in active.values {
            if tr.state.isTerminal { continue }
            if now > tr.expiresAt {
                tr.state = .expired
                tr.nextAttempt = Int64.max
                payloads.removeValue(forKey: tr.id)
                continue
            }
            if now < tr.nextAttempt { continue }
            if tr.attempts >= maxAttempts {
                tr.state = .deadLetter
                tr.nextAttempt = Int64.max
                payloads.removeValue(forKey: tr.id)
                continue
            }
            tr.attempts += 1
            tr.state = .relaying
            var backoff = ackTimeoutMs
            var i = 1
            while i < tr.attempts {
                backoff *= 2
                if backoff >= 60_000 { backoff = 60_000; break }
                i += 1
            }
            tr.nextAttempt = now + backoff
            due.append(tr)
        }
        lock.unlock()
        return due
    }

    /// Records the packet id of the most recent transmission.
    func notePacket(_ id: String, packetId: String) {
        lock.lock(); defer { lock.unlock() }
        active[id]?.packetId = packetId
    }

    /// Records which neighbour an attempt was handed to.
    func noteHop(_ id: String, hop: String, packetId: String) {
        lock.lock(); defer { lock.unlock() }
        active[id]?.lastHop = hop
        active[id]?.packetId = packetId
    }

    /**
     * Applies receiver-side exactly-once semantics: true the first time a
     * transfer id is seen, false for every duplicate copy.
     */
    func markDelivered(_ id: String) -> Bool {
        let now = nowMs()
        lock.lock(); defer { lock.unlock() }
        if delivered[id] != nil { return false }
        delivered[id] = now
        if delivered.count > 8192 {
            let victims = delivered.sorted { $0.value < $1.value }.prefix(delivered.count / 2)
            for (k, _) in victims { delivered.removeValue(forKey: k) }
        }
        return true
    }

    /// Drops transfers addressed to a revoked device (route withdrawal).
    func dropTo(_ dst: String) {
        lock.lock(); defer { lock.unlock() }
        let victims = active.values.filter { $0.dst == dst }.map { $0.id }
        for v in victims {
            active.removeValue(forKey: v)
            payloads.removeValue(forKey: v)
        }
    }

    /// How many transfers sit in each state — the aggregate status surface.
    func counts() -> [String: Int] {
        lock.lock(); defer { lock.unlock() }
        var out: [String: Int] = [:]
        for s in TransferState.allCases { out[s.rawValue] = 0 }
        for tr in active.values { out[tr.state.rawValue, default: 0] += 1 }
        return out
    }
}

/// One buffered packet with its class and enqueue order.
private struct QItem {
    let packet: MeshPacket
    let priority: MeshPriority
    let seq: Int64
    var bytes: Int { packet.payload.count + packet.nonce.count }
}

/**
 * Bounded, priority-ordered forwarding buffer: drain order is by traffic class
 * then FIFO within a class; capacity is bounded by bytes and count; under
 * pressure the lowest-priority class is dropped first and a control packet is
 * never dropped to make room.
 */
final class MeshPriorityQueue {
    private let maxPackets: Int
    private let maxBytes: Int
    private let maxAgeMs: Int64

    private let lock = NSLock()
    private var items: [QItem] = []
    private var seq: Int64 = 0
    private var bytes = 0

    init(maxPackets: Int = 4096, maxBytes: Int = 8 * 1024 * 1024, maxAgeMs: Int64 = 7 * 24 * 60 * 60 * 1000) {
        self.maxPackets = maxPackets
        self.maxBytes = maxBytes
        self.maxAgeMs = maxAgeMs
    }

    /** Admits a packet; reports false when the buffer refuses it. */
    func enqueue(_ p: MeshPacket, nowMs: Int64) -> Bool {
        let pri = MeshPriority.forKind(p.kind)
        let itemBytes = p.payload.count + p.nonce.count
        lock.lock(); defer { lock.unlock() }
        if items.count >= maxPackets || bytes + itemBytes > maxBytes {
            // Drop the lowest-priority droppable packet; control is never
            // dropped, and a control packet may displace any lower class.
            guard let victim = items.filter({ $0.priority != .control }).max(by: { $0.priority.rawValue < $1.priority.rawValue }),
                  victim.priority.rawValue >= pri.rawValue else { return false }
            items.removeAll { $0.packet.id == victim.packet.id }
            bytes -= victim.bytes
        }
        seq += 1
        items.append(QItem(packet: p, priority: pri, seq: seq))
        bytes += itemBytes
        return true
    }

    /// Valid packets in drain order; expired or out-of-TTL packets are removed.
    func pending(_ nowMs: Int64) -> [MeshPacket] {
        lock.lock(); defer { lock.unlock() }
        var alive: [QItem] = []
        for q in items {
            if nowMs - q.packet.createdAt > maxAgeMs || q.packet.ttl <= 0 {
                bytes -= q.bytes
            } else {
                alive.append(q)
            }
        }
        items = alive
        alive.sort { ($0.priority.rawValue, $0.seq) < ($1.priority.rawValue, $1.seq) }
        return alive.map { $0.packet }
    }

    func remove(_ id: String) {
        lock.lock(); defer { lock.unlock() }
        for (i, q) in items.enumerated() where q.packet.id == id {
            bytes -= q.bytes
            items.remove(at: i)
            break
        }
    }

    func size() -> Int {
        lock.lock(); defer { lock.unlock() }
        return items.count
    }
}
