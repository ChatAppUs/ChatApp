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

data class PulsePost(
    val id: String,
    val author: String,
    val body: String,
    val topics: List<String>,
    val replyCount: Int,
    val repostCount: Int,
    val quoteCount: Int,
)

// Android Pulse surface (master plan §32): global/local feed, compose with
// hashtags, reply/quote/repost, and trends. Same endpoints as web /pulse.
@Composable
fun PulseScreen(api: ApiClient, session: Session) {
    var posts by remember { mutableStateOf(listOf<PulsePost>()) }
    var trends by remember { mutableStateOf(listOf<String>()) }
    var body by remember { mutableStateOf("") }
    var localOnly by remember { mutableStateOf(false) }
    var replyingTo by remember { mutableStateOf<String?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val token = session.accessToken ?: ""

    fun parse(api: ApiClient, token: String, path: String, key: String): List<PulsePost> {
        val resp = api.get(path, token)
        val arr = JSONObject(resp).getJSONArray(key)
        val parsed = mutableListOf<PulsePost>()
        for (i in 0 until arr.length()) {
            val p = arr.getJSONObject(i)
            val tags = mutableListOf<String>()
            val ta = p.optJSONArray("topics")
            if (ta != null) for (j in 0 until ta.length()) tags.add(ta.getString(j))
            parsed.add(
                PulsePost(
                    id = p.getString("id"),
                    author = p.optString("author"),
                    body = p.optString("body"),
                    topics = tags,
                    replyCount = p.optInt("reply_count"),
                    repostCount = p.optInt("repost_count"),
                    quoteCount = p.optInt("quote_count"),
                )
            )
        }
        return parsed
    }

    fun load() {
        scope.launch {
            try {
                val path = if (localOnly) "/api/pulse/posts?scope=local" else "/api/pulse/posts"
                posts = withContext(Dispatchers.IO) { parse(api, token, path, "posts") }
                val trendPath = if (localOnly) "/api/pulse/trends?scope=local" else "/api/pulse/trends"
                val tr = withContext(Dispatchers.IO) { api.get(trendPath, token) }
                val arr = JSONObject(tr).getJSONArray("trends")
                val tags = mutableListOf<String>()
                for (i in 0 until arr.length()) {
                    tags.add(arr.getJSONObject(i).optString("name"))
                }
                trends = tags
            } catch (e: Exception) {
                error = e.message
            }
        }
    }

    LaunchedEffect(localOnly) { load() }

    Column(modifier = Modifier.fillMaxSize().padding(16.dp)) {
        Text("Pulse", style = MaterialTheme.typography.headlineSmall)
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }

        Card(modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
            Column(modifier = Modifier.padding(12.dp)) {
                OutlinedTextField(
                    value = body,
                    onValueChange = { body = it },
                    label = { Text(if (replyingTo == null) "What's happening?" else "Reply") },
                )
                Row {
                    Button(onClick = {
                        scope.launch {
                            try {
                                val payload = JSONObject().put("body", body)
                                if (replyingTo != null) payload.put("parent_id", replyingTo!!)
                                withContext(Dispatchers.IO) {
                                    api.post("/api/pulse/posts", payload.toString(), token)
                                }
                                body = ""; replyingTo = null
                                load()
                            } catch (e: Exception) {
                                error = e.message
                            }
                        }
                    }, enabled = body.isNotBlank()) { Text("Post") }
                    TextButton(onClick = { localOnly = !localOnly }) {
                        Text(if (localOnly) "Local feed" else "Global feed")
                    }
                }
            }
        }

        if (trends.isNotEmpty()) {
            Text("Trends", style = MaterialTheme.typography.titleMedium)
            Row {
                trends.take(6).forEach { t -> Text("#$t  ", style = MaterialTheme.typography.bodySmall) }
            }
        }

        LazyColumn(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            items(posts) { p ->
                Card(modifier = Modifier.fillMaxWidth()) {
                    Column(modifier = Modifier.padding(12.dp)) {
                        Text("@${p.author.ifEmpty { "unknown" }}", style = MaterialTheme.typography.labelMedium)
                        Text(p.body)
                        if (p.topics.isNotEmpty()) {
                            Text(p.topics.joinToString(" ") { "#$it" }, style = MaterialTheme.typography.bodySmall)
                        }
                        Text(
                            "${p.replyCount} replies · ${p.repostCount} reposts · ${p.quoteCount} quotes",
                            style = MaterialTheme.typography.bodySmall,
                        )
                        Row {
                            TextButton(onClick = { replyingTo = p.id; body = "" }) { Text("Reply") }
                            TextButton(onClick = {
                                scope.launch {
                                    try {
                                        val payload = JSONObject().put("body", "").put("repost_of", p.id)
                                        withContext(Dispatchers.IO) {
                                            api.post("/api/pulse/posts", payload.toString(), token)
                                        }
                                        load()
                                    } catch (e: Exception) {
                                        error = e.message
                                    }
                                }
                            }) { Text("Repost") }
                            TextButton(onClick = {
                                scope.launch {
                                    try {
                                        val payload = JSONObject().put("body", body.ifBlank { "" }).put("quote_of", p.id)
                                        withContext(Dispatchers.IO) {
                                            api.post("/api/pulse/posts", payload.toString(), token)
                                        }
                                        body = ""
                                        load()
                                    } catch (e: Exception) {
                                        error = e.message
                                    }
                                }
                            }) { Text("Quote") }
                        }
                    }
                }
            }
        }
    }
}
