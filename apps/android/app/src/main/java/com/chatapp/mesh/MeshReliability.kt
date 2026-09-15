package com.chatapp.mesh

import java.util.PriorityQueue

// MeshReliability.kt — traffic classes, the priority-ordered forwarding
// buffer, and the reliable-delivery state machine.
//
// Mirrors services/mesh (priority.go, pfifo.go, reliability.go). A relay
// carries other devices' traffic: with one undifferentiated FIFO, a burst of
// media starves control traffic, so the buffer is priority-ordered and
// byte-bounded. Reliable delivery adds end-to-end acknowledgements, bounded
// exponential retries, and a user-visible delivery state including a
// dead-letter state.

/** Traffic classes for the forwarding buffer; lower drains first. */
enum class Priority(val label: String) {
    CONTROL("control"),   // acks and call signalling: drained first, dropped last
    TEXT("text"),         // chat and group messages
    VOICE("voice"),       // voice notes
    MEDIA("media");       // bulk media: dropped first under pressure

    companion object {
        /** Maps a packet kind to its class; unknown kinds are media. */
        fun forKind(kind: String): Priority = when (kind) {
            MeshEngine.KIND_ACK, MeshEngine.KIND_CALL_SIGNAL -> CONTROL
            MeshEngine.KIND_MESSAGE, MeshEngine.KIND_GROUP_MESSAGE -> TEXT
            MeshEngine.KIND_VOICE_MESSAGE -> VOICE
            else -> MEDIA
        }
    }
}

/** One buffered packet with its class and enqueue order. */
private data class QItem(val packet: MeshPacket, val pri: Int, val seq: Long) {
    fun bytes(): Int = packet.payload.size + packet.nonce.size
}

/**
 * Bounded, priority-ordered forwarding buffer: drain order is by traffic class
 * then FIFO within a class; capacity is bounded by bytes and count; under
 * pressure the lowest-priority class is dropped first and a control packet is
 * never dropped to make room.
 */
class MeshPriorityQueue(
    private val maxBytes: Int = 8 * 1024 * 1024,
    private val maxPackets: Int = 4096,
    private val maxAgeMs: Long = 7L * 24 * 60 * 60 * 1000,
) {
    private val lock = Any()
    private var seq: Long = 0
    private var bytes = 0
    private val items = PriorityQueue<QItem>(
        maxPackets,
        compareBy<QItem> { it.pri }.thenBy { it.seq },
    )

    /** Admits a packet; reports false when the buffer refuses it. */
    fun enqueue(p: MeshPacket, nowMs: Long): Boolean = synchronized(lock) {
        val itemBytes = p.payload.size + p.nonce.size
        if (items.size >= maxPackets || bytes + itemBytes > maxBytes) {
            // Drop the lowest-priority droppable packet; control is never
            // dropped, and a control packet may displace any lower class.
            val victim = items.filter { it.pri > Priority.CONTROL.ordinal }.maxByOrNull { it.pri }
            if (victim != null && victim.pri >= Priority.forKind(p.kind).ordinal) {
                items.remove(victim)
                bytes -= victim.bytes()
            } else {
                return false
            }
        }
        items.add(QItem(p, Priority.forKind(p.kind).ordinal, ++seq))
        bytes += itemBytes
        true
    }

    /** Valid packets in drain order; expired or out-of-TTL packets are removed. */
    fun pending(now: Long): List<MeshPacket> = synchronized(lock) {
        val out = ArrayList<QItem>(items.size)
        val it = items.iterator()
        while (it.hasNext()) {
            val q = it.next()
            if (now - q.packet.createdAt > maxAgeMs || q.packet.ttl <= 0) {
                it.remove()
                bytes -= q.bytes()
            } else {
                out.add(q)
            }
        }
        // java.util.PriorityQueue's iterator is NOT in priority order; drain
        // order is by class then enqueue sequence.
        out.sortWith(compareBy({ it.pri }, { it.seq }))
        out.map { it.packet }
    }

    fun remove(id: String) = synchronized(lock) {
        val it = items.iterator()
        while (it.hasNext()) {
            val q = it.next()
            if (q.packet.id == id) {
                it.remove()
                bytes -= q.bytes()
            }
        }
    }

    fun size(): Int = synchronized(lock) { items.size }
}

/** User-visible state of a reliable mesh transfer (see reliability.go). */
enum class TransferState(val label: String) {
    QUEUED("queued"),         // created, awaiting its first send
    RELAYING("relaying"),     // a transmission is in flight, awaiting an ack
    ACKED("acked"),           // the destination acknowledged the transfer
    EXPIRED("expired"),       // the transfer outlived its lifetime unacked
    DEAD_LETTER("dead_letter"); // the retry budget was exhausted

    val isTerminal: Boolean
        get() = this == ACKED || this == EXPIRED || this == DEAD_LETTER
}

/** One reliable delivery and its state. */
class Transfer(
    val id: String,
    val dst: String,
    val kind: String,
    var state: TransferState = TransferState.QUEUED,
    var attempts: Int = 0,
    /** Packet id of the most recent transmission. */
    var packetId: String = "",
    /** Neighbour the most recent attempt was handed to (alternate-path retry). */
    var lastHop: String = "",
    val createdAt: Long,
    var nextAttempt: Long = createdAt,
    val expiresAt: Long,
) {
    var lastError: String = ""
}

/**
 * Tracks the sender-side reliable-delivery state machine and the receiver-side
 * delivered-transfer set, so retries collapse to exactly-once application
 * delivery while every copy is still acknowledged.
 */
class TransferTracker(
    ackTimeoutMs: Long = 6_000,
    maxAttempts: Int = 5,
    ttlMs: Long = 30L * 60 * 1000,
) {
    private val lock = Any()
    private val active = LinkedHashMap<String, Transfer>()
    private val payloads = HashMap<String, ByteArray>()
    private val delivered = HashMap<String, Long>()
    private val ackTimeout: Long = if (ackTimeoutMs <= 0) 6_000 else ackTimeoutMs
    private val maxAttempts: Int = if (maxAttempts <= 0) 5 else maxAttempts
    private val ttl: Long = if (ttlMs <= 0) 30L * 60 * 1000 else ttlMs

    /** Registers a transfer and returns it (id assigned). */
    fun create(dst: String, kind: String, payload: ByteArray): Transfer = synchronized(lock) {
        val now = System.currentTimeMillis()
        val tr = Transfer(
            id = MeshEngine.newTransferId(),
            dst = dst, kind = kind,
            createdAt = now, nextAttempt = now, expiresAt = now + ttl,
        )
        active[tr.id] = tr
        payloads[tr.id] = payload.copyOf()
        evictLocked()
        tr
    }

    /** Registers a transfer whose id was chosen by the caller (group sends). */
    fun createForForeign(id: String, groupId: String, kind: String, payload: ByteArray): Transfer = synchronized(lock) {
        val now = System.currentTimeMillis()
        val tr = Transfer(
            id = id, dst = groupId, kind = kind,
            createdAt = now, nextAttempt = now, expiresAt = now + ttl,
        )
        active[id] = tr
        payloads[id] = payload.copyOf()
        evictLocked()
        tr
    }

    /** Oldest-first eviction once over the retention bound. */
    private fun evictLocked() {
        while (active.size > 4096) {
            val victim = active.keys.firstOrNull() ?: break
            active.remove(victim)
            payloads.remove(victim)
        }
    }

    fun get(id: String): Transfer? = synchronized(lock) { active[id] }

    fun payload(id: String): ByteArray? = synchronized(lock) { payloads[id]?.copyOf() }

    /** Marks a transfer acknowledged; reports whether this is the first ack. */
    fun ack(id: String): Boolean = synchronized(lock) {
        val tr = active[id] ?: return false
        if (tr.state == TransferState.ACKED) return false
        tr.state = TransferState.ACKED
        tr.nextAttempt = Long.MAX_VALUE
        payloads.remove(id)
        true
    }

    /**
     * Advances the sender-side state machine and returns the transfers that
     * must be (re)transmitted now. Performs no I/O, so it is directly testable.
     */
    fun tick(now: Long = System.currentTimeMillis()): List<Transfer> = synchronized(lock) {
        val due = ArrayList<Transfer>()
        for (tr in active.values) {
            if (tr.state.isTerminal) continue
            if (now > tr.expiresAt) {
                tr.state = TransferState.EXPIRED
                tr.nextAttempt = Long.MAX_VALUE
                payloads.remove(tr.id)
                continue
            }
            if (now < tr.nextAttempt) continue
            if (tr.attempts >= maxAttempts) {
                tr.state = TransferState.DEAD_LETTER
                tr.nextAttempt = Long.MAX_VALUE
                payloads.remove(tr.id)
                continue
            }
            tr.attempts++
            tr.state = TransferState.RELAYING
            var backoff = ackTimeout
            var i = 1
            while (i < tr.attempts) {
                backoff *= 2
                if (backoff >= 60_000) { backoff = 60_000; break }
                i++
            }
            tr.nextAttempt = now + backoff
            due.add(tr)
        }
        due
    }

    /** Records the packet id of the most recent transmission. */
    fun notePacket(id: String, packetId: String) = synchronized(lock) {
        active[id]?.packetId = packetId
    }

    /** Records which neighbour an attempt was handed to. */
    fun noteHop(id: String, hop: String, packetId: String) = synchronized(lock) {
        val tr = active[id] ?: return
        tr.lastHop = hop
        tr.packetId = packetId
    }

    /**
     * Applies receiver-side exactly-once semantics: true the first time a
     * transfer id is seen, false for every duplicate copy.
     */
    fun markDelivered(id: String): Boolean = synchronized(lock) {
        if (delivered.containsKey(id)) return false
        delivered[id] = System.currentTimeMillis()
        if (delivered.size > 8192) {
            val victims = delivered.entries.sortedBy { it.value }.take(delivered.size / 2)
            for (v in victims) delivered.remove(v.key)
        }
        true
    }

    /** Drops transfers addressed to a revoked device (route withdrawal). */
    fun dropTo(dst: String) = synchronized(lock) {
        val victims = active.values.filter { it.dst == dst }.map { it.id }
        for (v in victims) {
            active.remove(v)
            payloads.remove(v)
        }
    }

    /** How many transfers sit in each state — the aggregate status surface. */
    fun counts(): Map<String, Int> = synchronized(lock) {
        val out = HashMap<String, Int>()
        for (s in TransferState.entries) out[s.label] = 0
        for (tr in active.values) out.merge(tr.state.label, 1, Int::plus)
        out
    }
}
