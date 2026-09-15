import Foundation

// JVM/Linux-test stub for the iOS MeshLink protocol (normally in MeshTransport.swift).
public protocol MeshLink: AnyObject {
    var kind: String { get }
    func isAvailable() -> Bool
    var peer: ((String, Data) -> Void)? { get set }
    func start()
    func stop()
    func send(addr: String, data: Data) -> Bool
}
