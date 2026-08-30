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
import androidx.compose.material3.Checkbox
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
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.chatapp.data.ApiClient
import com.chatapp.data.Session
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.json.JSONObject

data class GroupTag(
    val id: String,
    val name: String,
    val description: String?,
    val memberCount: Int,
    val privacy: String,
    var joined: Boolean,
)

data class GroupEvent(
    val id: String,
    val title: String,
    val going: Int,
    val interested: Int,
)

// Groups: create/join/leave plus per-group events with RSVP, matching the
// web /groups and /groups/[id] pages.
@Composable
fun GroupsScreen(api: ApiClient, session: Session) {
    var groups by remember { mutableStateOf(listOf<GroupTag>()) }
    var eventsByGroup by remember { mutableStateOf(mapOf<String, List<GroupEvent>>()) }
    var expanded by remember { mutableStateOf(setOf<String>()) }
    var name by remember { mutableStateOf("") }
    var topic by remember { mutableStateOf("") }
    var private by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val token = session.accessToken ?: ""

    fun loadGroups() {
        scope.launch {
            try {
                val resp = withContext(Dispatchers.IO) { api.get("/api/groups?limit=50", token) }
                val arr = JSONObject(resp).getJSONArray("groups")
                val parsed = mutableListOf<GroupTag>()
                for (i in 0 until arr.length()) {
                    val g = arr.getJSONObject(i)
                    parsed.add(
                        GroupTag(
                            id = g.getString("id"),
                            name = g.getString("name"),
                            description = if (g.isNull("description")) null else g.getString("description"),
                            memberCount = g.optInt("member_count"),
                            privacy = g.optString("privacy", "public"),
                            joined = groups.firstOrNull { it.id == g.getString("id") }?.joined ?: false,
                        )
                    )
                }
                groups = parsed
            } catch (e: Exception) {
                error = e.message
            }
        }
    }

    fun loadEvents(groupId: String) {
        scope.launch {
            try {
                val resp = withContext(Dispatchers.IO) { api.get("/api/events?limit=50", token) }
                val arr = JSONObject(resp).getJSONArray("events")
                val parsed = mutableListOf<GroupEvent>()
                for (i in 0 until arr.length()) {
                    val e = arr.getJSONObject(i)
                    if (e.optString("group_id") != groupId) continue
                    parsed.add(
                        GroupEvent(
                            id = e.getString("id"),
                            title = e.getString("title"),
                            going = e.optInt("going_count"),
                            interested = 0,
                        )
                    )
                }
                eventsByGroup = eventsByGroup + (groupId to parsed)
            } catch (e: Exception) {
                error = e.message
            }
        }
    }

    fun rsvp(eventId: String, status: String, groupId: String) {
        scope.launch {
            try {
                withContext(Dispatchers.IO) {
                    api.post("/api/events/$eventId/rsvp", JSONObject().put("response", status).toString(), token)
                }
                loadEvents(groupId)
            } catch (e: Exception) {
                error = e.message
            }
        }
    }

    LaunchedEffect(Unit) { loadGroups() }

    Column(modifier = Modifier.fillMaxSize().padding(16.dp)) {
        Text("Groups", style = MaterialTheme.typography.headlineSmall)
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }

        Card(modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
            Column(modifier = Modifier.padding(12.dp)) {
                Text("New group", style = MaterialTheme.typography.titleMedium)
                OutlinedTextField(value = name, onValueChange = { name = it }, label = { Text("Name") })
                OutlinedTextField(value = topic, onValueChange = { topic = it }, label = { Text("Description") })
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Checkbox(checked = private, onCheckedChange = { private = it })
                    Text("Private")
                }
                Button(onClick = {
                    scope.launch {
                        try {
                            val body = JSONObject().put("name", name).put("description", topic)
                                .put("privacy", if (private) "private" else "public").toString()
                            withContext(Dispatchers.IO) { api.post("/api/groups", body, token) }
                            name = ""; topic = ""; private = false
                            loadGroups()
                        } catch (e: Exception) {
                            error = e.message
                        }
                    }
                }, enabled = name.isNotBlank()) { Text("Create") }
            }
        }

        LazyColumn(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            items(groups) { g ->
                Card(modifier = Modifier.fillMaxWidth()) {
                    Column(modifier = Modifier.padding(12.dp)) {
                        Text(g.name, style = MaterialTheme.typography.titleMedium)
                        g.description?.let { Text(it, style = MaterialTheme.typography.bodySmall) }
                        Row {
                            Button(onClick = {
                                scope.launch {
                                    try {
                                        if (g.joined) {
                                            withContext(Dispatchers.IO) {
                                                api.delete("/api/groups/${g.id}/join", "{}", token)
                                            }
                                            groups = groups.map { if (it.id == g.id) it.copy(joined = false) else it }
                                        } else {
                                            val resp = withContext(Dispatchers.IO) {
                                                api.post("/api/groups/${g.id}/join", "{}", token)
                                            }
                                            val st = JSONObject(resp).optString("status")
                                            groups = groups.map {
                                                if (it.id == g.id) it.copy(joined = st == "active") else it
                                            }
                                        }
                                    } catch (e: Exception) {
                                        error = e.message
                                    }
                                }
                            }) { Text(if (g.joined) "Leave" else "Join") }
                            TextButton(onClick = {
                                expanded = if (expanded.contains(g.id)) expanded - g.id else expanded + g.id
                                if (expanded.contains(g.id)) loadEvents(g.id)
                            }) { Text("Events") }
                        }
                        if (expanded.contains(g.id)) {
                            eventsByGroup[g.id]?.forEach { ev ->
                                Row(verticalAlignment = Alignment.CenterVertically) {
                                    Text("${ev.title} (${ev.going} going)")
                                    TextButton(onClick = { rsvp(ev.id, "going", g.id) }) { Text("Going") }
                                    TextButton(onClick = { rsvp(ev.id, "interested", g.id) }) { Text("Interested") }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}
