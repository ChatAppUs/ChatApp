package com.chatapp.mesh

import java.security.SecureRandom

// MeshFragment.kt — MTU-bounded fragmentation and reassembly.
//
// Mirrors services/mesh/fragment.go. Radio links carry far smaller datagrams
// than logical payloads (voice notes, media). A payload larger than the radio
// MTU is split into fixed-size fragments, each encrypted with its own AEAD
// nonce and carried as an ordinary packet, so routing, TTL, dedup and
// store-and-forward apply unchanged. The receiver reassembles under bounded
// memory and time, ignores duplicate fragments, expires incomplete groups,
// and verifies a SHA-256 digest before handing anything to the application.

data class SplitParts(val chunks: List<ByteArray>, val fragId: String?, val digest: ByteArray)

private class Assembly(val total: Int, val digest: ByteArray) {
    val parts = HashMap<Int, ByteArray>()
    var bytes = 0
    var updated = System.currentTimeMillis()
}

/** Splits a plaintext into MTU-bounded chunks; null fragId when it fits whole. */
fun splitPayload(plaintext: ByteArray, maxPayload: Int = MeshEngine.DEFAULT_MAX_PAYLOAD): SplitParts {
    val mp = if (maxPayload <= 0) MeshEngine.DEFAULT_MAX_PAYLOAD else maxPayload
    val sum = MeshEngine.sha256(plaintext)
    if (plaintext.size <= mp) return SplitParts(listOf(plaintext.copyOf()), null, sum)
    val chunks = ArrayList<ByteArray>()
    var off = 0
    while (off < plaintext.size) {
        val end = minOf(off + mp, plaintext.size)
        chunks.add(plaintext.copyOfRange(off, end))
        off = end
    }
    val b = ByteArray(16); SecureRandom().nextBytes(b)
    return SplitParts(chunks, b.joinToString("") { "%02x".format(it) }, sum)
}

/**
 * Collects decrypted fragments per (source, fragment-group) and yields the
 * complete payload once every part has arrived and the digest verifies.
 */
class FragmentAssembler(
    private val maxOpen: Int = 256,
    private val maxBytes: Int = 8 * 1024 * 1024,
    private val ttlMs: Long = 5L * 60 * 1000,
) {
    private val lock = Any()
    private val open = HashMap<String, Assembly>()
    private var bytes = 0

    /**
     * Accepts one decrypted fragment. Returns the complete payload when the
     * group finished (digest verified), null while parts are outstanding or
     * the fragment was a duplicate / invalid.
     */
    fun add(p: MeshPacket, chunk: ByteArray): ByteArray? {
        if (p.fragTotal <= 1) return chunk
        val fragId = p.fragId
        if (fragId.isNullOrEmpty() || p.fragIndex < 0 || p.fragIndex >= p.fragTotal || p.fragTotal > 4096) return null
        val key = p.src + "\u0000" + fragId
        val now = System.currentTimeMillis()
        synchronized(lock) {
            expireLocked(now)
            var a = open[key]
            if (a == null) {
                if (open.size >= maxOpen) return null
                val sum = p.fragSum ?: return null
                a = Assembly(p.fragTotal, sum)
                open[key] = a
            }
            if (a.total != p.fragTotal || !a.digest.contentEquals(p.fragSum)) {
                // Contradictory metadata for one group id: drop and fail closed.
                dropLocked(key, a)
                return null
            }
            if (a.parts.containsKey(p.fragIndex)) return null
            if (bytes + chunk.size > maxBytes) {
                dropLocked(key, a)
                return null
            }
            a.parts[p.fragIndex] = chunk.copyOf()
            a.bytes += chunk.size
            bytes += chunk.size
            a.updated = now
            if (a.parts.size < a.total) return null
            val full = ByteArray(a.bytes)
            var pos = 0
            for (i in 0 until a.total) {
                val piece = a.parts[i] ?: return null
                piece.copyInto(full, pos)
                pos += piece.size
            }
            dropLocked(key, a)
            if (!MeshEngine.sha256(full).contentEquals(a.digest)) return null
            return full
        }
    }

    /** How many reassembly groups are in progress. */
    fun openCount(): Int = synchronized(lock) { open.size }

    private fun dropLocked(key: String, a: Assembly) {
        open.remove(key)
        bytes -= a.bytes
        if (bytes < 0) bytes = 0
    }

    private fun expireLocked(now: Long) {
        val dead = open.filter { now - it.value.updated > ttlMs }
        for ((k, a) in dead) dropLocked(k, a)
    }
}
