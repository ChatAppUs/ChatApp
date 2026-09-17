package com.chatapp.mesh

// MeshRelayLimit.kt — per-source relay quotas (abuse prevention).
//
// Mirrors services/mesh/relaylimit.go. A relay forwards other devices' traffic.
// Without a per-source cap, one flooded device can monopolise the relay's radio
// and queue. This is a token-bucket limiter keyed by source device id, applied
// only to traffic a node forwards (never to packets addressed to itself).

private class RelayBucket(var tokens: Double, var last: Long)

class RelayLimiter(
    private val everyMs: Long,
    private val burst: Int,
) {
    companion object {
        /** Allows a burst of 200 packets then refills 50/s — comfortably above
         *  chat/voice-note traffic, far below flood rates. */
        fun defaultQuota(): RelayQuota = RelayQuota(20, 200)
    }

    data class RelayQuota(val everyMs: Long, val burst: Int) {
        init {
            require(everyMs > 0) { "everyMs must be positive" }
            require(burst > 0) { "burst must be positive" }
        }
    }

    private val lock = Any()
    private val buckets = HashMap<String, RelayBucket>()

    /** Reports whether [src] may forward one packet now. */
    fun allow(src: String, nowMs: Long): Boolean = synchronized(lock) {
        val b = buckets.getOrPut(src) {
            RelayBucket(burst.toDouble(), nowMs)
        }
        val elapsed = nowMs - b.last
        b.tokens += elapsed.toDouble() / everyMs.toDouble()
        if (b.tokens > burst.toDouble()) b.tokens = burst.toDouble()
        b.last = nowMs
        if (b.tokens < 1.0) return false
        b.tokens -= 1.0
        true
    }

    fun size(): Int = synchronized(lock) { buckets.size }

    /** Drops buckets idle longer than [maxIdleMs] so long-lived relays do not
     *  grow the map without bound. */
    fun prune(nowMs: Long, maxIdleMs: Long) = synchronized(lock) {
        val iter = buckets.iterator()
        while (iter.hasNext()) {
            if (nowMs - iter.next().value.last > maxIdleMs) iter.remove()
        }
    }
}