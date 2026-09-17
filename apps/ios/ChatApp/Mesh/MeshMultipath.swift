import Foundation

/// MeshMultipath.swift — explicit multipath selection for the mesh.
///
/// Mirrors services/mesh/multipath.go. Picks a set of relays that are both
/// well-scoring and provide independent paths, avoiding single-relay bottlenecks.

struct MeshNeighbor {
    let deviceId: String
    var relayOk: Bool
    var lastSeen: Date
    var score: Double
}

final class MultipathSelector {
    private let maxPaths: Int
    private let minScore: Double

    init(maxPaths: Int = 3, minScore: Double = 1.0) {
        self.maxPaths = maxPaths
        self.minScore = minScore
    }

    func select(from candidates: [MeshNeighbor], destination: String) -> [MeshNeighbor] {
        let filtered = candidates
            .filter { $0.relayOk && $0.score >= minScore }
            .sorted { $0.score > $1.score }
        guard !filtered.isEmpty else { return [] }
        var picked: [MeshNeighbor] = []
        var seen = Set<String>()
        for n in filtered {
            if seen.contains(n.deviceId) { continue }
            seen.insert(n.deviceId)
            picked.append(n)
            if picked.count >= maxPaths { break }
        }
        return picked
    }

    func pathScore(neighbors: [MeshNeighbor]) -> Double {
        guard !neighbors.isEmpty else { return 0 }
        let total = neighbors.map(\.score).reduce(0, +)
        return total / Double(neighbors.count)
    }
}