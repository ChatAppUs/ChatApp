import Foundation

/// MeshLinkFeedback.swift — link quality metrics for the offline mesh.
///
/// Mirrors services/mesh/linkfeedback.go. EWMA-smoothed RTT, loss rate,
/// and throughput estimate per neighbour.

struct LinkScores {
    var rttMs: Double = 0
    var lossRate: Double = 0
    var throughputBps: Double = 0
    var score: Double { max(0, 100 - max(0, rttMs - 10) * 0.5 - lossRate * 100) }
}

struct LinkSample {
    let sentAt: Date
    var ackedAt: Date?
    var size: Int = 0
}

final class LinkFeedback {
    private let windowDuration: TimeInterval
    private let alpha: Double = 0.25
    private let lock = NSLock()
    private var scores: [String: LinkScores] = [:]
    private var window: [String: [LinkSample]] = [:]

    init(windowDuration: TimeInterval = 30) {
        self.windowDuration = windowDuration
    }

    func recordSent(dst: String, sampleId: String, size: Int) {
        lock.lock(); defer { lock.unlock() }
        window[dst, default: []].append(LinkSample(sentAt: Date(), size: size))
    }

    func recordAck(dst: String, sampleId: String, ackSize: Int = 0) {
        lock.lock(); defer { lock.unlock() }
        guard var samples = window[dst] else { return }
        let now = Date()
        let cutoff = now.addingTimeInterval(-windowDuration)
        for i in (0..<samples.count).reversed() where samples[i].ackedAt == nil {
            samples[i].ackedAt = now
            break
        }
        window[dst] = samples.filter { $0.sentAt >= cutoff || $0.ackedAt == nil }
        recompute(dst: dst, now: now)
    }

    func prune(now: Date = Date()) {
        lock.lock(); defer { lock.unlock() }
        let cutoff = now.addingTimeInterval(-windowDuration)
        for dst in window.keys {
            window[dst] = window[dst]?.filter { $0.sentAt >= cutoff || $0.ackedAt == nil }
            if window[dst]?.isEmpty == true { window.removeValue(forKey: dst) }
        }
    }

    func get(dst: String) -> LinkScores? {
        lock.lock(); defer { lock.unlock() }
        return scores[dst]
    }

    var all: [String: LinkScores] {
        lock.lock(); defer { lock.unlock() }
        return scores
    }

    private func recompute(dst: String, now: Date) {
        guard let samples = window[dst], !samples.isEmpty else { return }
        let acked = samples.compactMap { s -> (Double, Int)? in
            guard let ack = s.ackedAt else { return nil }
            return (ack.timeIntervalSince(s.sentAt) * 1000, s.size)
        }
        let lost = Double(samples.filter { $0.sentAt < now.addingTimeInterval(-windowDuration) && $0.ackedAt == nil }.count)
        let total = Double(samples.count)
        let rtt = acked.isEmpty ? 0 : acked.map(\.0).reduce(0, +) / Double(acked.count)
        let loss = total > 0 ? lost / total : 0
        let bps: Double = acked.isEmpty ? 0 : Double(acked.map(\.1).reduce(0, +)) / max(windowDuration, 1)
        var s = scores[dst] ?? LinkScores()
        s.rttMs = alpha * rtt + (1 - alpha) * s.rttMs
        s.lossRate = alpha * loss + (1 - alpha) * s.lossRate
        s.throughputBps = alpha * bps + (1 - alpha) * s.throughputBps
        scores[dst] = s
    }
}