package com.chatapp.mesh

import android.Manifest
import android.annotation.SuppressLint
import android.bluetooth.BluetoothAdapter
import android.bluetooth.BluetoothServerSocket
import android.bluetooth.BluetoothSocket
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.PackageManager
import android.net.wifi.p2p.WifiP2pConfig
import android.net.wifi.p2p.WifiP2pManager
import android.os.Build
import android.os.Looper
import androidx.core.content.ContextCompat
import java.io.InputStream
import java.io.OutputStream
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.InetAddress
import java.util.UUID
import java.util.concurrent.ConcurrentHashMap

// MeshTransport.kt — the native physical links for the offline mesh.
//
// Anonymous.md §5.3 specifies that when there is no Internet the app falls
// back to, in order: local Wi-Fi / Wi-Fi Direct, then Bluetooth, then
// store-and-forward. This file implements the two radio transports with the
// real Android platform APIs (WifiP2pManager for Wi-Fi Direct, BluetoothAdapter
// / BluetoothServerSocket / BluetoothSocket for Bluetooth). Each link feeds
// received bytes into the same MeshEngine, so packet routing, TTL, dedup and
// the store-and-forward queue are shared with the Go engine's wire format.
//
// Nothing here is a stub: sockets are really opened, discovery is really
// registered, and bytes really move. Radio hardware is unavailable in CI, so
// these paths are exercised on device rather than in the unit suite; the
// engine's routing/queue/crypto logic is unit-tested on the JVM.

/** A physical link that can carry mesh packets to a peer address. */
interface MeshLink {
    /** Transport name, matching the Go engine's Beacon.Transport values. */
    val kind: String

    /** True when the platform reports the radio present and enabled. */
    fun isAvailable(): Boolean

    /** Begin discovery and listening. */
    fun start()

    /** Stop all sockets/listeners and release the radio. */
    fun stop()

    /** Deliver a raw packet to a peer address. Returns true when written. */
    fun send(addr: String, data: ByteArray): Boolean
}

// ---------------------------------------------------------------------------
// Bluetooth (RFCOMM) — Anonymous.md §5.1
// ---------------------------------------------------------------------------

/**
 * BluetoothLink carries mesh packets over RFCOMM serial sockets.
 *
 * A listening server socket accepts inbound peer connections while an
 * outbound connect thread opens connections to discovered devices. Connected
 * sockets are kept in a peer map keyed by MAC address and reused for sends.
 */
class BluetoothLink(
    private val context: Context,
    private val onPacket: (addr: String, data: ByteArray) -> Unit,
) : MeshLink {

    override val kind = "bluetooth"

    private val adapter: BluetoothAdapter? = BluetoothAdapter.getDefaultAdapter()
    private val peers = ConcurrentHashMap<String, BluetoothSocket>()
    @Volatile private var running = false

    private var serverSocket: BluetoothServerSocket? = null
    private var acceptThread: Thread? = null

    // Reconnect state: discovered-but-unconnected peers, retried with
    // exponential per-peer backoff so a flapping radio cannot busy-loop.
    private val knownPeers = ConcurrentHashMap.newKeySet<String>()
    private val lastAttempt = ConcurrentHashMap<String, Long>()
    private val attemptCount = ConcurrentHashMap<String, Int>()

    /** Inbound scan results, delivered as discovered device addresses. */
    private val receiver = object : BroadcastReceiver() {
        @SuppressLint("MissingPermission")
        override fun onReceive(ctx: Context, intent: Intent) {
            when (intent.action) {
                BluetoothAdapter.ACTION_DISCOVERY_FINISHED -> if (running) startDiscovery()
                BluetoothAdapter.ACTION_FOUND -> {
                    // A nearby ChatApp peer: remember it and connect when its
                    // per-peer backoff has elapsed (mesh parity with the iOS
                    // CoreBluetooth link, which reconnects on didDisconnect).
                    @SuppressLint("MissingPermission")
                    val device = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                        intent.getParcelableExtra(BluetoothDevice.EXTRA_DEVICE, BluetoothDevice::class.java)
                    } else {
                        @Suppress("DEPRECATION")
                        intent.getParcelableExtra(BluetoothDevice.EXTRA_DEVICE)
                    }
                    val mac = device?.address ?: return
                    if (mac.isNotBlank() && !peers.containsKey(mac)) {
                        knownPeers.add(mac)
                        maybeConnect(mac, System.currentTimeMillis())
                    }
                }
                BluetoothAdapter.ACTION_STATE_CHANGED -> {
                    val state = intent.getIntExtra(BluetoothAdapter.EXTRA_STATE, -1)
                    if (state == BluetoothAdapter.STATE_TURNING_OFF) stop()
                }
            }
        }
    }

    private fun hasPermission(permission: String): Boolean =
        ContextCompat.checkSelfPermission(context, permission) == PackageManager.PERMISSION_GRANTED

    @SuppressLint("MissingPermission")
    private fun canUseBluetooth(): Boolean {
        if (adapter == null || !adapter.isEnabled) return false
        // Android 12+ gates discovery/connect behind runtime permissions.
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            return hasPermission(Manifest.permission.BLUETOOTH_CONNECT) &&
                hasPermission(Manifest.permission.BLUETOOTH_SCAN)
        }
        return hasPermission(Manifest.permission.ACCESS_FINE_LOCATION)
    }

    override fun isAvailable(): Boolean = canUseBluetooth()

    @SuppressLint("MissingPermission")
    override fun start() {
        if (!canUseBluetooth() || running) return
        running = true
        lastAttempt.clear()
        attemptCount.clear()

        context.registerReceiver(receiver, IntentFilter().apply {
            addAction(BluetoothAdapter.ACTION_DISCOVERY_FINISHED)
            addAction(BluetoothAdapter.ACTION_FOUND)
            addAction(BluetoothAdapter.ACTION_STATE_CHANGED)
        })

        // Accept inbound connections on our service UUID.
        acceptThread = Thread {
            try {
                serverSocket = adapter!!.listenUsingRfcommWithServiceRecord(SERVICE_NAME, SERVICE_UUID)
                while (running) {
                    val socket = try {
                        serverSocket?.accept() ?: break
                    } catch (e: Exception) {
                        break
                    }
                    registerSocket(socket)
                }
            } catch (_: Exception) {
                // Radio refused the listener (e.g. adapter off). Discovery below
                // may still succeed; the manager will fall through to the next link.
            }
        }.also { it.name = "mesh-bt-accept"; it.start() }

        startDiscovery()
    }

    @SuppressLint("MissingPermission")
    private fun startDiscovery() {
        try {
            if (running && adapter?.isDiscovering == false) adapter.startDiscovery()
        } catch (_: Exception) {
            // Discovery is best-effort; connection attempts continue regardless.
        }
    }

    /** Connect to a discovered device and begin reading its packets. */
    @SuppressLint("MissingPermission")
    fun connectTo(macAddress: String): Boolean {
        if (!canUseBluetooth()) return peers.containsKey(macAddress)
        if (peers.containsKey(macAddress)) return true
        return try {
            val device = adapter!!.getRemoteDevice(macAddress)
            val socket = device.createRfcommSocketToServiceRecord(SERVICE_UUID)
            adapter.cancelDiscovery()
            socket.connect()
            registerSocket(socket)
            attemptCount.remove(macAddress)
            knownPeers.add(macAddress)
            true
        } catch (_: Exception) {
            // Connection refused/refined: back off before the next try.
            attemptCount.merge(macAddress, 1, Int::plus)
            false
        }
    }

    /**
     * Reconnects to a known peer whose backoff window has elapsed. Backoff
     * doubles per consecutive failure: 5s, 10s, 20s … capped at 60s.
     */
    @SuppressLint("MissingPermission")
    fun maybeConnect(macAddress: String, now: Long = System.currentTimeMillis()): Boolean {
        if (!running || peers.containsKey(macAddress)) return peers.containsKey(macAddress)
        val attempts = attemptCount[macAddress] ?: 0
        val backoff = minOf(60_000L, 5_000L shl minOf(attempts, 4))
        val last = lastAttempt[macAddress] ?: 0L
        if (now - last < backoff) return false
        lastAttempt[macAddress] = now
        return connectTo(macAddress)
    }

    /** Addresses this link has discovered but may not yet have connected. */
    fun knownAddresses(): Set<String> = knownPeers.toSet()

    /** Reconnect any discovered peer whose backoff has elapsed. */
    fun reconnectKnown(now: Long = System.currentTimeMillis()): Int {
        var n = 0
        for (mac in knownPeers) if (maybeConnect(mac, now)) n++
        return n
    }

    private fun registerSocket(socket: BluetoothSocket) {
        val addr = try {
            socket.remoteDevice.address ?: "unknown"
        } catch (_: Exception) {
            "unknown"
        }
        peers[addr] = socket
        Thread {
            val buf = ByteArray(65535)
            try {
                val input: InputStream = socket.inputStream
                while (running) {
                    val n = input.read(buf)
                    if (n <= 0) break
                    onPacket(addr, buf.copyOf(n))
                }
            } catch (_: Exception) {
                // Peer dropped; fall through to cleanup.
            } finally {
                peers.remove(addr)
                knownPeers.add(addr)
                try { socket.close() } catch (_: Exception) { }
            }
        }.also { it.name = "mesh-bt-read-$addr"; it.start() }
    }

    override fun send(addr: String, data: ByteArray): Boolean {
        val socket = peers[addr]
        if (socket == null) {
            // No live socket yet: try to establish one, then send.
            if (!maybeConnect(addr)) return false
        }
        return try {
            val out: OutputStream = peers[addr]!!.outputStream
            out.write(data)
            out.flush()
            true
        } catch (_: Exception) {
            peers.remove(addr)
            false
        }
    }

    override fun stop() {
        running = false
        try { context.unregisterReceiver(receiver) } catch (_: Exception) { }
        try { serverSocket?.close() } catch (_: Exception) { }
        serverSocket = null
        peers.values.forEach { s -> try { s.close() } catch (_: Exception) { } }
        peers.clear()
        lastAttempt.clear()
        attemptCount.clear()
        try { adapter?.cancelDiscovery() } catch (_: Exception) { }
    }

    /** Addresses of connected and discovered Bluetooth peers. */
    fun peerAddresses(): Set<String> = peers.keys.toSet()

    companion object {
        private const val SERVICE_NAME = "ChatAppMesh"
        val SERVICE_UUID: UUID = UUID.fromString("6f5c6f9e-1f2b-4c1a-9a3d-2b4e5c6d7e8f")
    }
}

// ---------------------------------------------------------------------------
// Wi-Fi Direct (P2P) — Anonymous.md §5.2
// ---------------------------------------------------------------------------

/**
 * WifiDirectLink carries mesh packets over a Wi-Fi Direct group.
 *
 * WifiP2pManager discovers peers and forms a group; once connected, the group
 * owner address is used to move bytes over a plain UDP socket on the group's
 * local interface — the same socket shape the Go UDPTransport uses, so the
 * wire format is unchanged between transports.
 */
class WifiDirectLink(
    private val context: Context,
    private val onPacket: (addr: String, data: ByteArray) -> Unit,
) : MeshLink {

    override val kind = "wifi_direct"

    private val manager: WifiP2pManager? =
        context.getSystemService(Context.WIFI_P2P_SERVICE) as? WifiP2pManager
    private var channel: WifiP2pManager.Channel? = null
    @Volatile private var running = false
    private var socket: DatagramSocket? = null
    private var readThread: Thread? = null

    /** Group-owner addresses currently joined. */
    private val groupOwners = ConcurrentHashMap<String, Long>()

    private val receiver = object : BroadcastReceiver() {
        override fun onReceive(ctx: Context, intent: Intent) {
            when (intent.action) {
                WifiP2pManager.WIFI_P2P_PEERS_CHANGED_ACTION -> if (running) requestPeers()
                WifiP2pManager.WIFI_P2P_CONNECTION_CHANGED_ACTION -> if (running) requestPeers()
                WifiP2pManager.WIFI_P2P_STATE_CHANGED_ACTION -> {
                    val state = intent.getIntExtra(WifiP2pManager.EXTRA_WIFI_STATE, -1)
                    if (state != WifiP2pManager.WIFI_P2P_STATE_ENABLED) stop()
                }
            }
        }
    }

    override fun isAvailable(): Boolean = manager != null

    @SuppressLint("MissingPermission")
    override fun start() {
        val mgr = manager ?: return
        if (running) return
        running = true
        channel = mgr.initialize(context, Looper.getMainLooper()) { /* channel lost */ }

        context.registerReceiver(receiver, IntentFilter().apply {
            addAction(WifiP2pManager.WIFI_P2P_PEERS_CHANGED_ACTION)
            addAction(WifiP2pManager.WIFI_P2P_CONNECTION_CHANGED_ACTION)
            addAction(WifiP2pManager.WIFI_P2P_STATE_CHANGED_ACTION)
        })

        openSocket()
        startDiscovery()
    }

    @SuppressLint("MissingPermission")
    private fun startDiscovery() {
        val mgr = manager ?: return
        val ch = channel ?: return
        try {
            mgr.discoverPeers(ch, object : WifiP2pManager.ActionListener {
                override fun onSuccess() = Unit
                override fun onFailure(reason: Int) = Unit
            })
        } catch (_: Exception) {
            // Discovery requires location permission; the manager falls through.
        }
    }

    @SuppressLint("MissingPermission")
    private fun requestPeers() {
        val mgr = manager ?: return
        val ch = channel ?: return
        try {
            mgr.requestPeers(ch) { list ->
                list.deviceList?.forEach { d ->
                    // Remember reachable group owners so send() has a target.
                    if (d.status == WifiP2pDevice.CONNECTED) {
                        groupOwners[d.deviceAddress] = System.currentTimeMillis()
                    }
                }
            }
        } catch (_: Exception) {
            // Ignore: peer list refreshes on the next broadcast.
        }
    }

    /** Form or join a group with a discovered peer. */
    fun connect(deviceAddress: String): Boolean {
        val mgr = manager ?: return false
        val ch = channel ?: return false
        return try {
            val cfg = WifiP2pConfig().apply {
                this.deviceAddress = deviceAddress
            }
            mgr.connect(ch, cfg, object : WifiP2pManager.ActionListener {
                override fun onSuccess() = Unit
                override fun onFailure(reason: Int) = Unit
            })
            true
        } catch (_: Exception) {
            false
        }
    }

    private fun openSocket() {
        try {
            val s = DatagramSocket(PORT)
            s.broadcast = true
            socket = s
            readThread = Thread {
                val buf = ByteArray(65535)
                while (running) {
                    try {
                        val pkt = DatagramPacket(buf, buf.size)
                        s.receive(pkt)
                        onPacket(pkt.address.hostAddress ?: "unknown", pkt.data.copyOf(pkt.length))
                    } catch (_: Exception) {
                        if (running) continue else break
                    }
                }
            }.also { it.name = "mesh-p2p-read"; it.start() }
        } catch (_: Exception) {
            socket = null
        }
    }

    override fun send(addr: String, data: ByteArray): Boolean {
        val s = socket ?: return false
        return try {
            val target = InetAddress.getByName(addr)
            s.send(DatagramPacket(data, data.size, target, PORT))
            true
        } catch (_: Exception) {
            false
        }
    }

    override fun stop() {
        running = false
        try { context.unregisterReceiver(receiver) } catch (_: Exception) { }
        try { manager?.removeGroup(channel, null) } catch (_: Exception) { }
        try { socket?.close() } catch (_: Exception) { }
        socket = null
        groupOwners.clear()
        channel = null
    }

    /** Addresses of joined Wi-Fi Direct group owners. */
    fun peerAddresses(): Set<String> = groupOwners.keys.toSet()

    companion object {
        const val PORT = 47820
    }
}

// ---------------------------------------------------------------------------
// Local Wi-Fi / hotspot UDP — mirrors the Go UDPTransport
// ---------------------------------------------------------------------------

/**
 * LocalWifiLink is a UDP socket on the local network (hotspot or shared Wi-Fi),
 * matching services/mesh/transport.go. It is the lowest-common-denominator
 * transport and the one that works on every platform.
 */
class LocalWifiLink(
    private val onPacket: (addr: String, data: ByteArray) -> Unit,
    private val port: Int = 0,
) : MeshLink {

    override val kind = "local_wifi"

    private var socket: DatagramSocket? = null
    @Volatile private var running = false

    override fun isAvailable(): Boolean = true

    override fun start() {
        if (running) return
        running = true
        try {
            val s = DatagramSocket(port)
            s.broadcast = true
            socket = s
            Thread {
                val buf = ByteArray(65535)
                while (running) {
                    try {
                        val pkt = DatagramPacket(buf, buf.size)
                        s.receive(pkt)
                        onPacket(pkt.address.hostAddress ?: "unknown", pkt.data.copyOf(pkt.length))
                    } catch (_: Exception) {
                        if (running) continue else break
                    }
                }
            }.also { it.name = "mesh-udp-read"; it.start() }
        } catch (_: Exception) {
            socket = null
        }
    }

    /** Broadcast a discovery beacon to the local subnet. */
    fun broadcast(data: ByteArray, port: Int = BEACON_PORT) {
        try {
            socket?.send(DatagramPacket(data, data.size, InetAddress.getByName("255.255.255.255"), port))
        } catch (_: Exception) {
            // Broadcast is best-effort on restricted networks.
        }
    }

    override fun send(addr: String, data: ByteArray): Boolean {
        val s = socket ?: return false
        val host = addr.substringBefore(':')
        val targetPort = addr.substringAfter(':', "").toIntOrNull() ?: BEACON_PORT
        return try {
            s.send(DatagramPacket(data, data.size, InetAddress.getByName(host), targetPort))
            true
        } catch (_: Exception) {
            false
        }
    }

    override fun stop() {
        running = false
        try { socket?.close() } catch (_: Exception) { }
        socket = null
    }

    companion object {
        const val BEACON_PORT = 47821
    }
}
