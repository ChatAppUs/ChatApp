package com.chatapp.mesh

import org.json.JSONObject
import java.security.MessageDigest
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
// first sight (TOFU), can be revoked locally AND network-wide via flooded,
// signed revocation notices, and per-source sequence numbers are checked
// against a sliding replay window.
//
// Delivery guarantees mirror the Go engine: reliable transfers carry a
// transfer id and are acknowledged end-to-end with bounded exponential
// retries and a user-visible delivery state; payloads larger than one radio
// datagram are split into MTU-bounded, individually encrypted fragments and
// reassembled under bounded memory with a SHA-256 digest check; the
// forwarding buffer is priority-ordered (control first, media droppable); and
// group messages are sealed under rotating per-group sender keys with
// per-member acknowledgements.

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
    /** Envelope format version; the engine stamps 1 and rejects newer formats. */
    val version: Int = MeshEngine.MESH_VERSION,
    /** The immediate hop this packet arrived from (relay bookkeeping). */
    var hopSrc: String? = null,
    /** The transfer this packet acknowledges. */
    val ackFor: String? = null,
    /** The reliable-transfer id shared by every (re)transmission. */
    val xfer: String? = null,
    /** Fragmentation: this fragment's index and the group's total. */
    val fragIndex: Int = 0,
    val fragTotal: Int = 0,
    /** Fragmentation: the group id and SHA-256 digest over the full payload. */
    val fragId: String? = null,
    val fragSum: ByteArray? = null,
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
 * priority forwarding buffer and the ordered transport chain.
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
    private val pfifo = MeshPriorityQueue(maxPackets = maxQueue, maxBytes = DEFAULT_QUEUE_MAX_BYTES, maxAgeMs = maxAgeMs)
    private val random = SecureRandom()

    /** Device identity: Ed25519 signing + X25519 key agreement. */
    val identity = MeshIdentity()

    /** Pinned device id -> Ed25519 public key (trust-on-first-use). */
    private val pinnedKeys = ConcurrentHashMap<String, ByteArray>()

    /** Revoked device id -> revoked Ed25519 public key. */
    private val revokedKeys = ConcurrentHashMap<String, ByteArray>()

    /** Peer device id -> advertised X25519 key-agreement key + epoch. */
    private val peerKEM = ConcurrentHashMap<String, Pair<ByteArray, Long>>()

    /** Per-group sender keys and rotation (see MeshGroupKeys.kt). */
    val groupKeys = GroupKeyManager()

    /** Reliable-transfer tracker (see MeshReliability.kt). */
    val transfers = TransferTracker()

    /** Per-member group acknowledgements (see MeshGroupAck.kt). */
    val groupAcks = GroupAckTracker()

    /** Fragment reassembly (see MeshFragment.kt). */
    private val frags = FragmentAssembler()

    /** Flooded revocation notices (see MeshRevocation.kt). */
    private val revStore = RevocationStore()

    /** Group id -> member device ids, for per-member ack tracking. */
    private val groupMembers = ConcurrentHashMap<String, List<String>>()

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
        pruneNeighbors(now)
        neighbors[deviceId] = MeshNeighbor(deviceId, addr, transport, relayOk = relayOk, lastSeen = now)
    }

    fun neighborList(): List<MeshNeighbor> = pruneNeighbors(System.currentTimeMillis()).values.toList()

    /**
     * Drops neighbours whose last beacon is older than [NEIGHBOUR_TTL_MS]
     * (three minutes, matching the Go engine's neighbourMaxAge): a phone that
     * slept, moved or left must stop consuming send attempts.
     */
    private fun pruneNeighbors(now: Long): Map<String, MeshNeighbor> {
        neighbors.entries.removeAll { now - it.value.lastSeen > NEIGHBOUR_TTL_MS }
        return neighbors
    }

    /**
     * Forwarding candidates, best first: relay-consenting peers, then the
     * freshest beacons — the same ordering signals the Go route table scores
     * (consent, freshness).
     */
    private fun relayCandidates(p: MeshPacket): List<MeshNeighbor> {
        val now = System.currentTimeMillis()
        return neighbors.values
            .filter { now - it.lastSeen <= NEIGHBOUR_TTL_MS }
            .sortedWith(compareByDescending<MeshNeighbor> { it.relayOk || it.deviceId == p.dst }.thenByDescending { it.lastSeen })
    }

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

    /**
     * Sends a reliable unicast transfer: the payload carries a transfer id and
     * the destination acknowledges it end-to-end. Retry with [tick]; the
     * delivery state is user-visible (see TransferTracker).
     */
    fun sendReliable(kind: String, dst: String, plaintext: ByteArray): String {
        if (plaintext.size > MAX_RELIABLE_PAYLOAD) return sendLarge(kind, dst, plaintext, group = false)
        val tr = transfers.create(dst, kind, plaintext)
        // Drive the first attempt through the SAME accounting the retry loop
        // uses, so the transfer's state reflects a real transmission now.
        tick()
        return tr.id
    }

    /**
     * Sends a group message sealed under the group's per-group sender key.
     * Members acknowledge per-member (see GroupAckTracker) when
     * [trackGroupMembers] recorded the roster.
     */
    fun sendGroup(kind: String, groupId: String, plaintext: ByteArray): String {
        if (plaintext.size > DEFAULT_MAX_PAYLOAD) return sendLarge(kind, groupId, plaintext, group = true)
        val gk = groupKeys.keyFor(groupId)
        val sealed = MeshCrypto.encrypt(gk.key, plaintext)
        val p = MeshPacket(
            id = newId(),
            src = deviceId,
            dst = "",
            groupId = groupId,
            kind = kind,
            ttl = maxHops,
            payload = sealed.ciphertext,
            nonce = sealed.nonce,
            createdAt = System.currentTimeMillis(),
            seq = nextSeq(),
            xfer = newId(),
        )
        val members = groupMembers[groupId]
        if (members != null && members.isNotEmpty()) {
            groupAcks.create(p.xfer!!, groupId, members)
            transfers.createForForeign(p.xfer!!, groupId, kind, plaintext)
        }
        seen[p.id] = System.currentTimeMillis()
        enqueue(p)
        flush()
        return p.xfer!!
    }

    /**
     * Splits a payload that exceeds one radio datagram into MTU-bounded,
     * individually encrypted fragments and enqueues every one. Returns the
     * fragment-group id, or the packet id when the payload fit unfragmented.
     */
    fun sendLarge(kind: String, target: String, plaintext: ByteArray, group: Boolean): String {
        val parts = splitPayload(plaintext, DEFAULT_MAX_PAYLOAD)
        if (parts.fragId == null) {
            return if (group) sendGroup(kind, target, plaintext) else send(kind, target, plaintext)
        }
        val encKey = if (group) groupKeys.keyFor(target).key else sessionKeyFor(target)
        for (i in parts.chunks.indices) {
            val sealed = MeshCrypto.encrypt(encKey, parts.chunks[i])
            val p = MeshPacket(
                id = newId(),
                src = deviceId,
                dst = if (group) "" else target,
                groupId = if (group) target else null,
                kind = kind,
                ttl = maxHops,
                hops = 0,
                payload = sealed.ciphertext,
                nonce = sealed.nonce,
                createdAt = System.currentTimeMillis(),
                seq = nextSeq(),
                fragIndex = i,
                fragTotal = parts.chunks.size,
                fragId = parts.fragId,
                fragSum = parts.digest,
            )
            seen[p.id] = System.currentTimeMillis()
            enqueue(p)
        }
        flush()
        return parts.fragId
    }

    /** Records a group's member roster so group sends track per-member acks. */
    fun trackGroupMembers(groupId: String, members: List<String>) {
        groupMembers[groupId] = members.filter { it != deviceId }
    }

    /** Advances the retry state machine; retransmits every due transfer. */
    fun tick(now: Long = System.currentTimeMillis()): Int {
        var sent = 0
        for (tr in transfers.tick(now)) {
            transmit(tr.id, now)
            sent++
        }
        return sent
    }

    /** Transmits one attempt of a reliable transfer (fresh packet id + nonce). */
    private fun transmit(transferId: String, now: Long = System.currentTimeMillis()) {
        val tr = transfers.get(transferId) ?: return
        if (tr.state.isTerminal) return
        val payload = transfers.payload(transferId) ?: return
        if (payload.isEmpty()) return
        val sealed = MeshCrypto.encrypt(sessionKeyFor(tr.dst), payload)
        val p = MeshPacket(
            id = newId(),
            src = deviceId,
            dst = tr.dst,
            kind = tr.kind,
            ttl = maxHops,
            hops = 0,
            payload = sealed.ciphertext,
            nonce = sealed.nonce,
            createdAt = now,
            seq = nextSeq(),
            xfer = transferId,
        )
        seen[p.id] = now
        transfers.notePacket(transferId, p.id)
        enqueue(p)
        flush()
    }

    fun enqueue(p: MeshPacket) {
        pfifo.enqueue(p, nowMs = System.currentTimeMillis())
    }

    /** Packets still valid (unexpired, TTL remaining), in priority order. */
    fun pending(now: Long = System.currentTimeMillis()): List<MeshPacket> = pfifo.pending(now)

    fun dequeue(id: String) {
        pfifo.remove(id)
    }

    fun queueSize(): Int = pfifo.size()

    // ---- receiving -------------------------------------------------------

    /**
     * Handles a raw inbound datagram: a signed beacon registers a neighbour, a
     * revocation notice is verified and flooded, and a packet is deduplicated,
     * delivered locally when addressed to us (unicast or group), or forwarded.
     */
    fun handleInbound(addr: String, data: ByteArray, now: Long = System.currentTimeMillis()): MeshPacket? {
        // A network-wide revocation notice: verify it against the pinned
        // identities and apply it if valid, so a revocation propagates beyond
        // the node that issued it. Distinguished from beacons/packets by its
        // distinctive fields.
        MeshRevocation.unmarshal(data)?.let { r ->
            if (r.revoker.isNotEmpty() && r.deviceId.isNotEmpty() && r.sig.isNotEmpty()) {
                handleRevocation(r, now)
                return null
            }
        }
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
        if (!validatePacket(p)) return null

        if (seen.containsKey(p.id)) return null
        seen[p.id] = now
        if (seen.size > MAX_SEEN) pruneSeen()

        if (p.dst == deviceId || (p.dst.isEmpty() && p.groupId != null)) {
            dequeue(p.id)
            deliverLocal(p, now)
            return p
        }

        if (p.ttl <= 0) return null
        p.ttl -= 1
        p.hops += 1
        p.hopSrc = deviceId
        enqueue(p)
        flush()
        return p
    }

    /**
     * Applies the same structural envelope checks as Go Packet.Validate before
     * a packet can enter deduplication, decryption, or forwarding.
     * Authentication still happens in decryptPayload.
     */
    private fun validatePacket(p: MeshPacket): Boolean {
        if (p.id.isEmpty() || p.id.length > 128 || p.src.isEmpty() || p.src.length > 256) return false
        if (p.dst.length > 256 || (p.dst.isEmpty() && p.groupId.isNullOrEmpty())) return false
        if ((p.groupId?.length ?: 0) > 256 || p.ttl !in 0..1024 || p.hops !in 0..1024) return false
        if (p.payload.isNotEmpty() && p.nonce.size != 12) return false
        if (p.fragTotal > 1 && (p.fragId.isNullOrEmpty() || p.fragIndex !in 0 until p.fragTotal || p.fragSum?.size != 32)) return false
        if (p.fragTotal <= 1 && (p.fragIndex != 0 || p.fragTotal < 0)) return false
        if (p.kind == KIND_ACK && (p.ackFor.isNullOrEmpty() || p.dst.isEmpty())) return false
        return true
    }

    /** Application-layer delivery callback (decrypted packet). */
    var delivered: ((MeshPacket, ByteArray?) -> Unit)? = null

    /** Deliver-local path: acks are consumed; data is reassembled and delivered exactly once. */
    private fun deliverLocal(p: MeshPacket, now: Long) {
        // An acknowledgement settles a reliable transfer and is consumed here.
        if (p.kind == KIND_ACK) {
            val proof = decryptPayload(p) ?: return
            if (p.groupId != null) {
                if (String(proof) != "chatapp-mesh-groupack-v1:${p.groupId}:${p.ackFor}") return
                if (p.seq != 0L && !replay.check(p.src, p.seq)) return
                if (p.ackFor != null) {
                    groupAcks.ack(p.ackFor!!, p.src)
                    transfers.ack(p.ackFor!!)
                }
                return
            }
            if (String(proof) != "chatapp-mesh-ack-v1:${p.ackFor}") return
            if (p.seq != 0L && !replay.check(p.src, p.seq)) return
            if (p.ackFor != null) transfers.ack(p.ackFor!!)
            return
        }
        val pt = decryptPayload(p) ?: return
        // Anti-replay runs AFTER the payload authenticates: a forged packet
        // must never be able to advance or poison the replay window.
        if (p.seq != 0L && !replay.check(p.src, p.seq)) return
        // A fragmented payload is withheld from the application until every
        // part has arrived and the digest verifies.
        val full = frags.add(p, pt) ?: return
        // A reliable transfer delivers exactly once no matter how many copies
        // were retransmitted, and EVERY copy is acknowledged so a lost ACK
        // converges.
        val xferId = p.xfer
        if (xferId != null && xferId.isNotEmpty()) {
            if (transfers.markDelivered(xferId)) {
                delivered?.invoke(p, full)
            }
            if (p.groupId != null) sendGroupAck(p.src, p.groupId!!, xferId) else sendAck(p.src, xferId)
            return
        }
        // Best-effort packet: handleInbound's dedup cache guarantees the
        // first (and only) copy is the one being delivered here.
        delivered?.invoke(p, full)
    }

    /** Returns an acknowledgement for a settled unicast transfer to its origin. */
    private fun sendAck(dst: String, transferId: String) {
        if (dst.isEmpty() || transferId.isEmpty()) return
        val proof = "chatapp-mesh-ack-v1:$transferId".toByteArray()
        val sealed = MeshCrypto.encrypt(sessionKeyFor(dst), proof)
        val p = MeshPacket(
            id = newId(), src = deviceId, dst = dst, kind = KIND_ACK, ttl = maxHops,
            payload = sealed.ciphertext, nonce = sealed.nonce,
            createdAt = System.currentTimeMillis(), seq = nextSeq(),
            ackFor = transferId, xfer = transferId,
        )
        seen[p.id] = System.currentTimeMillis()
        enqueue(p)
        flush()
    }

    /** Returns a per-member acknowledgement for a group transfer to its origin. */
    private fun sendGroupAck(dst: String, groupId: String, transferId: String) {
        if (dst.isEmpty() || groupId.isEmpty() || transferId.isEmpty()) return
        val proof = "chatapp-mesh-groupack-v1:$groupId:$transferId".toByteArray()
        val sealed = MeshCrypto.encrypt(sessionKeyFor(dst), proof)
        val p = MeshPacket(
            id = newId(), src = deviceId, dst = dst, groupId = groupId, kind = KIND_ACK, ttl = maxHops,
            payload = sealed.ciphertext, nonce = sealed.nonce,
            createdAt = System.currentTimeMillis(), seq = nextSeq(),
            ackFor = transferId, xfer = transferId,
        )
        seen[p.id] = System.currentTimeMillis()
        enqueue(p)
        flush()
    }

    /**
     * Attempts to hand queued packets to a known neighbour. Packets stay queued
     * when no link can carry them yet — that is the store-and-forward path.
     */
    fun flush(): Int {
        var sent = 0
        val target = links.firstOrNull { it.isAvailable() } ?: return 0
        for (p in pending()) {
            for (nb in relayCandidates(p)) {
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

    // ---- revocation ------------------------------------------------------

    /**
     * Locally revokes a pinned device key and floods a signed, network-wide
     * notice so every relay applies the same decision. The expected public key
     * must match the pinned key so a device id alone cannot revoke an
     * unrelated identity.
     */
    fun revokePeer(deviceId: String, expectedPub: ByteArray): ByteArray? {
        val pinned = pinnedKeys[deviceId] ?: return null
        if (!pinned.contentEquals(expectedPub)) return null
        val r = MeshRevocation.sign(identity, this.deviceId, deviceId, expectedPub, System.currentTimeMillis())
        applyRevocation(r)
        val data = MeshRevocation.marshal(r)
        // Flood to every known relay so the notice propagates.
        for (nb in neighbors.values) {
            if (nb.relayOk) {
                val flood = MeshPacket(
                    id = newId(), src = this.deviceId, dst = nb.deviceId, kind = KIND_MESSAGE,
                    ttl = maxHops, payload = data, nonce = ByteArray(0),
                    createdAt = System.currentTimeMillis(), seq = nextSeq(),
                )
                enqueue(flood)
            }
        }
        flush()
        return data
    }

    /** Verifies and applies a flooded revocation notice, then propagates it. */
    private fun handleRevocation(r: RevocationNotice, now: Long) {
        val verified = MeshRevocation.verify(r, pinnedKeys) ?: return
        if (!revStore.apply(verified)) return
        applyRevocation(verified)
        // Propagate the notice to every other relay neighbour.
        val data = MeshRevocation.marshal(verified)
        for (nb in neighbors.values) {
            if (nb.relayOk && nb.deviceId != verified.revoker) {
                val flood = MeshPacket(
                    id = newId(), src = deviceId, dst = nb.deviceId, kind = KIND_MESSAGE,
                    ttl = maxHops, payload = data, nonce = ByteArray(0),
                    createdAt = now, seq = nextSeq(),
                )
                enqueue(flood)
            }
        }
        flush()
    }

    /** Applies a verified revocation locally. */
    private fun applyRevocation(r: RevocationNotice) {
        revokedKeys[r.deviceId] = r.publicKey
        peerKEM.remove(r.deviceId)
        neighbors.remove(r.deviceId)
        transfers.dropTo(r.deviceId)
    }

    /** Whether this node has revoked a peer identity (locally or flooded). */
    fun isPeerRevoked(deviceId: String): Boolean = revokedKeys.containsKey(deviceId)

    // ---- group keys ------------------------------------------------------

    /** Installs a group key received from the group owner (epoch-monotonic). */
    fun adoptGroupKey(groupId: String, key: ByteArray, epoch: Long): Boolean =
        groupKeys.adoptKey(groupId, key, epoch)

    /** Forces a rotation for a group (evicting a member is done by NOT telling them). */
    fun rotateGroupKey(groupId: String): GroupKey = groupKeys.rotateGroup(groupId)

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
        if (p.groupId != null && p.dst.isEmpty()) {
            val gk = groupKeys.keyFor(p.groupId!!)
            MeshCrypto.decrypt(gk.key, p.payload, p.nonce)?.let { return it }
        }
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

        /** A neighbour beacon expires after three minutes (services/mesh/node.go). */
        const val NEIGHBOUR_TTL_MS = 3 * 60 * 1000L

        /** Envelope version this engine stamps; newer formats are rejected. */
        const val MESH_VERSION = 1

        /** Fragment payload ceiling, matching services/mesh/fragment.go. */
        const val DEFAULT_MAX_PAYLOAD = 512

        /** Reliable-transfer ceiling, matching services/mesh/reliability.go. */
        const val MAX_RELIABLE_PAYLOAD = 64 * 1024

        /** Forwarding-buffer byte bound, matching services/mesh/pfifo.go. */
        const val DEFAULT_QUEUE_MAX_BYTES = 8 * 1024 * 1024

        const val KIND_MESSAGE = "message"
        const val KIND_GROUP_MESSAGE = "group_message"
        const val KIND_VOICE_MESSAGE = "voice_message"
        const val KIND_CALL_SIGNAL = "call_signal"
        const val KIND_CALL_MEDIA = "call_media"
        const val KIND_CALL_FEC = "call_fec"
        const val KIND_CALL_PING = "call_ping"
        const val KIND_CALL_PONG = "call_pong"
        const val KIND_CALL_BYE = "call_bye"
        const val KIND_ACK = "ack"

        /** Kinds the engine accepts, matching services/mesh Validate(). */
        val VALID_KINDS = setOf(
            KIND_MESSAGE, KIND_GROUP_MESSAGE, KIND_VOICE_MESSAGE, KIND_CALL_SIGNAL,
            KIND_CALL_MEDIA, KIND_CALL_FEC, KIND_CALL_PING, KIND_CALL_PONG,
            KIND_CALL_BYE, KIND_ACK,
        )

        fun sha256(data: ByteArray): ByteArray = MessageDigest.getInstance("SHA-256").digest(data)

        /** Fresh 32-hex-char id for a reliable transfer. */
        fun newTransferId(): String {
            val b = ByteArray(16)
            SecureRandom().nextBytes(b)
            return b.joinToString("") { "%02x".format(it) }
        }
    }
}

/** Packet wire codec — JSON, matching the Go engine's marshal layout. */
object MeshPacketCodec {
    private fun b64(b: ByteArray): String = java.util.Base64.getEncoder().encodeToString(b)
    private fun unB64(s: String): ByteArray = java.util.Base64.getDecoder().decode(s)

    fun encode(p: MeshPacket): ByteArray = JSONObject()
        .put("id", p.id)
        .put("v", p.version)
        .put("src", p.src)
        .put("hop_src", p.hopSrc ?: JSONObject.NULL)
        .put("dst", p.dst)
        .put("group_id", p.groupId ?: JSONObject.NULL)
        .put("kind", p.kind)
        .put("ttl", p.ttl)
        .put("hops", p.hops)
        .put("payload", b64(p.payload))
        .put("nonce", b64(p.nonce))
        .put("created_at", p.createdAt)
        .put("seq", p.seq)
        .put("frag_index", p.fragIndex)
        .put("frag_total", p.fragTotal)
        .put("frag_id", p.fragId ?: JSONObject.NULL)
        .put("frag_sum", p.fragSum?.let { b64(it) } ?: JSONObject.NULL)
        .put("ack_for", p.ackFor ?: JSONObject.NULL)
        .put("xfer", p.xfer ?: JSONObject.NULL)
        .toString()
        .toByteArray()

    fun decode(data: ByteArray): MeshPacket? = try {
        val o = JSONObject(String(data))
        // Reject a future envelope format before any other parsing work.
        val version = o.optInt("v", 0)
        if (version > MeshEngine.MESH_VERSION) null else {
            val kind = o.optString("kind", "message")
            val fragTotal = o.optInt("frag_total", 0)
            val fragIndex = o.optInt("frag_index", 0)
            if (kind !in MeshEngine.VALID_KINDS) null
            else if (fragTotal > 1 && (o.optString("frag_id", "").isEmpty() || fragIndex < 0 || fragIndex >= fragTotal)) null
            else MeshPacket(
                id = o.getString("id"),
                src = o.getString("src"),
                dst = o.optString("dst", ""),
                groupId = o.optString("group_id", "").takeUnless { it.isEmpty() || it == "null" },
                kind = kind,
                ttl = o.optInt("ttl", MeshEngine.DEFAULT_MAX_HOPS),
                hops = o.optInt("hops", 0),
                payload = unB64(o.optString("payload")),
                nonce = unB64(o.optString("nonce")),
                createdAt = o.optLong("created_at", System.currentTimeMillis()),
                seq = o.optLong("seq", 0),
                version = version,
                hopSrc = o.optString("hop_src", "").takeUnless { it.isEmpty() || it == "null" },
                ackFor = o.optString("ack_for", "").takeUnless { it.isEmpty() || it == "null" },
                xfer = o.optString("xfer", "").takeUnless { it.isEmpty() || it == "null" },
                fragIndex = fragIndex,
                fragTotal = fragTotal,
                fragId = o.optString("frag_id", "").takeUnless { it.isEmpty() || it == "null" },
                fragSum = o.optString("frag_sum", "").takeUnless { it.isEmpty() || it == "null" }?.let { unB64(it) },
            )
        }
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
class NativeMeshManager(private val context: android.content.Context, private val deviceId: String, private val key: ByteArray) {

    val engine = MeshEngine(deviceId, key)

    private val wifiDirect = WifiDirectLink(context) { addr, data -> engine.handleInbound(addr, data) }
    private val localWifi = LocalWifiLink({ addr, data -> engine.handleInbound(addr, data) })
    private val bluetooth = BluetoothLink(context) { addr, data -> engine.handleInbound(addr, data) }

    private val allLinks = listOf(wifiDirect, localWifi, bluetooth)
    private val startedLinks = HashSet<String>()
    private var supervisor: Thread? = null

    @Volatile private var supervising = false

    /**
     * Starts the chain in Anonymous.md §5.3 order. Links that the platform
     * reports unavailable are skipped rather than failing the whole mesh.
     * A supervision loop then keeps the chain alive for the rest of the
     * session: radios and permissions come and go (user toggles Bluetooth,
     * grants location later, Wi-Fi Direct re-forms its group), and a link
     * that was skipped at start must come up the moment it can.
     */
    fun start() {
        engine.attach(wifiDirect, localWifi, bluetooth)
        startedLinks.clear()
        allLinks.forEach { if (it.isAvailable()) { it.start(); startedLinks.add(it.kind) } }
        startSupervisor()
    }

    fun stop() {
        supervising = false
        supervisor?.interrupt()
        supervisor = null
        engine.stop()
        startedLinks.clear()
    }

    /**
     * Supervision loop (§4: automatic radio permission/discovery/reconnect
     * state machines). Every cycle: start links that became available, stop
     * links that did, ask the Bluetooth link to reconnect any discovered peer
     * whose backoff has elapsed, and re-evaluate the active transport.
     */
    private fun startSupervisor() {
        if (supervising) return
        supervising = true
        supervisor = Thread {
            while (supervising) {
                try {
                    for (link in allLinks) {
                        val available = try { link.isAvailable() } catch (_: Exception) { false }
                        val isStarted = link.kind in startedLinks
                        if (available && !isStarted) {
                            try { link.start(); startedLinks.add(link.kind) } catch (_: Exception) { }
                        } else if (!available && isStarted) {
                            try { link.stop() } catch (_: Exception) { }
                            startedLinks.remove(link.kind)
                        }
                    }
                    try { bluetooth.reconnectKnown() } catch (_: Exception) { }
                    engine.refreshTransport()
                } catch (_: InterruptedException) {
                    break
                } catch (_: Exception) {
                    // A supervision error must never kill the loop.
                }
                try {
                    Thread.sleep(SUPERVISOR_INTERVAL_MS)
                } catch (_: InterruptedException) {
                    break
                }
            }
        }.also { it.name = "mesh-supervisor"; it.isDaemon = true; it.start() }
    }

    /** Current transport state for the UI / API status surface. */
    fun status(): JSONObject = JSONObject()
        .put("transport", engine.activeTransport)
        .put("relay_ok", engine.relayOk)
        .put("peers", engine.neighborList().size)
        .put("pending", engine.queueSize())
        .put("links", org.json.JSONArray().apply {
            allLinks.forEach { put(JSONObject().put("kind", it.kind).put("available", try { it.isAvailable() } catch (_: Exception) { false }).put("started", it.kind in startedLinks)) }
        })

    private companion object {
        const val SUPERVISOR_INTERVAL_MS = 10_000L
    }
}
