import Foundation

/// MeshRelayLimit.swift — per-source relay quotas (abuse prevention).
///
/// Mirrors services/mesh/relaylimit.go. A relay forwards other devices' traffic.
/// Without a per-source cap, one flooded device can monopolise the relay's radio
/// and queue. Token-bucket limiter keyed by source device id.

final class RelayLimiter {
    struct Quota { let everyMs: Int64; let burst: Int }
    static func defaultQuota() -> Quota { Quota(everyMs: 20, burst: 200) }

    private let quota: Quota
    private let lock = NSLock()
    private var buckets: [String: (tokens: Double, last: Int64)] = [:]

    init(quota: Quota = RelayLimiter.defaultQuota()) {
        self.quota = quota
    }

    func allow(src: String, nowMs: Int64) -> Bool {
        lock.lock(); defer { lock.unlock() }
        var b = buckets[src]
        if b == nil {
            b = (Double(quota.burst), nowMs)
        }
        let elapsed = Double(nowMs - b!.last)
        var tokens = b!.tokens + elapsed / Double(quota.everyMs)
        if tokens > Double(quota.burst) { tokens = Double(quota.burst) }
        if tokens < 1.0 { return false }
        tokens -= 1.0
        buckets[src] = (tokens, nowMs)
        return true
    }

    func prune(nowMs: Int64, maxIdleMs: Int64) {
        lock.lock(); defer { lock.unlock() }
        buckets = buckets.filter { nowMs - $0.value.last <= maxIdleMs }
    }

    var size: Int { lock.lock(); defer { lock.unlock() }; return buckets.count }
}