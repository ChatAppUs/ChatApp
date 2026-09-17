import Foundation

/// MeshCongestion.swift — byte-based radio backpressure for the offline mesh.
///
/// Mirrors services/mesh/congestion.go. Token bucket metering byte throughput;
/// excess packets are buffered in the priority queue, which sheds lowest class first.

final class CongestionController {
    private static let defaultRate = 256 * 1024
    private static let defaultBurst = 512 * 1024

    private let lock = NSLock()
    private let rate: Double
    private let burst: Double
    private var tokens: Double
    private var last: Int64

    private(set) var admitted: UInt64 = 0
    private(set) var blocked: UInt64 = 0

    init(rateBytesPerSecond: Int = defaultRate, burstBytes: Int = defaultBurst) {
        self.rate = Double(rateBytesPerSecond > 0 ? rateBytesPerSecond : Self.defaultRate)
        self.burst = Double(burstBytes > 0 ? burstBytes : Self.defaultBurst)
        self.tokens = self.burst
        self.last = Int64(Date().timeIntervalSince1970 * 1000)
    }

    func allow(bytes: Int, nowMs: Int64) -> Bool {
        lock.lock(); defer { lock.unlock() }
        guard bytes > 0 else { return false }
        var now = nowMs
        if now < last { now = last }
        tokens += Double(now - last) / 1000.0 * rate
        if tokens > burst { tokens = burst }
        last = now
        if Double(bytes) > tokens { blocked += 1; return false }
        tokens -= Double(bytes)
        admitted += 1
        return true
    }

    func status() -> [String: Any] {
        lock.lock(); defer { lock.unlock() }
        return [
            "rate_bytes_per_second": Int(rate),
            "burst_bytes": Int(burst),
            "admitted_packets": admitted,
            "blocked_packets": blocked,
            "available_bytes": Int(tokens),
        ]
    }
}