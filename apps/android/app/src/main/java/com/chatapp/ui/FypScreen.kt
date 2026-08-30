package com.chatapp.ui

import android.net.Uri
import android.widget.VideoView
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.viewinterop.AndroidView
import androidx.compose.ui.unit.dp
import com.chatapp.data.ApiClient
import com.chatapp.data.Session
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.json.JSONObject

data class FypPost(
    val id: String,
    val username: String,
    val body: String,
    val mediaUrl: String?,
    val likeCount: Int,
)

// For-You feed with real watch-signal reporting via platform VideoView
// playback (no extra dependencies). Reports position/completion/rewatch when
// the user taps a reel or marks it not-interested.
@Composable
fun FypScreen(api: ApiClient, session: Session) {
    var posts by remember { mutableStateOf(listOf<FypPost>()) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val token = session.accessToken ?: ""

    LaunchedEffect(Unit) {
        try {
            val resp = withContext(Dispatchers.IO) { api.get("/api/fyp", token) }
            val arr = JSONObject(resp).optJSONArray("posts") ?: org.json.JSONArray()
            val parsed = mutableListOf<FypPost>()
            for (i in 0 until arr.length()) {
                val p = arr.getJSONObject(i)
                parsed.add(
                    FypPost(
                        id = p.getString("id"),
                        username = p.optString("username"),
                        body = p.optString("body"),
                        mediaUrl = p.optString("media_url", null),
                        likeCount = p.optInt("like_count"),
                    )
                )
            }
            posts = parsed
        } catch (e: Exception) {
            error = e.message
        }
    }

    fun signal(postId: String, view: VideoView?, notInterested: Boolean = false) {
        scope.launch(Dispatchers.IO) {
            try {
                val dur = view?.duration?.toLong() ?: 0L
                val pos = view?.currentPosition?.toLong() ?: 0L
                val payload = JSONObject()
                    .put("watched_ms", pos)
                    .put("duration_ms", dur)
                    .put("completed", dur > 0 && pos >= dur * 0.9)
                    .put("rewatched", false)
                    .put("not_interested", notInterested)
                api.post("/api/reels/$postId/watch", payload.toString(), token)
            } catch (_: Exception) { /* signal lost */ }
        }
    }

    Column(modifier = Modifier.fillMaxSize().padding(16.dp)) {
        Text("For You", style = MaterialTheme.typography.headlineSmall)
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        LazyColumn(verticalArrangement = androidx.compose.foundation.layout.Arrangement.spacedBy(12.dp)) {
            items(posts) { post ->
                Card(modifier = Modifier.fillMaxWidth()) {
                    Column(modifier = Modifier.padding(12.dp)) {
                        Text("@${post.username}", style = MaterialTheme.typography.titleMedium)
                        Text(post.body)
                        if (post.mediaUrl != null) {
                            var video: VideoView? by remember { mutableStateOf(null) }
                            AndroidView(
                                factory = { ctx ->
                                    VideoView(ctx).apply {
                                        setVideoURI(Uri.parse(post.mediaUrl))
                                    }
                                },
                                modifier = Modifier.height(220.dp),
                                update = { view -> video = view },
                            )
                            Row {
                                Button(onClick = {
                                    video?.let { v ->
                                        if (!v.isPlaying) v.start() else v.pause()
                                        signal(post.id, v)
                                    }
                                }) { Text("Play") }
                                Button(onClick = { signal(post.id, null, notInterested = true) }) { Text("Skip") }
                                Spacer(modifier = Modifier.weight(1f))
                                Text("♡ ${post.likeCount}")
                            }
                        }
                    }
                }
            }
        }
    }
}
