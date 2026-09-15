package com.chatapp.mesh

// MeshGroupAck.kt — per-member acknowledgements for group messages.
//
// Mirrors services/mesh/groupack.go: a group message carries a transfer id;
// each member that receives it returns a signed acknowledgement naming the
// group and the transfer id, and the sender tracks how many members have
// acknowledged. The aggregate state is user-visible (pending / partial /
// complete).

enum class GroupAckState(val label: String) {
    PENDING("pending"), PARTIAL("partial"), COMPLETE("complete");
}

/** Aggregate delivery state of one group transfer. */
class GroupTransfer(
    val id: String,
    val groupId: String,
    val members: List<String>,
    val acked: MutableMap<String, Boolean> = HashMap(),
    var state: GroupAckState = GroupAckState.PENDING,
    val createdAt: Long,
)

class GroupAckTracker(private val maxActive: Int = 1024) {

    private val lock = Any()
    private val active = LinkedHashMap<String, GroupTransfer>()

    /** Registers a group transfer for the given members. */
    fun create(id: String, groupId: String, members: List<String>): GroupTransfer = synchronized(lock) {
        val tr = GroupTransfer(id = id, groupId = groupId, members = members.toList(), createdAt = System.currentTimeMillis())
        active[id] = tr
        while (active.size > maxActive) {
            val oldest = active.keys.firstOrNull() ?: break
            active.remove(oldest)
        }
        tr
    }

    /**
     * Records a member's acknowledgement and recomputes the aggregate state.
     * Reports whether the aggregate state changed.
     */
    fun ack(id: String, member: String): Boolean = synchronized(lock) {
        val tr = active[id] ?: return false
        if (tr.acked[member] == true) return false
        tr.acked[member] = true
        val prev = tr.state
        val ackedCount = tr.members.count { tr.acked[it] == true }
        tr.state = when {
            ackedCount == 0 -> GroupAckState.PENDING
            ackedCount >= tr.members.size -> GroupAckState.COMPLETE
            else -> GroupAckState.PARTIAL
        }
        tr.state != prev
    }

    /** Returns a snapshot of one group transfer's state, if known. */
    fun get(id: String): GroupTransfer? = synchronized(lock) { active[id] }

    /** Snapshots every tracked group transfer, oldest first. */
    fun snapshot(): List<GroupTransfer> = synchronized(lock) { active.values.toList() }
}
