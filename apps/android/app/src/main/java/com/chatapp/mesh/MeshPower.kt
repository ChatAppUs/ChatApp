package com.chatapp.mesh

// MeshPower.kt — battery and resource management for the offline mesh.
//
// Mirrors services/mesh/power.go. Mesh networking is expensive on battery-
// powered devices. This module owns every resource knob in one place so no
// caller ever runs an unrestricted scan loop or an unbounded queue. The
// manager is purely advisory state + pure functions over that state, so
// tests can simulate days of operation deterministically.

/** Device power snapshot as reported by the OS. */
data class PowerState(
    val batteryPercent: Int,  // 0..100, or -1 when unknown
    val charging: Boolean,
    val powerSaver: Boolean,
)

data class ScanIntervals(
    val activeMs: Long = 10_000,
    val idleMs: Long = 30_000,
    val dozeMs: Long = 120_000,
)

data class ConnectionLimits(
    val maxPeers: Int = 8,
    val maxPeersCharging: Int = 16,
    val maxQueuePerLink: Int = 256,
)

class PowerManager(
    private var state: PowerState = PowerState(50, false, false),
    private val intervals: ScanIntervals = ScanIntervals(),
    private val limits: ConnectionLimits = ConnectionLimits(),
) {
    companion object {
        private const val TOPO_ACTIVE_WINDOW_MS = 90_000L
    }

    private val lock = Any()
    private var lastTrafficAt: Long = 0
    private var lastScanAt: Long = 0
    private var topologyAt: Long = 0

    fun setPowerState(s: PowerState) { synchronized(lock) { state = s } }

    fun markTraffic(nowMs: Long) { synchronized(lock) { lastTrafficAt = nowMs } }

    fun markTopologyChange(nowMs: Long) { synchronized(lock) { topologyAt = nowMs } }

    fun nextScanIn(nowMs: Long): Pair<Long, Boolean> = synchronized(lock) {
        val recentTraffic = lastTrafficAt > 0 && nowMs - lastTrafficAt < TOPO_ACTIVE_WINDOW_MS
        val recentTopo = topologyAt > 0 && nowMs - topologyAt < TOPO_ACTIVE_WINDOW_MS
        when {
            state.charging && recentTraffic -> intervals.activeMs to true
            recentTraffic || recentTopo -> intervals.activeMs to true
            state.powerSaver && state.batteryPercent in 0..20 -> intervals.dozeMs to false
            state.batteryPercent in 0..15 -> intervals.dozeMs to false
            else -> intervals.idleMs to false
        }
    }

    fun maxPeers(): Int = synchronized(lock) {
        if (state.charging) limits.maxPeersCharging else limits.maxPeers
    }

    fun maxQueuePerLink(): Int = synchronized(lock) { limits.maxQueuePerLink }

    fun relayScale(): Double = synchronized(lock) {
        when {
            state.charging -> 1.0
            state.powerSaver && state.batteryPercent in 0..20 -> 0.25
            state.batteryPercent in 0..15 -> 0.25
            else -> 1.0
        }
    }

    fun scaleRelayQuota(baseBytes: Long): Long =
        (baseBytes.toDouble() * relayScale()).toLong()

    fun status(): Map<String, Any> = synchronized(lock) {
        mapOf(
            "battery_percent" to state.batteryPercent,
            "charging" to state.charging,
            "power_saver" to state.powerSaver,
            "scan_intervals" to mapOf(
                "active_ms" to intervals.activeMs,
                "idle_ms" to intervals.idleMs,
                "doze_ms" to intervals.dozeMs,
            ),
            "max_peers" to limits.maxPeers,
            "max_peers_chg" to limits.maxPeersCharging,
            "max_queue" to limits.maxQueuePerLink,
            "relay_scale" to relayScale(),
        )
    }
}