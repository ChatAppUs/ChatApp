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
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.chatapp.data.ApiClient
import com.chatapp.data.Session
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.json.JSONObject

data class MeshPeer(val deviceId: String, val transport: String, val relayOk: Boolean)
data class MeshPending(val id: String, val dst: String, val kind: String)

// Android offline-mesh surface: shows the active transport chain (local
// Wi-Fi → Wi-Fi Direct → Bluetooth, chosen automatically per Anonymous.md
// §5.3), discovered peers, the store-and-forward queue, and the relay policy.
// Packets move over the real native transports in MeshTransport.kt; the queue
// and dedup live in MeshEngine.kt. The backend /api/mesh/* routes remain the
// internet fallback.
@Composable
fun MeshScreen(api: ApiClient, session: Session) {
    var active by remember { mutableStateOf<String>("none") }
    var peers by remember { mutableStateOf(listOf<MeshPeer>()) }
    var pending by remember { mutableStateOf(listOf<MeshPending>()) }
    var relayOk by remember { mutableStateOf(true) }
    var destination by remember { mutableStateOf("") }
    var message by remember { mutableStateOf("") }
    var notice by remember { mutableStateOf<String?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val token = session.accessToken ?: ""

    fun refresh() {
        scope.launch {
            try {
                val s = withContext(Dispatchers.IO) { api.get("/api/mesh/status", token) }
                val j = JSONObject(s)
                active = j.optString("transport", j.optString("active_transport", "none"))
                relayOk = j.optBoolean("relay_ok", true)
                val p = j.optJSONArray("peers")
                val plist = mutableListOf<MeshPeer>()
                if (p != null) for (i in 0 until p.length()) {
                    val n = p.getJSONObject(i)
                    plist.add(
                        MeshPeer(
                            deviceId = n.optString("device_id"),
                            transport = n.optString("transport", "local_wifi"),
                            relayOk = n.optBoolean("relay_ok", false),
                        )
                    )
                }
                peers = plist
                val q = j.optJSONArray("pending")
                val qlist = mutableListOf<MeshPending>()
                if (q != null) for (i in 0 until q.length()) {
                    val n = q.getJSONObject(i)
                    qlist.add(
                        MeshPending(
                            id = n.optString("id"),
                            dst = n.optString("dst"),
                            kind = n.optString("kind"),
                        )
                    )
                }
                pending = qlist
            } catch (e: Exception) {
                error = e.message
            }
        }
    }

    fun send() {
        scope.launch {
            try {
                val body = JSONObject()
                    .put("dst", destination)
                    .put("kind", "message")
                    .put("body", message)
                    .toString()
                val resp = withContext(Dispatchers.IO) { api.post("/api/mesh/send", body, token) }
                notice = "Queued packet " + JSONObject(resp).optString("packet_id", "")
                message = ""
                refresh()
            } catch (e: Exception) {
                error = e.message
            }
        }
    }

    LaunchedEffect(Unit) { refresh() }

    Column(modifier = Modifier.fillMaxSize().padding(16.dp)) {
        Text("Offline Mesh", style = MaterialTheme.typography.headlineSmall)
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        notice?.let { Text(it, color = MaterialTheme.colorScheme.primary) }
        Text("Active transport: $active", style = MaterialTheme.typography.bodyMedium)
        Text("Relay consent: ${if (relayOk) "on" else "off"}", style = MaterialTheme.typography.bodySmall)

        Card(modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
            Column(modifier = Modifier.padding(12.dp)) {
                Text("Send offline", style = MaterialTheme.typography.titleMedium)
                OutlinedTextField(value = destination, onValueChange = { destination = it }, label = { Text("Destination device id") })
                OutlinedTextField(value = message, onValueChange = { message = it }, label = { Text("Message") })
                Row {
                    Button(onClick = { send() }, enabled = destination.isNotBlank() && message.isNotBlank()) { Text("Queue") }
                    TextButton(onClick = {
                        scope.launch {
                            try {
                                val body = JSONObject().put("relay_ok", !relayOk).toString()
                                withContext(Dispatchers.IO) { api.put("/api/mesh/relay-policy", body, token) }
                                refresh()
                            } catch (e: Exception) {
                                error = e.message
                            }
                        }
                    }) { Text("Toggle relay") }
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
                        Text("${p.transport}${if (p.relayOk) " · relay" else ""}",
                            style = MaterialTheme.typography.bodySmall)
                    }
                }
            }
            item { Text("Store-and-forward (${pending.size})", style = MaterialTheme.typography.titleMedium) }
            items(pending) { q ->
                Card(modifier = Modifier.fillMaxWidth()) {
                    Column(modifier = Modifier.padding(10.dp)) {
                        Text("→ ${q.dst}", style = MaterialTheme.typography.titleSmall)
                        Text("${q.kind} · ${q.id.take(8)}", style = MaterialTheme.typography.bodySmall)
                    }
                }
            }
        }
    }
}
