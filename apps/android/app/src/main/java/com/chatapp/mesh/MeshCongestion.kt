package com.chatapp.mesh

// MeshCongestion.kt — byte-based radio backpressure for the offline mesh.
//
// Mirrors services/mesh/congestion.go. Without backpressure, a relay can
// saturate its radio regardless of capacity and starve higher-priority
// traffic. A token bucket meters byte throughput and buffers excess packets
// in the priority queue, which sheds the lowest-priority class first.

class CongestionController(
    rateBytesPerSecond: Int,
    burstBytes: Int,
) {
    companion object {
        const val DEFAULT_RATE = 256 * 1024
        const val DEFAULT_BURST = 512 * 1024
    }

    private val lock = Any()
    private val rate: Double
    private val burst: Double
    private var tokens: Double
    private var last: Long = System.currentTimeMillis()
    var admitted: Long = 0
        private set
    var blocked: Long = 0
        private set

    init {
        this.rate = (if (rateBytesPerSecond > 0) rateBytesPerSecond else DEFAULT_RATE).toDouble()
        this.burst = (if (burstBytes > 0) burstBytes else DEFAULT_BURST).toDouble()
        this.tokens = this.burst
    }

    /** Reports whether [bytes] may be sent now. Thread-safe. */
    fun allow(bytes: Int, nowMs: Long): Boolean = synchronized(lock) {
        if (bytes <= 0) return false
        var now = nowMs
        if (now < last) now = last
        tokens += (now - last).toDouble() / 1000.0 * rate
        if (tokens > burst) tokens = burst
        last = now
        if (bytes.toDouble() > tokens) {
            blocked++
            return false
        }
        tokens -= bytes.toDouble()
        admitted++
        true
    }

    fun status(): Map<String, Any> = synchronized(lock) {
        mapOf(
            "rate_bytes_per_second" to rate.toInt(),
            "burst_bytes" to burst.toInt(),
            "admitted_packets" to admitted,
            "blocked_packets" to blocked,
            "available_bytes" to tokens.toInt(),
        )
    }
}