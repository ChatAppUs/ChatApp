package mesh

// scale.go — mesh scaling: coverage grows with device count.
//
// The mesh is designed so that as devices increase, reachable distance and
// coverage grow with the network (up to the practical limits of the physical
// transports). The packet TTL (max hops) is the primary lever: a larger
// network needs a larger hop budget to span its diameter. This file provides
// the scalable default and a helper to size the hop budget from an expected
// device count.

import "math"

// DefaultMaxHops is the default packet TTL used when a node does not specify
// one. It is sized for a large mesh (hundreds to thousands of devices) so the
// network can span a wide area without a small fixed cap. Operators can
// override per node via NodeConfig.MaxHops.
const DefaultMaxHops = 64

// HopsForDevices returns a hop budget sized for a mesh of n devices.
//
// A mesh of n devices has a diameter that grows roughly with the square root
// of n (a 2-D mesh). We budget a generous multiple of that so packets can
// traverse the network even with imperfect topology. For very small networks
// a floor is applied so 1:1 and group traffic still works.
func HopsForDevices(n int) int {
	if n <= 0 {
		return DefaultMaxHops
	}
	// Diameter of a 2-D mesh ~ sqrt(n); budget 4x for slack + a floor.
	hops := int(math.Ceil(4 * math.Sqrt(float64(n))))
	if hops < 8 {
		hops = 8
	}
	if hops > 1024 {
		hops = 1024
	}
	return hops
}
