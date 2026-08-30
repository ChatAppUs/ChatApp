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

data class BotItem(
    val id: String,
    val username: String,
    val displayName: String,
    val active: Boolean,
    val hasWebhook: Boolean,
    val miniAppUrl: String,
)

@Composable
fun BotsScreen(api: ApiClient, session: Session) {
    var bots by remember { mutableStateOf(listOf<BotItem>()) }
    var username by remember { mutableStateOf("") }
    var displayName by remember { mutableStateOf("") }
    var description by remember { mutableStateOf("") }
    var newToken by remember { mutableStateOf<String?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val token = session.accessToken ?: ""

    fun load() {
        scope.launch {
            try {
                val resp = withContext(Dispatchers.IO) { api.get("/api/bots", token) }
                val arr = JSONObject(resp).getJSONArray("bots")
                val parsed = mutableListOf<BotItem>()
                for (i in 0 until arr.length()) {
                    val b = arr.getJSONObject(i)
                    parsed.add(
                        BotItem(
                            id = b.getString("id"),
                            username = b.getString("username"),
                            displayName = b.getString("display_name"),
                            active = b.getBoolean("active"),
                            hasWebhook = b.getBoolean("has_webhook"),
                            miniAppUrl = b.optString("mini_app_url"),
                        )
                    )
                }
                bots = parsed
            } catch (e: Exception) {
                error = e.message
            }
        }
    }

    LaunchedEffect(Unit) { load() }

    fun create() {
        scope.launch {
            try {
                val body = JSONObject().put("username", username)
                    .put("display_name", displayName)
                    .put("description", description).toString()
                val resp = withContext(Dispatchers.IO) { api.post("/api/bots", body, token) }
                newToken = JSONObject(resp).getString("token")
                username = ""; displayName = ""; description = ""
                load()
            } catch (e: Exception) {
                error = e.message
            }
        }
    }

    Column(modifier = Modifier.fillMaxSize().padding(16.dp)) {
        Text("Bots", style = MaterialTheme.typography.headlineSmall)
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        newToken?.let {
            Card(modifier = Modifier.fillMaxWidth()) {
                Column(modifier = Modifier.padding(12.dp)) {
                    Text("Token (shown once):")
                    Text(it)
                }
            }
        }

        Card(modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
            Column(modifier = Modifier.padding(12.dp)) {
                Text("New bot", style = MaterialTheme.typography.titleMedium)
                OutlinedTextField(value = username, onValueChange = { username = it }, label = { Text("Username") })
                OutlinedTextField(value = displayName, onValueChange = { displayName = it }, label = { Text("Display name") })
                OutlinedTextField(value = description, onValueChange = { description = it }, label = { Text("Description") })
                Button(onClick = { create() }, enabled = username.isNotBlank() && displayName.isNotBlank()) { Text("Create") }
            }
        }

        LazyColumn(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            items(bots) { b ->
                Card(modifier = Modifier.fillMaxWidth()) {
                    Column(modifier = Modifier.padding(12.dp)) {
                        Text("@${b.username}", style = MaterialTheme.typography.titleMedium)
                        Text(if (b.active) "active" else "inactive")
                        Row {
                            Button(onClick = {
                                scope.launch {
                                    try {
                                        withContext(Dispatchers.IO) { api.delete("/api/bots/${b.id}", "{}", token) }
                                        load()
                                    } catch (e: Exception) {
                                        error = e.message
                                    }
                                }
                            }) { Text("Delete") }
                        }
                    }
                }
            }
        }
    }
}
