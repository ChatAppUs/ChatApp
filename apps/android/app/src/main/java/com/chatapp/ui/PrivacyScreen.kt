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

data class PrivacyUser(val id: String, val username: String)
data class FollowReq(val id: String, val username: String)
data class MsgReq(val conversationId: String, val username: String, val preview: String)

@Composable
fun PrivacyScreen(api: ApiClient, session: Session) {
    var mutes by remember { mutableStateOf(listOf<PrivacyUser>()) }
    var restricted by remember { mutableStateOf(listOf<PrivacyUser>()) }
    var filters by remember { mutableStateOf(listOf<String>()) }
    var followReqs by remember { mutableStateOf(listOf<FollowReq>()) }
    var msgReqs by remember { mutableStateOf(listOf<MsgReq>()) }
    var phrase by remember { mutableStateOf("") }
    var locked by remember { mutableStateOf(false) }
    var showActive by remember { mutableStateOf(true) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val token = session.accessToken ?: ""

    fun load() {
        scope.launch {
            try {
                val mu = withContext(Dispatchers.IO) { api.get("/api/me/mutes", token) }
                val muArr = JSONObject(mu).getJSONArray("mutes")
                mutes = (0 until muArr.length()).map { i ->
                    val o = muArr.getJSONObject(i)
                    PrivacyUser(o.getString("id"), o.getString("username"))
                }
                val re = withContext(Dispatchers.IO) { api.get("/api/me/restricted", token) }
                val reArr = JSONObject(re).getJSONArray("restricted")
                restricted = (0 until reArr.length()).map { i ->
                    val o = reArr.getJSONObject(i)
                    PrivacyUser(o.getString("id"), o.getString("username"))
                }
                val wf = withContext(Dispatchers.IO) { api.get("/api/me/word-filters", token) }
                val wfArr = JSONObject(wf).getJSONArray("filters")
                filters = (0 until wfArr.length()).map { wfArr.getJSONObject(it).getString("phrase") }
                val fr = withContext(Dispatchers.IO) { api.get("/api/me/follow-requests", token) }
                val frArr = JSONObject(fr).getJSONArray("requests")
                followReqs = (0 until frArr.length()).map { i ->
                    val o = frArr.getJSONObject(i)
                    FollowReq(o.getString("id"), o.getString("username"))
                }
                val mr = withContext(Dispatchers.IO) { api.get("/api/me/message-requests", token) }
                val mrArr = JSONObject(mr).getJSONArray("requests")
                msgReqs = (0 until mrArr.length()).map { i ->
                    val o = mrArr.getJSONObject(i)
                    MsgReq(o.getString("conversation_id"), o.getString("username"), o.optString("preview"))
                }
            } catch (e: Exception) {
                error = e.message
            }
        }
    }

    LaunchedEffect(Unit) { load() }

    Column(modifier = Modifier.fillMaxSize().padding(16.dp)) {
        Text("Privacy", style = MaterialTheme.typography.headlineSmall)
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }

        LazyColumn(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            item {
                Card(modifier = Modifier.fillMaxWidth()) {
                    Column(modifier = Modifier.padding(12.dp)) {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Checkbox(checked = locked, onCheckedChange = { newVal ->
                                locked = newVal
                                scope.launch {
                                    try {
                                        withContext(Dispatchers.IO) {
                                            api.put("/api/me/profile-lock", JSONObject().put("locked", newVal).toString(), token)
                                        }
                                    } catch (e: Exception) {
                                        error = e.message
                                    }
                                }
                            })
                            Text("Lock profile")
                        }
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Checkbox(checked = showActive, onCheckedChange = { newVal ->
                                showActive = newVal
                                scope.launch {
                                    try {
                                        withContext(Dispatchers.IO) {
                                            api.put("/api/me/active-status", JSONObject().put("show", newVal).toString(), token)
                                        }
                                    } catch (e: Exception) {
                                        error = e.message
                                    }
                                }
                            })
                            Text("Show active status")
                        }
                    }
                }
            }

            item { Text("Follow requests (${followReqs.size})", style = MaterialTheme.typography.titleMedium) }
            items(followReqs) { r ->
                Card(modifier = Modifier.fillMaxWidth()) {
                    Row(modifier = Modifier.padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
                        Text("@${r.username}")
                        TextButton(onClick = {
                            scope.launch {
                                try {
                                    withContext(Dispatchers.IO) {
                                        api.post("/api/me/follow-requests/${r.id}/accept", "{}", token)
                                    }
                                    load()
                                } catch (e: Exception) {
                                    error = e.message
                                }
                            }
                        }) { Text("Accept") }
                        TextButton(onClick = {
                            scope.launch {
                                try {
                                    withContext(Dispatchers.IO) {
                                        api.post("/api/me/follow-requests/${r.id}/decline", "{}", token)
                                    }
                                    load()
                                } catch (e: Exception) {
                                    error = e.message
                                }
                            }
                        }) { Text("Decline") }
                    }
                }
            }

            item { Text("Message requests (${msgReqs.size})", style = MaterialTheme.typography.titleMedium) }
            items(msgReqs) { r ->
                Card(modifier = Modifier.fillMaxWidth()) {
                    Column(modifier = Modifier.padding(12.dp)) {
                        Text("@${r.username}", style = MaterialTheme.typography.titleMedium)
                        if (r.preview.isNotEmpty()) Text(r.preview, style = MaterialTheme.typography.bodySmall)
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            TextButton(onClick = {
                                scope.launch {
                                    try {
                                        withContext(Dispatchers.IO) {
                                            api.post("/api/me/message-requests/${r.conversationId}/accept", "{}", token)
                                        }
                                        load()
                                    } catch (e: Exception) {
                                        error = e.message
                                    }
                                }
                            }) { Text("Accept") }
                            TextButton(onClick = {
                                scope.launch {
                                    try {
                                        withContext(Dispatchers.IO) {
                                            api.post("/api/me/message-requests/${r.conversationId}/decline", "{}", token)
                                        }
                                        load()
                                    } catch (e: Exception) {
                                        error = e.message
                                    }
                                }
                            }) { Text("Decline") }
                        }
                    }
                }
            }

            item { Text("Muted", style = MaterialTheme.typography.titleMedium) }
            items(mutes) { u ->
                Card(modifier = Modifier.fillMaxWidth()) {
                    Row(modifier = Modifier.padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
                        Text("@${u.username}")
                        TextButton(onClick = {
                            scope.launch {
                                try {
                                    withContext(Dispatchers.IO) { api.delete("/api/users/${u.id}/mute", "{}", token) }
                                    load()
                                } catch (e: Exception) {
                                    error = e.message
                                }
                            }
                        }) { Text("Unmute") }
                    }
                }
            }

            item { Text("Restricted", style = MaterialTheme.typography.titleMedium) }
            items(restricted) { u ->
                Card(modifier = Modifier.fillMaxWidth()) {
                    Row(modifier = Modifier.padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
                        Text("@${u.username}")
                        TextButton(onClick = {
                            scope.launch {
                                try {
                                    withContext(Dispatchers.IO) { api.delete("/api/users/${u.id}/restrict", "{}", token) }
                                    load()
                                } catch (e: Exception) {
                                    error = e.message
                                }
                            }
                        }) { Text("Remove") }
                    }
                }
            }

            item {
                Text("Word filters", style = MaterialTheme.typography.titleMedium)
                Card(modifier = Modifier.fillMaxWidth()) {
                    Column(modifier = Modifier.padding(12.dp)) {
                        OutlinedTextField(value = phrase, onValueChange = { phrase = it }, label = { Text("Phrase") })
                        Button(onClick = {
                            scope.launch {
                                try {
                                    withContext(Dispatchers.IO) {
                                        api.post("/api/me/word-filters", JSONObject().put("phrase", phrase).toString(), token)
                                    }
                                    phrase = ""
                                    load()
                                } catch (e: Exception) {
                                    error = e.message
                                }
                            }
                        }) { Text("Add") }
                    }
                }
            }
            items(filters) { f ->
                Card(modifier = Modifier.fillMaxWidth()) {
                    Row(modifier = Modifier.padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
                        Text(f)
                        TextButton(onClick = {
                            scope.launch {
                                try {
                                    withContext(Dispatchers.IO) { api.delete("/api/me/word-filters", JSONObject().put("phrase", f).toString(), token) }
                                    load()
                                } catch (e: Exception) {
                                    error = e.message
                                }
                            }
                        }) { Text("Remove") }
                    }
                }
            }
        }
    }
}
