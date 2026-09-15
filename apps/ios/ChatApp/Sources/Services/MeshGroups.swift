import Foundation

// MeshGroups.swift — per-group sender keys with scheduled and forced rotation,
// and per-member acknowledgements for group messages.
//
// Mirrors services/mesh (groupkey.go, groupack.go) and the Android
// MeshGroupKeys.kt / MeshGroupAck.kt. A group message is sealed under the
// group's sender key, not a per-recipient key, so an evicted member cannot
// read it. Keys rotate on a schedule or on demand; adoption is epoch-
// monotonic, so a stale advertisement can never roll a group back to a
// revoked key. Every group message carries a transfer id so members can
// acknowledge it; the sender tracks how many members acknowledged
// (pending -> partial -> complete).

struct GroupKey {
    var key: Data
    var epoch: Int64
    var createdAt: Int64
}

final class GroupKeyManager {
    private let rotationIntervalMs: Int64

    private final class Entry {
        var key: GroupKey
        init(_ k: GroupKey) { key = k }
    }

    private let lock = NSLock()
    private var groups: [String: Entry] = [:]

    init(rotationIntervalMs: Int64 = 24 * 60 * 60 * 1000) {
        self.rotationIntervalMs = rotationIntervalMs > 0 ? rotationIntervalMs : 24 * 60 * 60 * 1000
    }

    private func nowMs() -> Int64 { Int64(Date().timeIntervalSince1970 * 1000) }

    /// Returns the current key for a group, rotating it first when due.
    func keyFor(_ groupId: String) -> GroupKey {
        precondition(!groupId.isEmpty, "mesh: group id is required")
        lock.lock(); defer { lock.unlock() }
        let now = nowMs()
        guard let e = groups[groupId] else {
            let k = newGroupKey(1)
            groups[groupId] = Entry(k)
            return k
        }
        if now - e.key.createdAt >= rotationIntervalMs {
            let k = newGroupKey(e.key.epoch + 1)
            groups[groupId] = Entry(k)
            return k
        }
        return e.key
    }

    /// Forces a rotation; a member never told of the new key is evicted.
    func rotateGroup(_ groupId: String) -> GroupKey {
        precondition(!groupId.isEmpty, "mesh: group id is required")
        lock.lock(); defer { lock.unlock() }
        let prev = groups[groupId]?.key.epoch ?? 0
        let k = newGroupKey(prev + 1)
        groups[groupId] = Entry(k)
        return k
    }

    /// Installs a key from a group-key advertisement; newer epochs only.
    /// Reports false when a stale or equal epoch was ignored.
    @discardableResult
    func adoptKey(_ groupId: String, key: Data, epoch: Int64) -> Bool {
        guard !groupId.isEmpty else { return false }
        lock.lock(); defer { lock.unlock() }
        if let e = groups[groupId], epoch <= e.key.epoch { return false }
        groups[groupId] = Entry(GroupKey(key: key, epoch: epoch, createdAt: nowMs()))
        return true
    }

    /// The current epoch for a group, or 0 when the group is unknown.
    func epoch(_ groupId: String) -> Int64 {
        lock.lock(); defer { lock.unlock() }
        return groups[groupId]?.key.epoch ?? 0
    }

    /// The set of group ids this node holds keys for.
    func groupIds() -> [String] {
        lock.lock(); defer { lock.unlock() }
        return Array(groups.keys)
    }

    private func newGroupKey(_ epoch: Int64) -> GroupKey {
        GroupKey(key: MeshCrypto.randomBytes(32), epoch: epoch, createdAt: nowMs())
    }
}

enum GroupAckState: String {
    case pending, partial, complete
}

/// Aggregate delivery state of one group transfer.
final class GroupTransfer {
    let id: String
    let groupId: String
    let members: [String]
    var acked: Set<String> = []
    var state: GroupAckState = .pending
    let createdAt: Int64

    init(id: String, groupId: String, members: [String]) {
        self.id = id
        self.groupId = groupId
        self.members = members
        self.createdAt = Int64(Date().timeIntervalSince1970 * 1000)
    }
}

final class GroupAckTracker {
    private let maxActive: Int

    private let lock = NSLock()
    private var active: [String: GroupTransfer] = [:]

    init(maxActive: Int = 1024) {
        self.maxActive = maxActive
    }

    /// Registers a group transfer for the given members.
    func create(_ id: String, groupId: String, members: [String]) {
        lock.lock(); defer { lock.unlock() }
        active[id] = GroupTransfer(id: id, groupId: groupId, members: members)
        while active.count > maxActive {
            guard let oldest = active.keys.first else { break }
            active.removeValue(forKey: oldest)
        }
    }

    /**
     * Records a member's acknowledgement and recomputes the aggregate state.
     * Reports whether the aggregate state changed.
     */
    func ack(_ id: String, member: String) -> Bool {
        lock.lock(); defer { lock.unlock() }
        guard let tr = active[id], !tr.acked.contains(member) else { return false }
        tr.acked.insert(member)
        let prev = tr.state
        let ackedCount = tr.members.filter { tr.acked.contains($0) }.count
        if ackedCount == 0 {
            tr.state = .pending
        } else if ackedCount >= tr.members.count {
            tr.state = .complete
        } else {
            tr.state = .partial
        }
        return tr.state != prev
    }

    /// Returns a snapshot of one group transfer's state, if known.
    func get(_ id: String) -> GroupTransfer? {
        lock.lock(); defer { lock.unlock() }
        return active[id]
    }

    /// Snapshots every tracked group transfer.
    func snapshot() -> [GroupTransfer] {
        lock.lock(); defer { lock.unlock() }
        return Array(active.values)
    }
}
