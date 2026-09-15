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
//
// Identity and crypto (MeshIdentity.kt): discovery beacons are Ed25519-signed
// and advertise an X25519 key-agreement key; unicast payloads are sealed with
// a per-peer AES-256 session key derived via ECDH + HKDF. A peer that never
// held our private key cannot derive the session key. Peers are pinned on
// first sight (TOFU), can be revoked, and per-source sequence numbers are
// checked against a sliding replay window.

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
    val seq: Long = 0,
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

    /** Device identity: Ed25519 signing + X25519 key agreement. */
    val identity = MeshIdentity()

    /** Pinned device id -> Ed25519 public key (trust-on-first-use). */
    private val pinnedKeys = ConcurrentHashMap<String, ByteArray>()

    /** Revoked device id -> revoked Ed25519 public key. */
    private val revokedKeys = ConcurrentHashMap<String, ByteArray>()

    /** Peer device id -> advertised X25519 key-agreement key + epoch. */
    private val peerKEM = ConcurrentHashMap<String, Pair<ByteArray, Long>>()

    /** Per-source anti-replay windows. */
    private val replay = ReplayFilter()

    /** Monotonic per-sender sequence number. */
    private var seqCtr: Long = 0

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
    fun upsertNeighbor(deviceId: String, addr: String, transport: String, relayOk: Boolean = true, now: Long = System.currentTimeMillis()) {
        neighbors[deviceId] = MeshNeighbor(deviceId, addr, transport, relayOk = relayOk, lastSeen = now)
    }

    fun neighborList(): List<MeshNeighbor> = neighbors.values.toList()

    /**
     * Serializes a signed presence beacon carrying routing metadata and this
     * device's X25519 key-agreement advertisement. The signature binds the
     * session-key exchange to the device identity, so a man-in-the-middle
     * cannot substitute its own key-agreement key on a replayed beacon.
     */
    fun beacon(seq: Long): ByteArray = identity.signBeacon(
        deviceId = deviceId,
        kind = if (relayOk) "relay" else "member",
        transport = activeTransport,
        addr = links.firstOrNull()?.kind ?: "",
        seq = seq,
    )

    // ---- sending ---------------------------------------------------------

    /** Encrypts a payload and queues a packet for [dst]. Returns the packet id. */
    fun send(kind: String, dst: String, plaintext: ByteArray, groupId: String? = null): String {
        val sessionKey = sessionKeyFor(dst)
        val sealed = MeshCrypto.encrypt(sessionKey, plaintext)
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
            seq = nextSeq(),
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
     * Handles a raw inbound datagram: a signed beacon registers a neighbour,
     * a packet is deduplicated, delivered locally when addressed to us, or
     * forwarded.
     */
    fun handleInbound(addr: String, data: ByteArray, now: Long = System.currentTimeMillis()): MeshPacket? {
        // Signed beacon: verify the signature and pinned-key binding before
        // trusting the advertised route. A revoked identity is rejected.
        identity.verifySignedBeacon(data, pinnedKeys, revokedKeys)?.let { b ->
            upsertNeighbor(b.deviceId, addr, b.transport, b.kind == "relay", now)
            // Pin the peer's advertised key-agreement key (signed, so it is
            // bound to the verified device identity). A newer epoch replaces
            // the stored key after a peer's rotation.
            if (b.kemPub.size == MeshIdentity.KEY_SIZE) {
                val prev = peerKEM[b.deviceId]
                if (prev == null || b.kemEpoch >= prev.second) {
                    peerKEM[b.deviceId] = b.kemPub to b.kemEpoch
                }
            }
            return null
        }
        val p = MeshPacketCodec.decode(data) ?: return null

        if (seen.containsKey(p.id)) return null
        seen[p.id] = now
        if (seen.size > MAX_SEEN) pruneSeen()

        if (p.dst == deviceId) {
            dequeue(p.id)
            val pt = decryptPayload(p)
            // Anti-replay runs AFTER the payload authenticates: a forged
            // packet must never be able to advance or poison the replay window.
            if (pt != null && (p.seq == 0L || replay.check(p.src, p.seq))) {
                delivered?.invoke(p, pt)
            }
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

    // ---- identity / session-key helpers ---------------------------------

    /**
     * Selects the AEAD key for a payload: the per-peer ECDH session key when
     * the destination has advertised a key-agreement key (the hardened path),
     * otherwise the pre-shared identity key (legacy/native fallback until the
     * peer advertises a KEM key).
     */
    private fun sessionKeyFor(dst: String): ByteArray {
        if (dst.isEmpty()) return key
        val adv = peerKEM[dst] ?: return key
        return identity.sessionKey(deviceId, adv.first, dst) ?: key
    }

    /** Opens a delivered packet: the per-peer session key first, falling back
     *  to the pre-shared identity key (interop with legacy/native senders). */
    private fun decryptPayload(p: MeshPacket): ByteArray? {
        if (p.dst == deviceId) {
            val adv = peerKEM[p.src]
            if (adv != null) {
                val sk = identity.sessionKey(deviceId, adv.first, p.src)
                if (sk != null) {
                    val pt = MeshCrypto.decrypt(sk, p.payload, p.nonce)
                    if (pt != null) return pt
                }
            }
        }
        return MeshCrypto.decrypt(key, p.payload, p.nonce)
    }

    /** Regenerates this device's key-agreement pair and bumps the epoch. */
    fun rotateSessions() = identity.rotate()

    /**
     * Permanently rejects a previously pinned device key for this node and
     * immediately withdraws its route and session advertisement. The expected
     * public key must match the pinned key so a device id alone cannot revoke
     * an unrelated identity.
     */
    fun revokePeer(deviceId: String, expectedPub: ByteArray): Boolean {
        val pinned = pinnedKeys[deviceId] ?: return false
        if (!pinned.contentEquals(expectedPub)) return false
        revokedKeys[deviceId] = expectedPub
        peerKEM.remove(deviceId)
        neighbors.remove(deviceId)
        return true
    }

    /** Whether this node has locally revoked a peer identity. */
    fun isPeerRevoked(deviceId: String): Boolean = revokedKeys.containsKey(deviceId)

    private fun nextSeq(): Long {
        synchronized(this) { seqCtr += 1; return seqCtr }
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
        .put("seq", p.seq)
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
            seq = o.optLong("seq", 0),
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