package com.chatapp.mesh

// MeshParityTest.kt — JVM parity tests for the native mesh engine, mirroring
// services/mesh/gaps_test.go (reliability, fragmentation, group keys, group
// acks and network-wide revocation distribution). Not annotated with JUnit:
// run with `kotlinc -cp json.jar ... && java -cp ... MeshParityTestKt`, or
// wire into a Gradle test task by adding assertions-as-tests annotations.
//
// The tests wire two engines through a loopback link whose `send` delivers
// directly to the peer's handleInbound, mirroring the Go sim bus.

import java.security.SecureRandom

/** Loopback link that delivers wire bytes straight to the peer engine. */
class LoopbackLink(val localId: String, val peer: () -> MeshEngine?) : MeshLink {
    val sent = ArrayList<Pair<String, ByteArray>>()
    override val kind = "loopback"
    override fun isAvailable() = true
    override fun start() {}
    override fun stop() {}
    override fun send(addr: String, data: ByteArray): Boolean {
        sent.add(addr to data)
        peer()?.handleInbound(addr, data)
        return true
    }
    fun deliverTo(peerEngine: MeshEngine) {
        for ((addr, data) in sent) peerEngine.handleInbound(addr, data)
        sent.clear()
    }
}

fun newKey(): ByteArray = ByteArray(32).also { SecureRandom().nextBytes(it) }

class Harness(val id: String, val key: ByteArray) {
    var peerEngine: MeshEngine? = null
    val link = LoopbackLink(id) { peerEngine }
    val engine = MeshEngine(id, key)
    val received = ArrayList<Pair<MeshPacket, ByteArray>>()

    init {
        engine.attach(link)
        engine.delivered = { p, pt -> received.add(p to (pt ?: ByteArray(0))) }
    }
}

fun wirePair(aKey: ByteArray = newKey(), bKey: ByteArray = aKey): Pair<Harness, Harness> {
    val a = Harness("dev-a", aKey)
    val b = Harness("dev-b", bKey)
    // Cross-wire the loopback links: each link delivers to the OTHER engine.
    a.peerEngine = b.engine
    b.peerEngine = a.engine
    a.engine.upsertNeighbor("dev-b", "addr-b", "loopback")
    b.engine.upsertNeighbor("dev-a", "addr-a", "loopback")
    return a to b
}

fun testEnvelopeVersionGate() {
    val (a, _) = wirePair()
    val p = MeshPacket(
        id = "p1", src = "dev-a", dst = "dev-b", kind = MeshEngine.KIND_MESSAGE,
        ttl = 8, payload = "hi".toByteArray(), nonce = ByteArray(12),
        createdAt = 1, seq = 1,
    )
    val wire = MeshPacketCodec.encode(p)
    check(String(wire).contains("\"v\":1")) { "encode must stamp v=1" }
    check(MeshPacketCodec.decode(wire) != null) { "v=1 decodes" }
    val future = String(wire).replace("\"v\":1", "\"v\":2").toByteArray()
    check(MeshPacketCodec.decode(future) == null) { "future envelope version must be rejected" }
    val legacy = String(wire).replace("\"v\":1,", "").toByteArray()
    check(MeshPacketCodec.decode(legacy) != null) { "legacy (no v) decodes" }
}

fun testKindValidation() {
    val (_, _) = wirePair()
    val wire = """{"id":"x","src":"a","dst":"b","kind":"bogus","ttl":4,"hops":0,
        "payload":"","nonce":"","created_at":1,"seq":0}""".toByteArray()
    check(MeshPacketCodec.decode(wire) == null) { "unknown kind must be rejected" }
}

fun testReliableUnicastRoundTrip() {
    val (a, b) = wirePair()
    val xfer = a.engine.sendReliable(MeshEngine.KIND_MESSAGE, "dev-b", "reliable hi".toByteArray())
    // The loopback link delivers synchronously, so the ack may already have
    // settled the transfer by this point.
    check(a.engine.transfers.get(xfer)!!.state != TransferState.QUEUED) { "transfer in flight" }
    check(b.received.size == 1 && String(b.received[0].second) == "reliable hi") { "delivered" }
    check(a.engine.transfers.get(xfer)!!.state == TransferState.ACKED) { "ack settled the transfer" }
    // A duplicate copy (retry) must not re-deliver but must still be acked.
    val dup = MeshPacket(
        id = "dup-1", src = "dev-a", dst = "dev-b", kind = MeshEngine.KIND_MESSAGE,
        ttl = 8, payload = "reliable hi".toByteArray(),
        nonce = ByteArray(12), createdAt = 1, seq = 2, xfer = xfer,
    )
    val sealed = MeshCrypto.encrypt(a.key, "reliable hi".toByteArray())
    val dupSealed = dup.copy(payload = sealed.ciphertext, nonce = sealed.nonce)
    b.engine.handleInbound("addr-a", MeshPacketCodec.encode(dupSealed))
    check(b.received.size == 1) { "retry collapsed to exactly-once" }
}

fun testRetryStateMachine() {
    // No peer wired: the ack never arrives, so the retry machine is observable.
    val a = Harness("dev-a", newKey())
    val engine = a.engine
    val now = System.currentTimeMillis()
    val xfer = engine.sendReliable(MeshEngine.KIND_MESSAGE, "dev-b", "retry me".toByteArray())
    check(engine.tick(now + 1000) == 0) { "not due yet" }
    check(engine.tick(now + 7_000) == 1) { "ack timeout elapsed: retransmit" }
    check(a.engine.transfers.get(xfer)!!.attempts == 2) { "attempt counted" }
    check(a.engine.transfers.get(xfer)!!.state == TransferState.RELAYING)
    engine.transfers.ack(xfer)
    check(engine.tick(now + 90_000) == 0) { "terminal transfers are not retried" }
}

fun testTransferDeadLetter() {
    val tracker = TransferTracker(ackTimeoutMs = 100, maxAttempts = 2, ttlMs = 1_000_000)
    val now = System.currentTimeMillis()
    val tr = tracker.create("dst", "message", "p".toByteArray())
    tracker.tick(now)
    tracker.tick(now + 200)
    tracker.tick(now + 400)
    check(tracker.get(tr.id)!!.state == TransferState.DEAD_LETTER) { "retry budget exhausted -> dead letter" }
}

fun testFragmentationRoundTrip() {
    val (a, b) = wirePair()
    val payload = ByteArray(1500) { ('a' + (it % 26)).code.toByte() }
    val fragId = a.engine.sendLarge(MeshEngine.KIND_MESSAGE, "dev-b", payload, group = false)
    check(fragId.isNotEmpty()) { "fragment group id returned" }
    check(b.received.size == 1 && b.received[0].second.contentEquals(payload)) {
        "reassembled payload matches and delivered once"
    }
    check(a.engine.pending().isEmpty()) { "all fragments drained" }
}

fun testFragmentAssemblerBounded() {
    val assembler = FragmentAssembler(maxOpen = 2)
    val sum = MeshEngine.sha256(ByteArray(10))
    fun frag(idx: Int, total: Int, id: String, src: String) = MeshPacket(
        id = "f$idx", src = src, dst = "me", kind = "message", ttl = 8,
        payload = ByteArray(4), nonce = ByteArray(12), createdAt = 1, seq = 1,
        fragIndex = idx, fragTotal = total, fragId = id, fragSum = sum,
    )
    check(assembler.add(frag(0, 2, "g1", "s"), ByteArray(4)) == null) { "incomplete" }
    check(assembler.add(frag(0, 2, "g2", "s"), ByteArray(4)) == null) { "incomplete" }
    check(assembler.add(frag(1, 2, "g3", "s"), ByteArray(4)) == null) { "new group refused at cap" }
    check(assembler.openCount() == 2) { "bounded open groups" }
}

fun testGroupKeysRotationAndAdoption() {
    val m = GroupKeyManager(rotationIntervalMs = 10)
    val k1 = m.keyFor("g1")
    check(k1.epoch == 1L)
    check(!m.adoptKey("g1", ByteArray(32) { 1 }, 0)) { "stale epoch ignored" }
    check(m.adoptKey("g1", ByteArray(32) { 2 }, 5)) { "newer epoch adopted" }
    check(m.epoch("g1") == 5L)
    Thread.sleep(15)
    val k2 = m.keyFor("g1") // rotated past interval -> epoch 6
    check(k2.epoch == 6L) { "scheduled rotation bumps epoch" }
    val k3 = m.rotateGroup("g1")
    check(k3.epoch == 7L) { "forced rotation" }
}

fun testGroupAckEndToEnd() {
    val (a, b) = wirePair()
    val gk = a.engine.groupKeys.keyFor("g1")
    check(b.engine.adoptGroupKey("g1", gk.key, gk.epoch)) { "member adopts the group key" }
    a.engine.trackGroupMembers("g1", listOf("dev-b"))
    val xfer = a.engine.sendGroup(MeshEngine.KIND_GROUP_MESSAGE, "g1", "hello group".toByteArray())
    check(b.received.size == 1 && String(b.received[0].second) == "hello group") { "member decrypted group payload" }
    val state = a.engine.groupAcks.get(xfer)
    check(state != null && state.state == GroupAckState.COMPLETE) {
        "member group ack settled: ${state?.state}"
    }
}

fun testGroupDeliveryWithoutRosterStillDelivers() {
    val (a, b) = wirePair()
    val gk = a.engine.groupKeys.keyFor("g2")
    b.engine.adoptGroupKey("g2", gk.key, gk.epoch)
    a.engine.sendGroup(MeshEngine.KIND_GROUP_MESSAGE, "g2", "no roster".toByteArray())
    check(b.received.size == 1) { "group message delivered without roster tracking" }
}

fun testRevocationDistribution() {
    val (a, b) = wirePair()
    // Seed beacons so A and B pin each other (TOFU).
    val beaconA = a.engine.beacon(1)
    val beaconB = b.engine.beacon(1)
    check(a.engine.handleInbound("addr-b", beaconB) == null) { "beacon processed" }
    check(b.engine.handleInbound("addr-a", beaconA) == null)
    check(a.engine.neighborList().any { it.deviceId == "dev-b" }) { "A pinned B" }
    val bPub = b.engine.identity.signPublic()
    val notice = a.engine.revokePeer("dev-b", bPub)
    check(notice != null) { "revocation notice signed and flooded" }
    check(a.engine.isPeerRevoked("dev-b")) { "revoker applied locally" }
    // A third node (C) pinned both A and B; the flooded notice must apply.
    val (c, _) = wirePair()
    c.engine.handleInbound("addr-a", a.engine.beacon(2))
    c.engine.handleInbound("addr-b", b.engine.beacon(2))
    check(c.engine.handleInbound("addr-a", notice!!) == null)
    check(c.engine.isPeerRevoked("dev-b")) { "network-wide revocation applied at C" }
    check(c.engine.neighborList().none { it.deviceId == "dev-b" }) { "C no longer routes to B" }
    // A tampered notice (wrong key) is rejected.
    val forged = MeshRevocation.unmarshal(notice)!!.copy(publicKey = ByteArray(32))
    val forgedWire = MeshRevocation.marshal(forged)
    val (c2, _) = wirePair()
    c2.engine.handleInbound("addr-a", a.engine.beacon(3))
    c2.engine.handleInbound("addr-x", forgedWire)
    check(!c2.engine.isPeerRevoked("dev-b")) { "forged notice rejected" }
}

fun testPriorityQueueOrderingAndBounds() {
    val q = MeshPriorityQueue(maxPackets = 3, maxBytes = 1 shl 20)
    fun p(id: String, kind: String) = MeshPacket(
        id = id, src = "s", dst = "d", kind = kind, ttl = 8,
        payload = ByteArray(16), nonce = ByteArray(12), createdAt = 1, seq = 1,
    )
    q.enqueue(p("m1", "voice_message"), nowMs = 1)
    q.enqueue(p("m2", "call_signal"), nowMs = 2)
    q.enqueue(p("m3", "message"), nowMs = 3)
    val order = q.pending(now = 10).map { it.id }
    check(order == listOf("m2", "m3", "m1")) { "control first, text, then voice: $order" }
    // Overflow drops the lowest-priority packet, never control.
    q.enqueue(p("m4", "message"), nowMs = 4)
    val ids = q.pending(now = 10).map { it.id }
    check(!ids.contains("m1")) { "voice dropped under pressure" }
    check(ids.contains("m2")) { "control retained" }
}

fun main() {
    val tests = listOf(
        ::testEnvelopeVersionGate, ::testKindValidation, ::testReliableUnicastRoundTrip,
        ::testRetryStateMachine, ::testTransferDeadLetter, ::testFragmentationRoundTrip,
        ::testFragmentAssemblerBounded, ::testGroupKeysRotationAndAdoption,
        ::testGroupAckEndToEnd, ::testGroupDeliveryWithoutRosterStillDelivers,
        ::testRevocationDistribution, ::testPriorityQueueOrderingAndBounds,
    )
    var failed = 0
    for (t in tests) {
        try {
            t()
            println("PASS ${t.name}")
        } catch (e: Throwable) {
            failed++
            println("FAIL ${t.name}: ${e.message}")
        }
    }
    if (failed > 0) {
        println("$failed test(s) failed")
        kotlin.system.exitProcess(1)
    }
    println("all ${tests.size} mesh parity tests passed")
}
