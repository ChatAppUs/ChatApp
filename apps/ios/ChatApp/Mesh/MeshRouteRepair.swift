import Foundation

/// MeshRouteRepair.swift — proactive route repair for the mesh.
///
/// Mirrors services/mesh/routerepair.go and routerepair_states.go. When a
/// neighbour fails, the node immediately re-selects the best remaining relay
/// and re-queues the packet so one dead link does not stall a transfer.

enum RepairState: String, CaseIterable {
    case idle, probing, active, cooldown
}

struct RepairEntry {
    let dest: String
    var state: RepairState = .idle
    var fails = 0
    var attempts = 0
    var lastProbe: Int64 = 0
    var lastFail: Int64 = 0
    var activeVia = ""
    var enteredAt: Int64 = 0
}

final class RepairStateMachine {
    private let maxProbes: Int
    private let probeBackoffMs: Int
    private let cooldownMs: Int

    private let lock = NSLock()
    private var entries: [String: RepairEntry] = [:]

    init(maxProbes: Int = 5, probeBackoffMs: Int = 200, cooldownMs: Int = 30_000) {
        self.maxProbes = maxProbes
        self.probeBackoffMs = probeBackoffMs
        self.cooldownMs = cooldownMs
    }

    func onLinkFailure(dest: String, failedNb: String) -> RepairState {
        lock.lock(); defer { lock.unlock() }
        var entry = entries[dest] ?? RepairEntry(dest: dest)
        let now = Int64(Date().timeIntervalSince1970 * 1000)
        switch entry.state {
        case .idle:
            entry.state = .probing; entry.attempts = 1
            entry.lastProbe = now; entry.lastFail = now; entry.enteredAt = now
        case .probing:
            if entry.attempts >= maxProbes {
                entry.state = .cooldown; entry.enteredAt = now
            } else {
                let backoff = Int64(probeBackoffMs * entry.attempts)
                if now - entry.lastProbe >= backoff {
                    entry.attempts += 1; entry.lastProbe = now
                }
            }
        case .active:
            entry.state = .probing; entry.attempts = 1
            entry.lastProbe = now; entry.lastFail = now
            entry.activeVia = ""; entry.enteredAt = now
        case .cooldown: break
        }
        entry.fails += 1; entry.lastFail = now
        entries[dest] = entry
        return entry.state
    }

    func onProbeSuccess(dest: String, via: String) -> RepairState {
        lock.lock(); defer { lock.unlock() }
        guard var entry = entries[dest] else { return .idle }
        entry.state = .active; entry.activeVia = via
        entry.enteredAt = Int64(Date().timeIntervalSince1970 * 1000)
        entries[dest] = entry
        return entry.state
    }

    func onLinkRecovered(dest: String) {
        lock.lock(); defer { lock.unlock() }
        guard var entry = entries[dest] else { return }
        entry.state = .idle; entry.activeVia = ""
        entry.attempts = 0; entry.fails = 0
        entries[dest] = entry
    }

    func tick(nowMs: Int64) -> [String] {
        lock.lock(); defer { lock.unlock() }
        var recovered: [String] = []
        for (dest, var entry) in entries where entry.state == .cooldown {
            if nowMs - entry.enteredAt >= cooldownMs {
                entry.state = .idle; entry.attempts = 0
                entries[dest] = entry; recovered.append(dest)
            }
        }
        return recovered
    }

    func get(dest: String) -> RepairEntry? {
        lock.lock(); defer { lock.unlock() }
        return entries[dest]
    }

    func snapshot() -> [RepairEntry] {
        lock.lock(); defer { lock.unlock() }
        return Array(entries.values)
    }
}

enum RouteRepair {
    static func repairRoute(dst: String, failedNeighbor: String,
                            neighbors: [MeshNeighbor], maxFanout: Int) -> MeshNeighbor? {
        let now = Date()
        if !dst.isEmpty {
            if let direct = neighbors.first(where: {
                $0.deviceId == dst && $0.lastSeen >= now.addingTimeInterval(-60)
            }) { return direct }
        }
        return neighbors
            .filter { $0.deviceId != failedNeighbor && $0.deviceId != dst && $0.relayOk }
            .sorted { $0.lastSeen > $1.lastSeen }
            .first
    }
}