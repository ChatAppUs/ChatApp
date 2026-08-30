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

data class FeedPost(
    val id: String,
    val authorId: String,
    val author: String,
    val body: String,
    val likeCount: Int,
    val commentCount: Int,
    val likedByMe: Boolean,
    val myReaction: String,
    val feeling: String,
    val location: String,
    val edited: Boolean,
)

private val REACTION_EMOJI = mapOf(
    "like" to "👍", "love" to "❤️", "haha" to "😂",
    "wow" to "😮", "sad" to "😢", "angry" to "😡",
)

@Composable
fun FeedScreen(api: ApiClient, session: Session, onOpenChat: () -> Unit) {
    var posts by remember { mutableStateOf(listOf<FeedPost>()) }
    var draft by remember { mutableStateOf("") }
    var feeling by remember { mutableStateOf("") }
    var location by remember { mutableStateOf("") }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val token = session.accessToken ?: ""
    val myId = session.userId ?: ""

    fun load() {
        scope.launch {
            try {
                val resp = withContext(Dispatchers.IO) { api.get("/api/feed", token) }
                val arr = JSONObject(resp).getJSONArray("posts")
                val parsed = mutableListOf<FeedPost>()
                for (i in 0 until arr.length()) {
                    val p = arr.getJSONObject(i)
                    parsed.add(
                        FeedPost(
                            id = p.getString("id"),
                            authorId = p.getString("author_id"),
                            author = p.getString("author_name"),
                            body = p.optString("body"),
                            likeCount = p.optInt("like_count"),
                            commentCount = p.optInt("comment_count"),
                            likedByMe = p.optBoolean("liked_by_me"),
                            myReaction = p.optString("my_reaction"),
                            feeling = p.optString("feeling"),
                            location = p.optString("location"),
                            edited = !p.isNull("edited_at") && p.optString("edited_at").isNotEmpty(),
                        )
                    )
                }
                posts = parsed
            } catch (e: Exception) {
                error = "Failed to load feed"
            }
        }
    }

    LaunchedEffect(Unit) { load() }

    Column(modifier = Modifier.fillMaxSize().padding(16.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text("Feed", style = MaterialTheme.typography.headlineSmall, modifier = Modifier.weight(1f))
            TextButton(onClick = onOpenChat) { Text("Chat") }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedTextField(
                value = draft,
                onValueChange = { draft = it },
                placeholder = { Text("What's happening? #hashtags work") },
                modifier = Modifier.weight(1f),
            )
            Button(onClick = {
                scope.launch {
                    try {
                        val body = JSONObject().put("type", "post").put("body", draft)
                        if (feeling.isNotBlank()) body.put("feeling", feeling.trim())
                        if (location.isNotBlank()) body.put("location", location.trim())
                        withContext(Dispatchers.IO) { api.post("/api/posts", body.toString(), token) }
                        draft = ""
                        feeling = ""
                        location = ""
                        load()
                    } catch (e: Exception) {
                        error = "Post failed"
                    }
                }
            }) { Text("Post") }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedTextField(
                value = feeling,
                onValueChange = { feeling = it },
                placeholder = { Text("Feeling/activity") },
                modifier = Modifier.weight(1f),
            )
            OutlinedTextField(
                value = location,
                onValueChange = { location = it },
                placeholder = { Text("Location") },
                modifier = Modifier.weight(1f),
            )
        }
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        LazyColumn(
            verticalArrangement = Arrangement.spacedBy(8.dp),
            modifier = Modifier.padding(top = 12.dp),
        ) {
            items(posts, key = { it.id }) { post ->
                Card(modifier = Modifier.fillMaxWidth()) {
                    Column(modifier = Modifier.padding(12.dp)) {
                        Text(post.author, style = MaterialTheme.typography.titleSmall)
                        if (post.feeling.isNotEmpty() || post.location.isNotEmpty()) {
                            Text(
                                listOfNotNull(
                                    post.feeling.takeIf { it.isNotEmpty() }?.let { "is $it" },
                                    post.location.takeIf { it.isNotEmpty() }?.let { "at $it" },
                                ).joinToString(" "),
                                style = MaterialTheme.typography.bodySmall,
                            )
                        }
                        Text(post.body, modifier = Modifier.padding(vertical = 6.dp))
                        Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                            REACTION_EMOJI.forEach { (kind, emoji) ->
                                TextButton(onClick = {
                                    scope.launch {
                                        try {
                                            withContext(Dispatchers.IO) {
                                                if (post.myReaction == kind) {
                                                    api.delete("/api/posts/${post.id}/react", token)
                                                } else {
                                                    api.put("/api/posts/${post.id}/react",
                                                        JSONObject().put("reaction", kind).toString(), token)
                                                }
                                            }
                                            load()
                                        } catch (_: Exception) {
                                        }
                                    }
                                }) {
                                    Text(if (post.myReaction == kind) "$emoji✓" else emoji)
                                }
                            }
                        }
                        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                            Text("👍 ${post.likeCount}", modifier = Modifier.padding(top = 12.dp))
                            Text("💬 ${post.commentCount}", modifier = Modifier.padding(top = 12.dp))
                            if (post.edited) {
                                Text("edited", style = MaterialTheme.typography.bodySmall,
                                    modifier = Modifier.padding(top = 12.dp))
                            }
                            if (post.authorId == myId) {
                                TextButton(onClick = {
                                    scope.launch {
                                        try {
                                            withContext(Dispatchers.IO) {
                                                api.put("/api/me/pinned-post",
                                                    JSONObject().put("post_id", post.id).toString(), token)
                                            }
                                        } catch (_: Exception) {
                                        }
                                    }
                                }) { Text("📌 Pin") }
                            }
                        }
                    }
                }
            }
        }
    }
}
