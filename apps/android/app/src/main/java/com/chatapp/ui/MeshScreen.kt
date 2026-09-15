package com.chatapp.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import com.chatapp.data.ApiClient
import com.chatapp.data.Session
import com.chatapp.mesh.NativeMeshManager
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.json.JSONObject

data class MeshPeer(val deviceId: String, val transport: String, val relayOk: Boolean)
data class MeshPending(val id: String, val dst: String, val kind: String)

@Composable
fun MeshScreen(api: ApiClient, session: Session) {
    val context = LocalContext.current
    val deviceKey = session.meshDeviceKey
    val native = remember(deviceKey) {
        NativeMeshManager(context, deviceKey, session.meshIdentityKey)
    }
    var active by remember { mutableStateOf("none") }
    var peers by remember { mutableStateOf(listOf<MeshPeer>()) }
    var pending by remember { mutableStateOf(0) }
    var relayOk by remember { mutableStateOf(true) }
    var destination by remember { mutableStateOf("") }
    var message by remember { mutableStateOf("") }
    var notice by remember { mutableStateOf<String?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val token = session.accessToken ?: ""
    val headers = remember(deviceKey) { mapOf("X-Mesh-Device" to deviceKey) }

    fun applyLocalStatus() {
        val status = native.status()
        active = status.optString("transport", "none")
        relayOk = status.optBoolean("relay_ok", true)
        pending = status.optInt("pending", 0)
        peers = native.engine.neighborList().map { MeshPeer(it.deviceId, it.transport, it.relayOk) }
    }

    fun refresh() {
        applyLocalStatus()
        scope.launch {
            try {
                withContext(Dispatchers.IO) {
                    api.post(
                        "/api/mesh/register",
                        JSONObject().put("device_key", deviceKey).put("transport", "internet").toString(),
                        token,
                        headers,
                    )
                    api.get("/api/mesh/status", token, headers)
                }
                applyLocalStatus()
            } catch (e: Exception) {
                error = e.message
            }
        }
    }

    fun send() {
        scope.launch {
            try {
                val packetId = withContext(Dispatchers.IO) {
                    native.engine.send("message", destination, message.toByteArray(Charsets.UTF_8))
                }
                notice = "Encrypted and queued packet $packetId"
                message = ""
                applyLocalStatus()
            } catch (e: Exception) {
                error = e.message
            }
        }
    }

    fun setRelay(enabled: Boolean) {
        relayOk = enabled
        native.engine.relayOk = enabled
        scope.launch {
            try {
                withContext(Dispatchers.IO) {
                    api.put(
                        "/api/mesh/relay-policy",
                        JSONObject()
                            .put("device_key", deviceKey)
                            .put("relay_enabled", enabled)
                            .put("relay_mode", if (enabled) "active" else "off")
                            .put("storage_quota", 10 * 1024 * 1024)
                            .toString(),
                        token,
                        headers,
                    )
                }
            } catch (e: Exception) {
                error = e.message
            }
        }
    }

    DisposableEffect(native) {
        native.start()
        applyLocalStatus()
        onDispose { native.stop() }
    }
    LaunchedEffect(native) { refresh() }

    Column(modifier = Modifier.fillMaxSize().padding(16.dp)) {
        Text("Offline Mesh", style = MaterialTheme.typography.headlineSmall)
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        notice?.let { Text(it, color = MaterialTheme.colorScheme.primary) }
        Text("Device: $deviceKey", style = MaterialTheme.typography.bodySmall)
        Text("Active transport: $active", style = MaterialTheme.typography.bodyMedium)
        Text("Relay consent: ${if (relayOk) "on" else "off"}", style = MaterialTheme.typography.bodySmall)

        Card(modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
            Column(modifier = Modifier.padding(12.dp)) {
                Text("Send offline", style = MaterialTheme.typography.titleMedium)
                OutlinedTextField(value = destination, onValueChange = { destination = it }, label = { Text("Destination device id") })
                OutlinedTextField(value = message, onValueChange = { message = it }, label = { Text("Message") })
                Row {
                    Button(onClick = { send() }, enabled = destination.isNotBlank() && message.isNotBlank()) { Text("Queue") }
                    TextButton(onClick = { setRelay(!relayOk) }) { Text(if (relayOk) "Disable relay" else "Enable relay") }
                    TextButton(onClick = { refresh() }) { Text("Refresh") }
                }
            }
        }

        LazyColumn(verticalArrangement = Arrangement.spacedBy(6.dp)) {
            item { Text("Peers (${peers.size})", style = MaterialTheme.typography.titleMedium) }
            items(peers) { p ->
                Card(modifier = Modifier.fillMaxWidth()) {
                    Column(modifier = Modifier.padding(10.dp)) {
                        Text(p.deviceId, style = MaterialTheme.typography.titleSmall)
                        Text("${p.transport}${if (p.relayOk) " · relay" else ""}", style = MaterialTheme.typography.bodySmall)
                    }
                }
            }
            item { Text("Store-and-forward ($pending)", style = MaterialTheme.typography.titleMedium) }
        }
    }
}
