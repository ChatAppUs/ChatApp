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

data class ForumTag(
    val id: String,
    val title: String,
    val slug: String,
    val description: String?,
    val visibility: String,
    val topicCount: Int,
    val postCount: Int,
)

data class ForumTopic(
    val id: String,
    val title: String,
    val body: String,
    val author: String,
    val pinned: Boolean,
    val locked: Boolean,
    val postCount: Int,
)

// Android Forums surface (master plan §30): create/list forums, open topics and
// reply. Mirrors the web /forums page and hits the same endpoints.
@Composable
fun ForumsScreen(api: ApiClient, session: Session) {
    var forums by remember { mutableStateOf(listOf<ForumTag>()) }
    var topicsByForum by remember { mutableStateOf(mapOf<String, List<ForumTopic>>()) }
    var expanded by remember { mutableStateOf(setOf<String>()) }
    var title by remember { mutableStateOf("") }
    var description by remember { mutableStateOf("") }
    var newTopicTitle by remember { mutableStateOf("") }
    var replyTo by remember { mutableStateOf<String?>(null) }
    var replyText by remember { mutableStateOf("") }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val token = session.accessToken ?: ""

    fun loadForums() {
        scope.launch {
            try {
                val resp = withContext(Dispatchers.IO) { api.get("/api/forums", token) }
                val arr = JSONObject(resp).getJSONArray("forums")
                val parsed = mutableListOf<ForumTag>()
                for (i in 0 until arr.length()) {
                    val f = arr.getJSONObject(i)
                    parsed.add(
                        ForumTag(
                            id = f.getString("id"),
                            title = f.getString("title"),
                            slug = f.optString("slug"),
                            description = if (f.isNull("description")) null else f.getString("description"),
                            visibility = f.optString("visibility", "public"),
                            topicCount = f.optInt("topic_count"),
                            postCount = f.optInt("post_count"),
                        )
                    )
                }
                forums = parsed
            } catch (e: Exception) {
                error = e.message
            }
        }
    }

    fun loadTopics(forumId: String) {
        scope.launch {
            try {
                val resp = withContext(Dispatchers.IO) { api.get("/api/forums/$forumId/topics", token) }
                val arr = JSONObject(resp).getJSONArray("topics")
                val parsed = mutableListOf<ForumTopic>()
                for (i in 0 until arr.length()) {
                    val t = arr.getJSONObject(i)
                    parsed.add(
                        ForumTopic(
                            id = t.getString("id"),
                            title = t.getString("title"),
                            body = t.optString("body"),
                            author = t.optString("author"),
                            pinned = t.optBoolean("pinned"),
                            locked = t.optBoolean("locked"),
                            postCount = t.optInt("post_count"),
                        )
                    )
                }
                topicsByForum = topicsByForum + (forumId to parsed)
            } catch (e: Exception) {
                error = e.message
            }
        }
    }

    LaunchedEffect(Unit) { loadForums() }

    Column(modifier = Modifier.fillMaxSize().padding(16.dp)) {
        Text("Forums", style = MaterialTheme.typography.headlineSmall)
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }

        Card(modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
            Column(modifier = Modifier.padding(12.dp)) {
                Text("New forum", style = MaterialTheme.typography.titleMedium)
                OutlinedTextField(value = title, onValueChange = { title = it }, label = { Text("Title") })
                OutlinedTextField(value = description, onValueChange = { description = it }, label = { Text("Description") })
                Button(onClick = {
                    scope.launch {
                        try {
                            val body = JSONObject().put("title", title).put("description", description).toString()
                            withContext(Dispatchers.IO) { api.post("/api/forums", body, token) }
                            title = ""; description = ""
                            loadForums()
                        } catch (e: Exception) {
                            error = e.message
                        }
                    }
                }, enabled = title.isNotBlank()) { Text("Create") }
            }
        }

        LazyColumn(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            items(forums) { f ->
                Card(modifier = Modifier.fillMaxWidth()) {
                    Column(modifier = Modifier.padding(12.dp)) {
                        Text(f.title, style = MaterialTheme.typography.titleMedium)
                        f.description?.let { Text(it, style = MaterialTheme.typography.bodySmall) }
                        Text("${f.topicCount} topics · ${f.postCount} posts", style = MaterialTheme.typography.bodySmall)
                        TextButton(onClick = {
                            expanded = if (expanded.contains(f.id)) expanded - f.id else expanded + f.id
                            if (expanded.contains(f.id)) loadTopics(f.id)
                        }) { Text(if (expanded.contains(f.id)) "Hide topics" else "Topics") }

                        if (expanded.contains(f.id)) {
                            OutlinedTextField(
                                value = if (replyTo == f.id) newTopicTitle else newTopicTitle,
                                onValueChange = { newTopicTitle = it; replyTo = f.id },
                                label = { Text("New topic title") },
                            )
                            Button(onClick = {
                                scope.launch {
                                    try {
                                        val body = JSONObject().put("title", newTopicTitle).put("body", "").toString()
                                        withContext(Dispatchers.IO) { api.post("/api/forums/${f.id}/topics", body, token) }
                                        newTopicTitle = ""
                                        loadTopics(f.id)
                                        loadForums()
                                    } catch (e: Exception) {
                                        error = e.message
                                    }
                                }
                            }, enabled = newTopicTitle.isNotBlank()) { Text("Post topic") }

                            topicsByForum[f.id]?.forEach { t ->
                                Card(modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp)) {
                                    Column(modifier = Modifier.padding(8.dp)) {
                                        Text(
                                            (if (t.pinned) "📌 " else "") + (if (t.locked) "🔒 " else "") + t.title,
                                            style = MaterialTheme.typography.titleSmall,
                                        )
                                        Text("by ${t.author.ifEmpty { "unknown" }} · ${t.postCount} replies",
                                            style = MaterialTheme.typography.bodySmall)
                                        Row {
                                            OutlinedTextField(
                                                value = if (replyTo == t.id) replyText else "",
                                                onValueChange = { replyText = it; replyTo = t.id },
                                                label = { Text("Reply") },
                                            )
                                            TextButton(onClick = {
                                                scope.launch {
                                                    try {
                                                        val body = JSONObject().put("body", replyText).toString()
                                                        withContext(Dispatchers.IO) {
                                                            api.post("/api/forums/topics/${t.id}/posts", body, token)
                                                        }
                                                        replyText = ""
                                                        loadTopics(f.id)
                                                    } catch (e: Exception) {
                                                        error = e.message
                                                    }
                                                }
                                            }, enabled = replyText.isNotBlank() && !t.locked) { Text("Send") }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}
