import Foundation
import UIKit
import CoreBluetooth
import Network

// MeshTransport.swift — the native iOS radio transports for the offline mesh.
//
// Anonymous.md §5.3 requires an automatic fallback chain when there is no
// Internet: local Wi-Fi / Wi-Fi Direct first, then Bluetooth, then
// store-and-forward. This file provides:
//
//   * `BluetoothMeshLink` — a real CoreBluetooth peripheral + central built on
//     CBPeripheralManager / CBCentralManager with a dedicated service and
//     characteristic, so nearby iOS devices can exchange mesh packets with no
//     infrastructure at all.
//   * `LocalWifiMeshLink` — a Network.framework UDP listener/connection pair on
//     the local network, mirroring services/mesh/transport.go.
//
// Both feed received bytes into `MeshEngine`, which owns routing, TTL, dedup
// and the store-and-forward queue — the same logic the Android client runs, so
// behaviour is identical across platforms.

/// A physical link that carries mesh packets.
protocol MeshLink: AnyObject {
    var kind: String { get }
    func isAvailable() -> Bool
    func start()
    func stop()
    func send(addr: String, data: Data) -> Bool
}

// MARK: - Bluetooth (CoreBluetooth)

/// BluetoothMeshLink exchanges mesh packets over a CoreBluetooth GATT service.
///
/// The device simultaneously advertises the mesh service (so peers can write to
/// it) and scans for peers advertising the same service. A characteristic write
/// is the packet boundary; a notify carries packets outward to subscribed peers.
final class BluetoothMeshLink: NSObject, MeshLink {

    let kind = "bluetooth"

    /// Mesh service / characteristic UUIDs (must match on every client).
    static let serviceUUID = CBUUID(string: "6F5C6F9E-1F2B-4C1A-9A3D-2B4E5C6D7E8F")
    static let charUUID = CBUUID(string: "6F5C6F9E-1F2B-4C1A-9A3D-2B4E5C6D7E90")

    private let onPacket: (String, Data) -> Void
    private var peripheral: CBPeripheralManager?
    private var central: CBCentralManager?
    private let queue = DispatchQueue(label: "mesh.bluetooth")
    private var subscribed = Set<UUID>()
    private var discovered: [UUID: CBPeripheral] = [:]
    @Volatile private var running = false

    init(onPacket: @escaping (String, Data) -> Void) {
        self.onPacket = onPacket
    }

    func isAvailable() -> Bool {
        // CoreBluetooth on iOS reports power state asynchronously; treat the
        // link as available until the manager says otherwise.
        return true
    }

    func start() {
        guard !running else { return }
        running = true
        peripheral = CBPeripheralManager(delegate: self, queue: queue)
        central = CBCentralManager(delegate: self, queue: queue)
    }

    func stop() {
        running = false
        peripheral?.stopAdvertising()
        peripheral?.removeAllServices()
        central?.stopScan()
        subscribed.removeAll()
        discovered.removeAll()
        peripheral = nil
        central = nil
    }

    func send(addr: String, data: Data) -> Bool {
        // Broadcast via notify to every subscribed central; Bluetooth has no
        // unicast addressing exposed to apps, so the packet's dst field routes.
        guard let peripheral, peripheral.isAdvertising || !subscribed.isEmpty else { return false }
        let payload = data.base64EncodedString()
        guard let bytes = payload.data(using: .utf8) else { return false }
        peripheral.updateValue(bytes, for: BluetoothMeshLink.characteristic, onSubscribedCentrals: nil)
        return true
    }

    private static let characteristic = CBMutableCharacteristic(
        type: BluetoothMeshLink.charUUID,
        properties: [.write, .notify],
        value: nil,
        permissions: [.writeable]
    )

    /// Local device identifier surfaced as the peer address.
    static var localAddress: String { UIDevice.current.identifierForVendor?.uuidString ?? "ios-device" }
}

extension BluetoothMeshLink: CBPeripheralManagerDelegate {

    func peripheralManagerDidUpdateState(_ peripheral: CBPeripheralManager) {
        guard running, peripheral.state == .poweredOn else { return }
        let service = CBMutableService(type: BluetoothMeshLink.serviceUUID, primary: true)
        service.characteristics = [BluetoothMeshLink.characteristic]
        peripheral.add(service)
        peripheral.startAdvertising([
            CBAdvertisementDataServiceUUIDsKey: [BluetoothMeshLink.serviceUUID],
            CBAdvertisementDataLocalNameKey: "ChatAppMesh",
        ])
    }

    func peripheralManager(_ peripheral: CBPeripheralManager, central: CBCentral, didSubscribeTo characteristic: CBCharacteristic) {
        subscribed.insert(central.identifier)
    }

    func peripheralManager(_ peripheral: CBPeripheralManager, central: CBCentral, didUnsubscribeFrom characteristic: CBCharacteristic) {
        subscribed.remove(central.identifier)
    }

    func peripheralManager(_ peripheral: CBPeripheralManager, didReceiveWrite requests: [CBATTRequest]) {
        for req in requests {
            if let value = req.value, let decoded = Data(base64Encoded: String(decoding: value, as: UTF8.self)) {
                onPacket(req.central.identifier.uuidString, decoded)
            }
            peripheral.respond(to: req, withResult: .success)
        }
    }
}

extension BluetoothMeshLink: CBCentralManagerDelegate {

    func centralManagerDidUpdateState(_ central: CBCentralManager) {
        guard running, central.state == .poweredOn else { return }
        central.scanForPeripherals(withServices: [BluetoothMeshLink.serviceUUID], options: nil)
    }

    func centralManager(_ central: CBCentralManager, didDiscover peripheral: CBPeripheral,
                        advertisementData: [String: Any], rssi RSSI: NSNumber) {
        discovered[peripheral.identifier] = peripheral
        peripheral.delegate = self
        central.connect(peripheral, options: nil)
    }

    func centralManager(_ central: CBCentralManager, didConnect peripheral: CBPeripheral) {
        peripheral.discoverServices([BluetoothMeshLink.serviceUUID])
    }

    func centralManager(_ central: CBCentralManager, didDisconnectPeripheral peripheral: CBPeripheral, error: Error?) {
        discovered.removeValue(forKey: peripheral.identifier)
        if running { central.scanForPeripherals(withServices: [BluetoothMeshLink.serviceUUID], options: nil) }
    }
}

extension BluetoothMeshLink: CBPeripheralDelegate {

    func peripheral(_ peripheral: CBPeripheral, didDiscoverServices error: Error?) {
        peripheral.services?.forEach { svc in
            peripheral.discoverCharacteristics([BluetoothMeshLink.charUUID], for: svc)
        }
    }

    func peripheral(_ peripheral: CBPeripheral, didDiscoverCharacteristicsFor service: CBService, error: Error?) {
        service.characteristics?.forEach { ch in
            if ch.properties.contains(.notify) { peripheral.setNotifyValue(true, for: ch) }
        }
    }

    func peripheral(_ peripheral: CBPeripheral, didUpdateValueFor characteristic: CBCharacteristic, error: Error?) {
        guard let value = characteristic.value,
              let decoded = Data(base64Encoded: String(decoding: value, as: UTF8.self)) else { return }
        onPacket(peripheral.identifier.uuidString, decoded)
    }
}

// MARK: - Local Wi-Fi (Network.framework UDP)

/// LocalWifiMeshLink carries mesh packets over UDP on the local network —
/// hotspot or shared Wi-Fi — matching the Go UDPTransport's wire behaviour.
final class LocalWifiMeshLink: MeshLink {

    let kind = "local_wifi"

    private let onPacket: (String, Data) -> Void
    private let port: NWEndpoint.Port
    private var listener: NWListener?
    private var connection: NWConnection?
    private let queue = DispatchQueue(label: "mesh.localwifi")
    @Volatile private var running = false

    init(onPacket: @escaping (String, Data) -> Void, port: UInt16 = 47821) {
        self.onPacket = onPacket
        self.port = NWEndpoint.Port(rawValue: port) ?? 47821
    }

    func isAvailable() -> Bool { true }

    func start() {
        guard !running else { return }
        running = true
        do {
            let params = NWParameters.udp
            params.allowLocalEndpointReuse = true
            let l = try NWListener(using: params, on: port)
            l.newConnectionHandler = { [weak self] conn in
                self?.receive(on: conn)
            }
            l.start(queue: queue)
            listener = l

            let c = NWConnection(host: "255.255.255.255", port: port, using: params)
            c.start(queue: queue)
            connection = c
        } catch {
            listener = nil
            connection = nil
        }
    }

    private func receive(on conn: NWConnection) {
        conn.receiveMessage { [weak self] data, _, _, _ in
            guard let self, self.running else { return }
            if let data { self.onPacket(conn.endpoint.debugDescription, data) }
            self.receive(on: conn)
        }
    }

    func send(addr: String, data: Data) -> Bool {
        guard let connection else { return false }
        connection.send(content: data, completion: .contentProcessed { _ in })
        return true
    }

    func stop() {
        running = false
        listener?.cancel()
        connection?.cancel()
        listener = nil
        connection = nil
    }
}

// MARK: - Manager

/// Chooses and runs the transport chain on iOS, in Anonymous.md §5.3 order.
final class NativeMeshManager {

    let engine: MeshEngine

    private let bluetooth: BluetoothMeshLink
    private let localWifi: LocalWifiMeshLink

    init(deviceId: String, key: Data) {
        // Bind the links to a local reference so the closures never capture
        // `self` before every stored property is initialised.
        let mesh = MeshEngine(deviceId: deviceId, key: key)
        self.engine = mesh
        bluetooth = BluetoothMeshLink { addr, data in _ = mesh.handleInbound(addr: addr, data: data) }
        localWifi = LocalWifiMeshLink { addr, data in _ = mesh.handleInbound(addr: addr, data: data) }
    }

    func start() {
        engine.attach([localWifi, bluetooth])
    }

    func stop() {
        engine.stop()
    }

    /// Current state for the mesh status screen.
    func status() -> [String: Any] {
        [
            "transport": engine.activeTransport,
            "relay_ok": engine.relayOk,
            "peers": engine.neighborList().count,
            "pending": engine.queueSize(),
        ]
    }
}
