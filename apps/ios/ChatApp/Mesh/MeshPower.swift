import Foundation

/// MeshPower.swift — battery and resource management for the offline mesh.
///
/// Mirrors services/mesh/power.go. Owns every resource knob — scan cadences,
/// connection cap, relay quota scaling — so no caller runs an unrestricted
/// scan loop. Purely advisory state + pure functions; deterministic in tests.

struct PowerState {
    var batteryPercent: Int = 50
    var charging = false
    var powerSaver = false
}

struct ScanIntervals {
    var activeMs: Int64 = 10_000
    var idleMs: Int64 = 30_000
    var dozeMs: Int64 = 120_000
}

struct ConnectionLimits {
    var maxPeers = 8
    var maxPeersCharging = 16
    var maxQueuePerLink = 256
}

final class PowerManager {
    private static let topoActiveWindowMs: Int64 = 90_000

    private let lock = NSLock()
    private var state = PowerState()
    private let intervals = ScanIntervals()
    private let limits = ConnectionLimits()
    private var lastTrafficAt: Int64 = 0
    private var lastScanAt: Int64 = 0
    private var topologyAt: Int64 = 0

    func setPowerState(_ s: PowerState) {
        lock.lock(); defer { lock.unlock() }
        state = s
    }

    func markTraffic(nowMs: Int64) {
        lock.lock(); defer { lock.unlock() }
        lastTrafficAt = nowMs
    }

    func markTopologyChange(nowMs: Int64) {
        lock.lock(); defer { lock.unlock() }
        topologyAt = nowMs
    }

    func nextScanIn(nowMs: Int64) -> (intervalMs: Int64, fullScan: Bool) {
        lock.lock(); defer { lock.unlock() }
        let recentTraffic = lastTrafficAt > 0 && nowMs - lastTrafficAt < Self.topoActiveWindowMs
        let recentTopo = topologyAt > 0 && nowMs - topologyAt < Self.topoActiveWindowMs
        switch true {
        case state.charging && recentTraffic: return (intervals.activeMs, true)
        case recentTraffic || recentTopo: return (intervals.activeMs, true)
        case state.powerSaver && state.batteryPercent <= 20: return (intervals.dozeMs, false)
        case state.batteryPercent <= 15: return (intervals.dozeMs, false)
        default: return (intervals.idleMs, false)
        }
    }

    func maxPeers() -> Int {
        lock.lock(); defer { lock.unlock() }
        return state.charging ? limits.maxPeersCharging : limits.maxPeers
    }

    func maxQueuePerLink() -> Int {
        lock.lock(); defer { lock.unlock() }
        return limits.maxQueuePerLink
    }

    func relayScale() -> Double {
        lock.lock(); defer { lock.unlock() }
        switch true {
        case state.charging: return 1.0
        case state.powerSaver && state.batteryPercent <= 20: return 0.25
        case state.batteryPercent <= 15: return 0.25
        default: return 1.0
        }
    }

    func scaleRelayQuota(baseBytes: Int64) -> Int64 {
        Int64(Double(baseBytes) * relayScale())
    }

    func status() -> [String: Any] {
        lock.lock(); defer { lock.unlock() }
        return [
            "battery_percent": state.batteryPercent,
            "charging": state.charging,
            "power_saver": state.powerSaver,
            "scan_intervals": [
                "active_ms": intervals.activeMs,
                "idle_ms": intervals.idleMs,
                "doze_ms": intervals.dozeMs,
            ],
            "max_peers": limits.maxPeers,
            "max_peers_chg": limits.maxPeersCharging,
            "max_queue": limits.maxQueuePerLink,
            "relay_scale": relayScale(),
        ]
    }
}