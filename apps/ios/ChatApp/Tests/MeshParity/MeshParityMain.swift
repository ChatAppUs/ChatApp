import Foundation

final class TestLink: MeshLink {
    let kind = "loopback"
    func isAvailable() -> Bool { true }
    func start() {}
    func stop() {}
    var peer: ((String, Data) -> Void)?
    func send(addr: String, data: Data) -> Bool { peer?(addr, data); return true }
}

final class Node {
    let engine: MeshEngine
    let link = TestLink()
    var received: [(MeshPacket, Data)] = []
    init(_ id: String, _ key: Data) {
        engine = MeshEngine(deviceId: id, key: key)
        engine.delivered = { [weak self] p, pt in self?.received.append((p, pt ?? Data())) }
    }
    func wire() {
        link.peer = { [weak self] addr, data in
            guard let self else { return }
            _ = self.engine.handleInbound(addr: addr, data: data)
            while let p = self.engine.pending().first {
                if self.link.send(addr: "x", data: MeshPacketCodec.encode(p)) { self.engine.dequeue(p.id) } else { break }
            }
        }
    }
}

func unattachedPair() -> (Node, Node) {
    let a = Node("dev-a", Data(repeating: 1, count: 32))
    let b = Node("dev-b", Data(repeating: 1, count: 32))
    a.link.peer = { [weak b] addr, data in _ = b?.engine.handleInbound(addr: addr, data: data) }
    b.link.peer = { [weak a] addr, data in _ = a?.engine.handleInbound(addr: addr, data: data) }
    a.engine.upsertNeighbor(deviceId: "dev-b", addr: "addr-b", transport: "loopback")
    b.engine.upsertNeighbor(deviceId: "dev-a", addr: "addr-a", transport: "loopback")
    return (a, b)
}

func wirePair() -> (Node, Node) {
    let a = Node("dev-a", Data(repeating: 1, count: 32))
    let b = Node("dev-b", Data(repeating: 1, count: 32))
    a.link.peer = { [weak b] addr, data in _ = b?.engine.handleInbound(addr: addr, data: data) }
    b.link.peer = { [weak a] addr, data in _ = a?.engine.handleInbound(addr: addr, data: data) }
    a.engine.attach([a.link])
    b.engine.attach([b.link])
    a.engine.upsertNeighbor(deviceId: "dev-b", addr: "addr-b", transport: "loopback")
    b.engine.upsertNeighbor(deviceId: "dev-a", addr: "addr-a", transport: "loopback")
    return (a, b)
}

var failed = 0

func check(_ cond: Bool, _ name: String) {
    if cond { print("PASS swift: \(name)") } else { failed += 1; print("FAIL swift: \(name)") }
}

func run() {
    // reliable unicast round trip with ack settle
    do {
        let (a, b) = wirePair()
        let id = a.engine.sendReliable(kind: MeshEngine.kindMessage, dst: "dev-b", plaintext: Data("reliable hi".utf8))
        check(b.received.count == 1 && String(data: b.received[0].1, encoding: .utf8) == "reliable hi", "reliable unicast delivered")
        check(a.engine.transfers.get(id)?.state == .acked, "ack settled transfer")
    }

    // fragmentation round trip
    do {
        let (a, b) = wirePair()
        let payload = Data((0..<1500).map { UInt8(97 + $0 % 26) })
        _ = a.engine.sendLarge(kind: MeshEngine.kindMessage, target: "dev-b", plaintext: payload, group: false)
        check(b.received.count == 1 && b.received[0].1 == payload, "fragmentation round trip")
    }

    // group keys + group ack
    do {
        let (a, b) = wirePair()
        let gk = a.engine.rotateGroupKey(groupId: "g1")
        b.engine.adoptGroupKey(groupId: "g1", key: gk.key, epoch: gk.epoch)
        a.engine.trackGroupMembers(groupId: "g1", members: ["dev-a", "dev-b"])
        b.engine.trackGroupMembers(groupId: "g1", members: ["dev-a", "dev-b"])
        _ = a.engine.sendGroup(kind: MeshEngine.kindGroupMessage, groupId: "g1", plaintext: Data("group hi".utf8))
        check(b.received.count == 1 && String(data: b.received[0].1, encoding: .utf8) == "group hi", "group message delivered")
        check(a.engine.groupAcks.snapshot().first?.acked.contains("dev-b") == true, "group ack complete")
    }

    // local revocation applied
    do {
        let (a, b) = wirePair()
        _ = b.engine.handleInbound(addr: "addr-a", data: a.engine.beacon(seq: 1))
        let pub = a.engine.identity.signPublic()
        let r = MeshRevocationNotice.sign(revoker: "dev-a", deviceId: "evil", publicKey: pub, signer: a.engine.identity)
        _ = b.engine.handleInbound(addr: "addr-a", data: r!.marshal())
        check(b.engine.isPeerRevoked(deviceId: "evil"), "local revocation applied")
    }

    // priority ordering
    do {
        let (a, _) = unattachedPair()
        _ = a.engine.send(kind: MeshEngine.kindVoiceMessage, dst: "dev-b", plaintext: Data("v".utf8))
        _ = a.engine.send(kind: MeshEngine.kindMessage, dst: "dev-b", plaintext: Data("m".utf8))
        _ = a.engine.send(kind: MeshEngine.kindCallSignal, dst: "dev-b", plaintext: Data("c".utf8))
        let order = a.engine.pending().map { $0.kind }
        check(order == [MeshEngine.kindCallSignal, MeshEngine.kindMessage, MeshEngine.kindVoiceMessage], "priority order (got \(order))")
    }

    print(failed == 0 ? "all swift parity tests passed" : "\(failed) swift test(s) failed")
    if failed > 0 { exit(1) }
}




run()
