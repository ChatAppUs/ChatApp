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

data class ChatTurn(val role: String, val content: String)
data class AssistantAction(val id: String, val kind: String, val status: String)

// Android in-app AI assistant (master plan §38): conversations, messages and
// the pending-action queue. Any action the assistant proposes must be decided
// explicitly by the user — the API never applies one automatically, and when
// no assistant model is configured the reply states that honestly.
@Composable
fun AssistantScreen(api: ApiClient, session: Session) {
    var conversations by remember { mutableStateOf(listOf<String>()) }
    var activeConv by remember { mutableStateOf<String?>(null) }
    var turns by remember { mutableStateOf(listOf<ChatTurn>()) }
    var actions by remember { mutableStateOf(listOf<AssistantAction>()) }
    var input by remember { mutableStateOf("") }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val token = session.accessToken ?: ""

    fun load() {
        scope.launch {
            try {
                val c = withContext(Dispatchers.IO) { api.get("/api/assistant/conversations", token) }
                val arr = JSONObject(c).getJSONArray("conversations")
                val ids = mutableListOf<String>()
                for (i in 0 until arr.length()) ids.add(arr.getJSONObject(i).getString("id"))
                conversations = ids
                if (activeConv == null && ids.isNotEmpty()) activeConv = ids.first()
                val a = withContext(Dispatchers.IO) { api.get("/api/assistant/actions", token) }
                val aa = JSONObject(a).getJSONArray("actions")
                val parsed = mutableListOf<AssistantAction>()
                for (i in 0 until aa.length()) {
                    val j = aa.getJSONObject(i)
                    parsed.add(
                        AssistantAction(
                            id = j.getString("id"),
                            kind = j.optString("kind"),
                            status = j.optString("status"),
                        )
                    )
                }
                actions = parsed
            } catch (e: Exception) {
                error = e.message
            }
        }
    }

    LaunchedEffect(Unit) { load() }

    Column(modifier = Modifier.fillMaxSize().padding(16.dp)) {
        Text("Assistant", style = MaterialTheme.typography.headlineSmall)
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }

        Card(modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
            Column(modifier = Modifier.padding(12.dp)) {
                Text("Conversations", style = MaterialTheme.typography.titleMedium)
                Row {
                    TextButton(onClick = {
                        scope.launch {
                            try {
                                val body = JSONObject().put("title", "New conversation").toString()
                                val resp = withContext(Dispatchers.IO) {
                                    api.post("/api/assistant/conversations", body, token)
                                }
                                activeConv = JSONObject(resp).optString("id")
                                turns = emptyList()
                                load()
                            } catch (e: Exception) {
                                error = e.message
                            }
                        }
                    }) { Text("New") }
                    conversations.take(4).forEach { id ->
                        TextButton(onClick = { activeConv = id; turns = emptyList() }) { Text(id.take(6)) }
                    }
                }
            }
        }

        LazyColumn(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            items(turns) { t ->
                Card(modifier = Modifier.fillMaxWidth()) {
                    Column(modifier = Modifier.padding(10.dp)) {
                        Text(t.role, style = MaterialTheme.typography.labelSmall)
                        Text(t.content)
                    }
                }
            }
            if (actions.isNotEmpty()) {
                item { Text("Pending actions", style = MaterialTheme.typography.titleMedium) }
                items(actions) { a ->
                    Card(modifier = Modifier.fillMaxWidth()) {
                        Column(modifier = Modifier.padding(10.dp)) {
                            Text("${a.kind} · ${a.status}", style = MaterialTheme.typography.bodySmall)
                            Row {
                                TextButton(onClick = {
                                    scope.launch {
                                        try {
                                            val body = JSONObject().put("approve", true).toString()
                                            withContext(Dispatchers.IO) {
                                                api.post("/api/assistant/actions/${a.id}/decide", body, token)
                                            }
                                            load()
                                        } catch (e: Exception) {
                                            error = e.message
                                        }
                                    }
                                }) { Text("Approve") }
                                TextButton(onClick = {
                                    scope.launch {
                                        try {
                                            val body = JSONObject().put("approve", false).toString()
                                            withContext(Dispatchers.IO) {
                                                api.post("/api/assistant/actions/${a.id}/decide", body, token)
                                            }
                                            load()
                                        } catch (e: Exception) {
                                            error = e.message
                                        }
                                    }
                                }) { Text("Reject") }
                            }
                        }
                    }
                }
            }
        }

        Row(modifier = Modifier.fillMaxWidth()) {
            OutlinedTextField(value = input, onValueChange = { input = it }, label = { Text("Ask the assistant") })
            Button(onClick = {
                val conv = activeConv ?: return@Button
                val asked = input
                turns = turns + ChatTurn("user", asked)
                input = ""
                scope.launch {
                    try {
                        val body = JSONObject().put("body", asked).toString()
                        val resp = withContext(Dispatchers.IO) {
                            api.post("/api/assistant/conversations/$conv/messages", body, token)
                        }
                        val j = JSONObject(resp)
                        val reply = j.optString("reply").ifEmpty {
                            "Unavailable: " + j.optString("reason", "no assistant model configured")
                        }
                        turns = turns + ChatTurn("assistant", reply)
                        load()
                    } catch (e: Exception) {
                        error = e.message
                    }
                }
            }, enabled = input.isNotBlank() && activeConv != null) { Text("Send") }
        }
    }
}
