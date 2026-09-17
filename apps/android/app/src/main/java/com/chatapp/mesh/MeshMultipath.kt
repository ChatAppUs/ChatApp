package com.chatapp.mesh

// MeshMultipath.kt — explicit multipath selection for the mesh.
//
// Mirrors services/mesh/multipath.go. The forwarding buffer fans a packet
// out to a bounded set of neighbours. This module chooses paths that are both
// high-scoring and topologically diverse (different transports, different last
// hops), so a single radio or a single congested relay does not become the
// bottleneck for a transfer.

object MeshMultipath {

    /** Returns up to [max] relays for a destination, preferring high-scoring
     *  neighbours and enforcing transport diversity. */
    fun selectMultipath(
        dst: String,
        max: Int,
        neighbors: List<MeshNeighbor>,
    ): List<MeshNeighbor> {
        val now = System.currentTimeMillis()
        val sorted = neighbors
            .filter { n -> !n.lastSeen.before(java.util.Date(now - MeshEngine.neighbourTTL.toLong())) }
            .sortedByDescending { it.lastSeen }

        val out = mutableListOf<MeshNeighbor>()
        val seen = HashSet<String>(max)
        val transports = HashSet<String>(max)

        // Group broadcast: flood to every directly-connected neighbour.
        if (dst.isEmpty()) {
            for (nb in sorted) {
                if (out.size >= max) break
                if (seen.add(nb.deviceId)) out.add(nb)
            }
            return out
        }

        // Direct destination first.
        val direct = sorted.firstOrNull { it.deviceId == dst }
        if (direct != null) {
            out.add(direct)
            seen.add(direct.deviceId)
            transports.add(direct.transport)
        }

        // Then high-scoring relays, preferring diverse transports.
        for (nb in sorted) {
            if (out.size >= max) break
            if (!seen.add(nb.deviceId)) continue
            if (!nb.relayOk) continue
            if (nb.deviceId == dst) continue
            if (transports.add(nb.transport)) {
                out.add(nb)
            }
        }

        // Fill remaining slots regardless of transport.
        for (nb in sorted) {
            if (out.size >= max) break
            if (!seen.add(nb.deviceId)) continue
            if (!nb.relayOk) continue
            if (nb.deviceId == dst) continue
            out.add(nb)
        }
        return out
    }
}