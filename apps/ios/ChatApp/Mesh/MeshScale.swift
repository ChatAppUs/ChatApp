import Foundation

/// MeshScale.swift — mesh scaling: coverage grows with device count.
///
/// Mirrors services/mesh/scale.go. The packet TTL (max hops) is the primary
/// lever. A larger network needs a larger hop budget to span its diameter.

enum MeshScale {
    static let defaultMaxHops = 64

    static func hopsForDevices(_ n: Int) -> Int {
        guard n > 0 else { return defaultMaxHops }
        var hops = Int(ceil(6.0 * sqrt(Double(n))))
        if hops < 8 { hops = 8 }
        if hops > 1024 { hops = 1024 }
        return hops
    }
}