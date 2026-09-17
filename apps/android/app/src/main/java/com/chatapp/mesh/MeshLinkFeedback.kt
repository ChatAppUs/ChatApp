package com.chatapp.mesh

import kotlin.math.abs

// MeshLinkFeedback.kt — link quality metrics for the offline mesh.
//
// Mirrors services/mesh/linkfeedback.go. EWMA-smoothed RTT, loss rate,
// jitter and estimated bandwidth per link, plus an adaptive rate that
// backs off under loss and a unified link-quality score for the routing
// table to use when sorting candidate relays.

data class LinkSample(
    val rtt: Long,       // microseconds
    val lost: Boolean,
    val sentAt: Long,    // ms
    val bytes: Int,
)

class LinkFeedback(private val windowSize: Int = 64) {

    private val lock = Any()
    private var ewmaRTT = 0.0        // microseconds
    private var ewmaLoss = 0.0       // 0..1
    private var ewmaJitter = 0.0     // microseconds
    private var estimatedBPS: Double = 256.0 * 1024.0
    private val windowSamples = Array<LinkSample?>(windowSize) { null }
    private var windowIdx = 0
    private var lastUpdate = System.currentTimeMillis()

    fun recordSample(sample: LinkSample) = synchronized(lock) {
        val alpha = 0.25
        val rttUs = sample.rtt.toDouble()
        if (ewmaRTT == 0.0) {
            ewmaRTT = rttUs
        } else {
            ewmaJitter = (1 - alpha) * ewmaJitter + alpha * abs(rttUs - ewmaRTT)
            ewmaRTT = (1 - alpha) * ewmaRTT + alpha * rttUs
        }
        ewmaLoss = (1 - alpha) * ewmaLoss + alpha * (if (sample.lost) 1.0 else 0.0)
        if (!sample.lost && sample.bytes > 0 && ewmaRTT > 0) {
            val rttSec = ewmaRTT / 1_000_000.0
            if (rttSec > 0) estimatedBPS = 0.875 * estimatedBPS + 0.125 * sample.bytes / rttSec
        }
        windowSamples[windowIdx % windowSize] = sample
        windowIdx++
        lastUpdate = System.currentTimeMillis()
    }

    fun rtt(): Long = synchronized(lock) {
        if (ewmaRTT <= 0) 50_000L else ewmaRTT.toLong()
    }

    fun lossRate(): Double = synchronized(lock) { ewmaLoss }

    fun jitter(): Long = synchronized(lock) { ewmaJitter.toLong() }

    fun estimatedBPS(): Double = synchronized(lock) { estimatedBPS }

    fun windowLossRate(): Double = synchronized(lock) {
        val count = minOf(windowIdx, windowSize)
        if (count == 0) return 0.0
        val lost = (0 until count).count { windowSamples[it % windowSize]?.lost == true }
        lost.toDouble() / count
    }

    fun adaptiveRate(currentBytesPerSecond: Int): Int = synchronized(lock) {
        var rate = (if (currentBytesPerSecond > 0) currentBytesPerSecond else 256 * 1024).toDouble()
        rate = when {
            ewmaLoss > 0.05 -> rate * 0.5
            ewmaLoss < 0.01 -> rate + 256.0 * 1024.0 / 10.0
            else -> rate
        }
        if (estimatedBPS > 0 && rate > estimatedBPS * 0.9) rate = estimatedBPS * 0.9
        val result = rate.toInt()
        if (result < 4096) 4096 else result
    }

    fun linkQuality(): Double = synchronized(lock) {
        val lossPenalty = maxOf(0.0, 1.0 - ewmaLoss)
        val rttScore = when {
            ewmaRTT > 1_000_000 -> 0.1
            ewmaRTT > 500_000 -> 0.3
            ewmaRTT > 200_000 -> 0.5
            ewmaRTT > 100_000 -> 0.7
            else -> 1.0
        }
        lossPenalty * 0.6 + rttScore * 0.4
    }
}