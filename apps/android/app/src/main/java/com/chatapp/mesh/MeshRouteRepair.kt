package com.chatapp.mesh

// MeshRouteRepair.kt — proactive route repair for the mesh.
//
// Mirrors services/mesh/routerepair.go and routerepair_states.go. The engine
// previously reacted to a failed link only by quarantining the neighbour for a
// cooldown. This module adds proactive route repair: when a neighbour fails,
// the node immediately re-selects the best remaining relay and re-queues the
// packet toward it, so a single dead link does not stall a transfer until the
// next discovery cycle. A companion repair state machine tracks per-destination
// probe attempts, backoff and cooldown transitions.

/** Ordered repair phases for a destination; mirrors Go RepairState. */
enum class RepairState {
    IDLE, PROBING, ACTIVE, COOLDOWN;

    companion object {
        fun fromGo(s: String): RepairState = when (s) {
            "idle" -> IDLE; "probing" -> PROBING
            "active" -> ACTIVE; "cooldown" -> COOLDOWN
            else -> IDLE
        }
    }
}

/** Per-destination repair entry. */
data class RepairEntry(
    val dest: String,
    var state: RepairState = RepairState.IDLE,
    var fails: Int = 0,
    var attempts: Int = 0,
    var lastProbe: Long = 0,
    var lastFail: Long = 0,
    var activeVia: String = "",
    var enteredAt: Long = 0,
)

/** Per-destination repair state machine, exact mirror of Go repairSM. */
class RepairStateMachine(
    private val maxProbes: Int = 5,
    private val probeBackoffMs: Int = 200,
    private val cooldownMs: Int = 30_000,
) {
    private val lock = Any()
    private val entries = HashMap<String, RepairEntry>()

    /** Called on a link failure; returns the resulting state. */
    fun onLinkFailure(dest: String, failedNb: String): RepairState = synchronized(lock) {
        val entry = entries.getOrPut(dest) { RepairEntry(dest) }
        val now = System.currentTimeMillis()
        when (entry.state) {
            RepairState.IDLE -> {
                entry.state = RepairState.PROBING
                entry.attempts = 1
                entry.lastProbe = now
                entry.lastFail = now
                entry.enteredAt = now
            }
            RepairState.PROBING -> {
                if (entry.attempts >= maxProbes) {
                    entry.state = RepairState.COOLDOWN
                    entry.enteredAt = now
                } else {
                    val backoff = (probeBackoffMs * entry.attempts).toLong()
                    if (now - entry.lastProbe >= backoff) {
                        entry.attempts++
                        entry.lastProbe = now
                    }
                }
            }
            RepairState.ACTIVE -> {
                entry.state = RepairState.PROBING
                entry.attempts = 1
                entry.lastProbe = now
                entry.lastFail = now
                entry.activeVia = ""
                entry.enteredAt = now
            }
            RepairState.COOLDOWN -> { /* stay cool */ }
        }
        entry.fails++
        entry.lastFail = now
        entry.state
    }

    fun onProbeSuccess(dest: String, via: String): RepairState = synchronized(lock) {
        val entry = entries[dest] ?: return RepairState.IDLE
        entry.state = RepairState.ACTIVE
        entry.activeVia = via
        entry.enteredAt = System.currentTimeMillis()
        entry.state
    }

    fun onLinkRecovered(dest: String) = synchronized(lock) {
        entries[dest]?.let {
            it.state = RepairState.IDLE
            it.activeVia = ""
            it.attempts = 0
            it.fails = 0
        }
    }

    fun tick(nowMs: Long): List<String> = synchronized(lock) {
        val recovered = mutableListOf<String>()
        val iter = entries.iterator()
        while (iter.hasNext()) {
            val (dest, entry) = iter.next()
            if (entry.state == RepairState.COOLDOWN &&
                nowMs - entry.enteredAt >= cooldownMs
            ) {
                entry.state = RepairState.IDLE
                entry.attempts = 0
                recovered.add(dest)
            }
        }
        recovered
    }

    fun get(dest: String): RepairEntry? = synchronized(lock) { entries[dest] }

    fun snapshot(): List<RepairEntry> = synchronized(lock) {
        entries.values.toList()
    }
}

/** Route-repair helper: re-selects the best remaining relay for a packet.
 *  Mirrors the Node.RepairRoute method in routerepair.go. */
object RouteRepair {

    /** Re-routes a packet after a failed hop. Returns the chosen neighbour's
     *  address, or null when no alternate route exists yet (store-and-forward). */
    fun repairRoute(
        dst: String,
        failedNeighbor: String,
        neighbors: List<MeshNeighbor>,
        maxFanout: Int,
    ): MeshNeighbor? {
        val now = System.currentTimeMillis()
        if (dst.isNotEmpty()) {
            val direct = neighbors.firstOrNull {
                it.deviceId == dst && now >= it.lastSeen.time + 60_000 // not quarantined
            }
            if (direct != null) return direct
        }
        val candidates = neighbors
            .filter { it.deviceId != failedNeighbor && it.deviceId != dst && it.relayOk }
            .sortedByDescending { it.lastSeen.time }
        return candidates.firstOrNull()
    }
}