package com.chatapp.mesh

import kotlin.math.*
// MeshScale.kt — scaling helpers for the offline mesh.
//
// Mirrors services/mesh/scale.go. The packet TTL (max hops) is the primary
// lever. A larger network needs a larger hop budget to span its logical
// diameter.

object MeshScale {
    const val DEFAULT_MAX_HOPS = 64

    /** Returns a hop budget sized for a mesh of [n] devices, driven by sqrt. */
    fun hopsForDevices(n: Int): Int {
        if (n <= 0) return DEFAULT_MAX_HOPS
        var hops = ceil(6.0 * sqrt(n.toDouble())).toInt()
        if (hops < 8) hops = 8
        if (hops > 1024) hops = 1024
        return hops
    }
}