package com.chatapp.mesh

import android.content.Context
import org.json.JSONObject
import java.security.SecureRandom
import java.util.concurrent.ConcurrentHashMap
import javax.crypto.Cipher
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.SecretKeySpec

// MeshEngine.kt — transport-agnostic mesh logic for the native clients.
//
// This mirrors services/mesh (routing.go, storeforward.go, packet.go) so a
// device behaves identically whichever radio is carrying packets: the same
// packet envelope, the same TTL/dedup rules, and the same store-and-forward
// queue. It deliberately depends only on the JDK (plus org.json, already a
// dependency) so the routing and queue logic is unit-testable on the JVM
// without radio hardware.
//
// Transport selection follows Anonymous.md §5.3: when there is no Internet the
// app prefers local Wi-Fi / Wi-Fi Direct, then Bluetooth, then falls back to
// store-and-forward until any link becomes available.

/** A mesh packet, byte-compatible with the Go engine's Packet envelope. */
data class MeshPacket(
    val id: String,
    val src: String,
    val dst: String,
    val groupId: String? = null,
    val kind: String,
    var ttl: Int,
    var hops: Int = 0,
    val payload: ByteArray,
    val nonce: ByteArray,
    val createdAt: Long,
)

/** A known peer on the mesh. */
data class MeshNeighbor(
    val deviceId: String,
    val addr: String,
    val transport: String,
    val relayOk: Boolean,
    val lastSeen: Long,
)

/**
 * MeshEngine owns the routing table, the duplicate-suppression cache, the
 * store-and-forward queue and the ordered transport chain.
 *
 * Concurrency: all mutable maps are concurrent; the queue is guarded by an
 * explicit lock because it is drained in-order.
 */
class MeshEngine(
    private val deviceId: String,
    private val key: ByteArray,
    private val maxHops: Int = DEFAULT_MAX_HOPS,
    private val maxQueue: Int = 1000,
    private val maxAgeMs: Long = 7L * 24 * 60 * 60 * 1000,
) {

    private val neighbors = ConcurrentHashMap<String, MeshNeighbor>()
    private val seen = ConcurrentHashMap<String, Long>()
    private val queue = ArrayDeque<MeshPacket>()
    private val queueLock = Any()
    private val random = SecureRandom()

    @Volatile private var links: List<MeshLink> = emptyList()

    /** The transport currently carrying traffic, or "none". */
    @Volatile var activeTransport: String = "none"
        private set

    /** Whether this device consents to relay other devices' packets. */
    @Volatile var relayOk: Boolean = true

    /** Wires the ordered transport chain: Wi-Fi Direct → local Wi-Fi → Bluetooth. */
    fun attach(vararg transportChain: MeshLink) {
        links = transportChain.toList()
        links.forEach { link ->
            if (link.isAvailable()) {
                link.start()
                if (activeTransport == "none") activeTransport = link.kind
            }
        }
        if (links.none { it.isAvailable() }) activeTransport = "none"
    }

    /** Re-evaluates which link is usable (e.g. after radios change state). */
    fun refreshTransport() {
        val usable = links.filter { it.isAvailable() }
        activeTransport = usable.firstOrNull()?.kind ?: "none"
    }

    fun stop() {
        links.forEach { runCatching { it.stop() } }
        activeTransport = "none"
    }

    // ---- discovery -------------------------------------------------------

    /** Records a neighbour from a discovery beacon (device id + reachable addr). */
    fun upsertNeighbor(deviceId: String, addr: String, transport: String, now: Long = System.currentTimeMillis()) {
        neighbors[deviceId] = MeshNeighbor(deviceId, addr, transport, relayOk = true, lastSeen = now)
    }

    fun neighborList(): List<MeshNeighbor> = neighbors.values.toList()

    /** Serializes a presence beacon; carries routing metadata only, never content. */
    fun beacon(seq: Long): ByteArray = JSONObject()
        .put("device_id", deviceId)
        .put("kind", if (relayOk) "relay" else "member")
        .put("transport", activeTransport)
        .put("addr", links.firstOrNull()?.kind ?: "")
        .put("seq", seq)
        .toString()
        .toByteArray()

    // ---- sending ---------------------------------------------------------

    /** Encrypts a payload and queues a packet for [dst]. Returns the packet id. */
    fun send(kind: String, dst: String, plaintext: ByteArray, groupId: String? = null): String {
        val sealed = MeshCrypto.encrypt(key, plaintext)
        val p = MeshPacket(
            id = newId(),
            src = deviceId,
            dst = dst,
            groupId = groupId,
            kind = kind,
            ttl = maxHops,
            payload = sealed.ciphertext,
            nonce = sealed.nonce,
            createdAt = System.currentTimeMillis(),
        )
        seen[p.id] = System.currentTimeMillis()
        enqueue(p)
        flush()
        return p.id
    }

    fun enqueue(p: MeshPacket) {
        synchronized(queueLock) {
            queue.addLast(p)
            while (queue.size > maxQueue) queue.removeFirst()
        }
    }

    /** Packets still valid (unexpired, TTL remaining). Does not remove them. */
    fun pending(now: Long = System.currentTimeMillis()): List<MeshPacket> = synchronized(queueLock) {
        val it = queue.iterator()
        while (it.hasNext()) {
            val p = it.next()
            if (now - p.createdAt > maxAgeMs || p.ttl <= 0) it.remove()
        }
        queue.toList()
    }

    fun dequeue(id: String) {
        synchronized(queueLock) { queue.removeAll { it.id == id } }
    }

    fun queueSize(): Int = synchronized(queueLock) { queue.size }

    // ---- receiving -------------------------------------------------------

    /**
     * Handles a raw inbound datagram: a beacon registers a neighbour, a packet
     * is deduplicated, delivered locally when addressed to us, or forwarded.
     */
    fun handleInbound(addr: String, data: ByteArray, now: Long = System.currentTimeMillis()): MeshPacket? {
        parseBeacon(data)?.let { b ->
            upsertNeighbor(b.first, addr, b.second, now)
            return null
        }
        val p = MeshPacketCodec.decode(data) ?: return null

        if (seen.containsKey(p.id)) return null
        seen[p.id] = now
        if (seen.size > MAX_SEEN) pruneSeen()

        if (p.dst == deviceId) {
            dequeue(p.id)
            delivered?.invoke(p, MeshCrypto.decrypt(key, p.payload, p.nonce))
            return p
        }

        if (p.ttl <= 0) return null
        p.ttl -= 1
        p.hops += 1
        enqueue(p)
        flush()
        return p
    }

    /** Application-layer delivery callback (decrypted packet). */
    var delivered: ((MeshPacket, ByteArray?) -> Unit)? = null

    /**
     * Attempts to hand queued packets to a known neighbour. Packets stay queued
     * when no link can carry them yet — that is the store-and-forward path.
     */
    fun flush(): Int {
        var sent = 0
        val target = links.firstOrNull { it.isAvailable() } ?: return 0
        for (p in pending()) {
            for (nb in neighbors.values) {
                if (!relayOk && nb.deviceId != p.dst) continue
                val wire = MeshPacketCodec.encode(p)
                if (target.send(nb.addr, wire)) {
                    dequeue(p.id)
                    sent++
                    break
                }
            }
        }
        return sent
    }

    private fun pruneSeen() {
        val cutoff = System.currentTimeMillis() - maxAgeMs
        seen.entries.removeIf { it.value < cutoff }
    }

    private fun newId(): String {
        val b = ByteArray(16)
        random.nextBytes(b)
        return b.joinToString("") { "%02x".format(it) }
    }

    private fun parseBeacon(data: ByteArray): Pair<String, String>? = try {
        val o = JSONObject(String(data))
        val id = o.optString("device_id")
        if (id.isEmpty()) null else id to o.optString("transport", "local_wifi")
    } catch (_: Exception) {
        null
    }

    companion object {
        /** Default hop budget, matching services/mesh/scale.go. */
        const val DEFAULT_MAX_HOPS = 64
        private const val MAX_SEEN = 10000
    }
}

/** Packet wire codec — JSON, matching the Go engine's marshal layout. */
object MeshPacketCodec {
    fun encode(p: MeshPacket): ByteArray = JSONObject()
        .put("id", p.id)
        .put("src", p.src)
        .put("dst", p.dst)
        .put("group_id", p.groupId ?: JSONObject.NULL)
        .put("kind", p.kind)
        .put("ttl", p.ttl)
        .put("hops", p.hops)
        .put("payload", android.util.Base64.encodeToString(p.payload, android.util.Base64.NO_WRAP))
        .put("nonce", android.util.Base64.encodeToString(p.nonce, android.util.Base64.NO_WRAP))
        .put("created_at", p.createdAt)
        .toString()
        .toByteArray()

    fun decode(data: ByteArray): MeshPacket? = try {
        val o = JSONObject(String(data))
        MeshPacket(
            id = o.getString("id"),
            src = o.getString("src"),
            dst = o.getString("dst"),
            groupId = o.optString("group_id", "").takeUnless { it.isEmpty() || it == "null" },
            kind = o.optString("kind", "message"),
            ttl = o.optInt("ttl", MeshEngine.DEFAULT_MAX_HOPS),
            hops = o.optInt("hops", 0),
            payload = android.util.Base64.decode(o.optString("payload"), android.util.Base64.NO_WRAP),
            nonce = android.util.Base64.decode(o.optString("nonce"), android.util.Base64.NO_WRAP),
            createdAt = o.optLong("created_at", System.currentTimeMillis()),
        )
    } catch (_: Exception) {
        null
    }
}

/** Authenticated encryption shared by Go, Android and iOS mesh clients. */
object MeshCrypto {
    private const val NONCE_LEN = 12
    private const val TAG_BITS = 128

    data class Sealed(val ciphertext: ByteArray, val nonce: ByteArray)

    fun encrypt(key: ByteArray, plaintext: ByteArray): Sealed {
        val nonce = ByteArray(NONCE_LEN).also { SecureRandom().nextBytes(it) }
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, SecretKeySpec(key, "AES"), GCMParameterSpec(TAG_BITS, nonce))
        return Sealed(cipher.doFinal(plaintext), nonce)
    }

    /** Returns the plaintext, or null when the key is wrong or data was tampered. */
    fun decrypt(key: ByteArray, ciphertext: ByteArray, nonce: ByteArray): ByteArray? = try {
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, SecretKeySpec(key, "AES"), GCMParameterSpec(TAG_BITS, nonce))
        cipher.doFinal(ciphertext)
    } catch (_: Exception) {
        null
    }
}

/**
 * Selects and manages the transport chain on a real device. Construction is
 * cheap; [start] opens the radios and begins discovery.
 */
class NativeMeshManager(private val context: Context, private val deviceId: String, private val key: ByteArray) {

    val engine = MeshEngine(deviceId, key)

    private val wifiDirect = WifiDirectLink(context) { addr, data -> engine.handleInbound(addr, data) }
    private val localWifi = LocalWifiLink({ addr, data -> engine.handleInbound(addr, data) })
    private val bluetooth = BluetoothLink(context) { addr, data -> engine.handleInbound(addr, data) }

    /**
     * Starts the chain in Anonymous.md §5.3 order. Links that the platform
     * reports unavailable are skipped rather than failing the whole mesh.
     */
    fun start() {
        engine.attach(wifiDirect, localWifi, bluetooth)
    }

    fun stop() = engine.stop()

    /** Current transport state for the UI / API status surface. */
    fun status(): JSONObject = JSONObject()
        .put("transport", engine.activeTransport)
        .put("relay_ok", engine.relayOk)
        .put("peers", engine.neighborList().size)
        .put("pending", engine.queueSize())
}
