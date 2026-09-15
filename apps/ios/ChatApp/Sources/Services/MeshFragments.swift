import Foundation
import CryptoKit

// MeshFragments.swift — MTU-bounded fragmentation and reassembly.
//
// Mirrors services/mesh/fragment.go and the Android MeshFragment.kt. A payload
// larger than one radio datagram is split into fixed-size fragments; every
// fragment is encrypted with its own AEAD nonce and carried as an ordinary
// packet, so routing, TTL, dedup and store-and-forward apply unchanged. The
// receiver reassembles under bounded memory and time, ignores duplicate
// fragments, expires incomplete groups, and verifies a SHA-256 digest over
// the complete payload before delivery.

/// Fragment payload ceiling in bytes, matching DefaultMaxPayload.
let meshDefaultMaxPayload = 512

private let meshMaxFragments = 4096
private let meshMaxAssemblies = 256
private let meshMaxAssemblyBytes = 8 * 1024 * 1024
private let meshAssemblyTtlMs: Int64 = 5 * 60 * 1000

struct SplitParts {
    let chunks: [Data]
    /// nil when the payload fits one datagram (single-packet wire shape kept).
    let fragId: String?
    let digest: Data
}

private final class Assembly {
    let total: Int
    let digest: Data
    var parts: [Int: Data] = [:]
    var bytes = 0
    var updated: Int64
    init(total: Int, digest: Data) {
        self.total = total
        self.digest = digest
        self.updated = Int64(Date().timeIntervalSince1970 * 1000)
    }
}

/// Splits a plaintext into MTU-bounded chunks; nil fragId when it fits whole.
func meshSplitPayload(_ plaintext: Data, maxPayload: Int = meshDefaultMaxPayload) -> SplitParts {
    let mp = maxPayload > 0 ? maxPayload : meshDefaultMaxPayload
    let digest = Data(SHA256.hash(data: plaintext))
    if plaintext.count <= mp {
        return SplitParts(chunks: [plaintext], fragId: nil, digest: digest)
    }
    var chunks: [Data] = []
    var off = 0
    while off < plaintext.count {
        let end = min(off + mp, plaintext.count)
        chunks.append(plaintext.subdata(in: off..<end))
        off = end
    }
    precondition(chunks.count <= meshMaxFragments, "mesh: too many fragments")
    return SplitParts(chunks: chunks, fragId: MeshCrypto.randomId(), digest: digest)
}

/**
 * Collects decrypted fragments per (source, fragment-group) and yields the
 * complete payload once every part has arrived and the digest verifies.
 */
final class FragmentAssembler {
    private let maxOpen: Int
    private let maxBytes: Int
    private let ttlMs: Int64

    private let lock = NSLock()
    private var open: [String: Assembly] = [:]
    private var bytes = 0

    init(maxOpen: Int = meshMaxAssemblies, maxBytes: Int = meshMaxAssemblyBytes, ttlMs: Int64 = meshAssemblyTtlMs) {
        self.maxOpen = maxOpen
        self.maxBytes = maxBytes
        self.ttlMs = ttlMs
    }

    private func nowMs() -> Int64 { Int64(Date().timeIntervalSince1970 * 1000) }

    private func dropLocked(_ key: String, _ a: Assembly) {
        open.removeValue(forKey: key)
        bytes -= a.bytes
        if bytes < 0 { bytes = 0 }
    }

    private func expireLocked(_ now: Int64) {
        for (k, a) in open where now - a.updated > ttlMs {
            bytes -= a.bytes
            if bytes < 0 { bytes = 0 }
            open.removeValue(forKey: k)
        }
    }

    /**
     * Accepts one decrypted fragment. Returns the complete payload when the
     * group finished (digest verified), nil while parts are outstanding, the
     * fragment was a duplicate, or the metadata is invalid.
     */
    func add(_ p: MeshPacket, _ chunk: Data) -> Data? {
        if p.fragTotal <= 1 { return chunk }
        guard let fragId = p.fragId, !fragId.isEmpty,
              p.fragIndex >= 0, p.fragIndex < p.fragTotal, p.fragTotal <= meshMaxFragments
        else { return nil }
        let key = p.src + "\u{0000}" + fragId
        let now = nowMs()
        lock.lock(); defer { lock.unlock() }
        expireLocked(now)
        var a = open[key]
        if a == nil {
            guard open.count < maxOpen, let sum = p.fragSum else { return nil }
            a = Assembly(total: p.fragTotal, digest: sum)
            open[key] = a
        }
        guard let asm = a else { return nil }
        if asm.total != p.fragTotal || asm.digest != p.fragSum {
            // Contradictory metadata for one group id: drop and fail closed.
            dropLocked(key, asm)
            return nil
        }
        if asm.parts[p.fragIndex] != nil { return nil } // duplicate: ignored
        if bytes + chunk.count > maxBytes {
            dropLocked(key, asm)
            return nil
        }
        asm.parts[p.fragIndex] = chunk
        asm.bytes += chunk.count
        bytes += chunk.count
        asm.updated = now
        if asm.parts.count < asm.total { return nil }
        // Complete: stitch in index order, verify the digest, deliver once.
        var full = Data(capacity: asm.bytes)
        for i in 0..<asm.total {
            guard let piece = asm.parts[i] else { return nil }
            full.append(piece)
        }
        dropLocked(key, asm)
        guard Data(SHA256.hash(data: full)) == asm.digest else { return nil }
        return full
    }

    /// How many reassembly groups are in progress.
    func openCount() -> Int {
        lock.lock(); defer { lock.unlock() }
        expireLocked(nowMs())
        return open.count
    }
}
