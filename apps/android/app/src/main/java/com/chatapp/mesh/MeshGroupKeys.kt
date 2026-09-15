package com.chatapp.mesh

import java.security.SecureRandom

// MeshGroupKeys.kt — per-group sender keys with scheduled and forced rotation.
//
// Mirrors services/mesh/groupkey.go: one symmetric key per group, rotated on
// schedule (or immediately to evict a member), and adopted from a group
// owner's advertisement epoch-monotonically so a stale advertisement cannot
// roll a group back to a revoked key.

data class GroupKey(val key: ByteArray, val epoch: Long, val createdAt: Long)

private class GroupKeyEntry(val entryKey: GroupKey, val adoptedAt: Long)

class GroupKeyManager(rotationIntervalMs: Long = 24L * 60 * 60 * 1000) {

    private val lock = Any()
    private val groups = HashMap<String, GroupKeyEntry>()
    private val rotationInterval: Long = if (rotationIntervalMs <= 0) 24L * 60 * 60 * 1000 else rotationIntervalMs

    /** Returns the current key for a group, rotating it first when due. */
    fun keyFor(groupId: String): GroupKey = synchronized(lock) {
        val now = System.currentTimeMillis()
        val e = groups[groupId]
        if (e == null) {
            val k = newGroupKey(1)
            groups[groupId] = GroupKeyEntry(k, now)
            return k
        }
        if (now - e.entryKey.createdAt >= rotationInterval) {
            val k = newGroupKey(e.entryKey.epoch + 1)
            groups[groupId] = GroupKeyEntry(k, now)
            return k
        }
        e.entryKey
    }

    /** Forces a rotation for a group; a member never told of the new key is evicted. */
    fun rotateGroup(groupId: String): GroupKey = synchronized(lock) {
        val now = System.currentTimeMillis()
        val e = groups[groupId]
        val k = newGroupKey((e?.entryKey?.epoch ?: 0) + 1)
        groups[groupId] = GroupKeyEntry(k, now)
        k
    }

    /** Installs a key from a group-key advertisement; newer epochs only. */
    fun adoptKey(groupId: String, key: ByteArray, epoch: Long): Boolean = synchronized(lock) {
        if (groupId.isEmpty()) return false
        val e = groups[groupId]
        // A stale or equal epoch is ignored (never rolls back), reported as
        // "not adopted".
        if (e != null && epoch <= e.entryKey.epoch) return false
        groups[groupId] = GroupKeyEntry(GroupKey(key.copyOf(), epoch, System.currentTimeMillis()), System.currentTimeMillis())
        true
    }

    /** Returns the current epoch for a group, or 0 if the group is unknown. */
    fun epoch(groupId: String): Long = synchronized(lock) { groups[groupId]?.entryKey?.epoch ?: 0 }

    /** The set of group ids this node holds keys for. */
    fun groupIds(): List<String> = synchronized(lock) { ArrayList(groups.keys) }

    private fun newGroupKey(epoch: Long): GroupKey {
        val b = ByteArray(32)
        SecureRandom().nextBytes(b)
        return GroupKey(b, epoch, System.currentTimeMillis())
    }
}
