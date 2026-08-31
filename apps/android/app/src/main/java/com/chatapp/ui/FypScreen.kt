package com.chatapp.ui

import android.net.Uri
import android.widget.VideoView
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.FilterChip
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.viewinterop.AndroidView
import androidx.compose.ui.unit.dp
import com.chatapp.BuildConfig
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
    val remixMode: String = "",
    val remixOf: String = "",
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
                        remixMode = p.optString("remix_mode", ""),
                        remixOf = p.optString("remix_of", ""),
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

    var remixTarget by remember { mutableStateOf<FypPost?>(null) }

    remixTarget?.let { target ->
        RemixDialog(
            api = api,
            token = token,
            reelId = target.id,
            onDone = { remixTarget = null },
        )
    }

    Column(modifier = Modifier.fillMaxSize().padding(16.dp)) {
        Text("For You", style = MaterialTheme.typography.headlineSmall)
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        LazyColumn(verticalArrangement = androidx.compose.foundation.layout.Arrangement.spacedBy(12.dp)) {
            items(posts) { post ->
                Card(modifier = Modifier.fillMaxWidth()) {
                    Column(modifier = Modifier.padding(12.dp)) {
                        Text("@${post.username}", style = MaterialTheme.typography.titleMedium)
                        if (post.remixMode.isNotEmpty()) {
                            Text("🎬 ${post.remixMode}", style = MaterialTheme.typography.labelSmall)
                        }
                        Text(post.body)
                        if (post.mediaUrl != null) {
                            var video: VideoView? by remember { mutableStateOf(null) }
                            if (post.remixMode.isNotEmpty() && post.remixOf.isNotEmpty()) {
                                RemixVideo(api = api, token = token, post = post)
                            } else {
                                AndroidView(
                                    factory = { ctx ->
                                        VideoView(ctx).apply {
                                            setVideoURI(Uri.parse(post.mediaUrl))
                                        }
                                    },
                                    modifier = Modifier.height(220.dp),
                                    update = { view -> video = view },
                                )
                            }
                            Row {
                                Button(onClick = {
                                    video?.let { v ->
                                        if (!v.isPlaying) v.start() else v.pause()
                                        signal(post.id, v)
                                    }
                                }) { Text("Play") }
                                Button(onClick = { signal(post.id, null, notInterested = true) }) { Text("Skip") }
                                Button(onClick = { remixTarget = post }) { Text("🎬") }
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

// Duet/stitch playback: fetches the source reel through the permalink
// endpoint. Duet renders source and response side-by-side; stitch plays the
// source clip once, then hands over to the response.
@Composable
fun RemixVideo(api: ApiClient, token: String, post: FypPost) {
    var sourceUrl by remember { mutableStateOf<String?>(null) }
    var sourceResolved by remember { mutableStateOf(false) }
    LaunchedEffect(post.remixOf) {
        try {
            val resp = withContext(Dispatchers.IO) { api.get("/api/posts/${post.remixOf}", token) }
            val media = JSONObject(resp).getJSONObject("post").optJSONArray("media")
            if (media != null) {
                for (i in 0 until media.length()) {
                    val m = media.getJSONObject(i)
                    if (m.optString("kind") == "video") {
                        sourceUrl = m.getString("url")
                        break
                    }
                }
            }
        } catch (_: Exception) { /* source unavailable; play response alone */ }
        sourceResolved = true
    }
    if (post.remixMode == "duet") {
        Row(modifier = Modifier.fillMaxWidth().height(220.dp)) {
            sourceUrl?.let { src ->
                AndroidView(
                    factory = { ctx ->
                        VideoView(ctx).apply {
                            setVideoURI(Uri.parse(src))
                            setOnPreparedListener { it.isLooping = true; it.setVolume(0f, 0f); start() }
                        }
                    },
                    modifier = Modifier.weight(1f).height(220.dp),
                )
            }
            AndroidView(
                factory = { ctx ->
                    VideoView(ctx).apply {
                        setVideoURI(Uri.parse(post.mediaUrl))
                        setOnPreparedListener { it.isLooping = true }
                    }
                },
                modifier = Modifier.weight(1f).height(220.dp),
            )
        }
    } else if (sourceResolved) {
        // stitch: source clip first, then the response loops
        val src = sourceUrl
        AndroidView(
            factory = { ctx ->
                VideoView(ctx).apply {
                    val own = post.mediaUrl!!
                    if (src != null) {
                        setVideoURI(Uri.parse(src))
                        setOnCompletionListener {
                            setVideoURI(Uri.parse(own))
                            setOnPreparedListener { it.isLooping = true }
                            start()
                        }
                    } else {
                        setVideoURI(Uri.parse(own))
                        setOnPreparedListener { it.isLooping = true }
                    }
                }
            },
            modifier = Modifier.fillMaxWidth().height(220.dp),
        )
    }
}

// Remix composer: caption + layout (remix / duet / stitch) + optional video
// picked from the device, uploaded via the signed-grant media flow.
@Composable
fun RemixDialog(api: ApiClient, token: String, reelId: String, onDone: () -> Unit) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var body by remember { mutableStateOf("") }
    var mode by remember { mutableStateOf("") }
    var picked by remember { mutableStateOf<Uri?>(null) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.GetContent()) { uri ->
        if (uri != null) picked = uri
    }

    AlertDialog(
        onDismissRequest = { if (!busy) onDone() },
        title = { Text("Remix this reel") },
        text = {
            Column {
                Row {
                    listOf("" to "Remix", "duet" to "Duet", "stitch" to "Stitch").forEach { (v, label) ->
                        FilterChip(
                            selected = mode == v,
                            onClick = { mode = v },
                            label = { Text(label) },
                            modifier = Modifier.padding(end = 4.dp),
                        )
                    }
                }
                OutlinedTextField(
                    value = body,
                    onValueChange = { body = it },
                    label = { Text("Add your take…") },
                    modifier = Modifier.fillMaxWidth(),
                )
                TextButton(onClick = { picker.launch("video/*") }) {
                    Text(if (picked == null) "Pick video (optional)" else "Video selected ✓")
                }
                error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            }
        },
        confirmButton = {
            Button(
                enabled = !busy && (body.isNotBlank() || picked != null),
                onClick = {
                    busy = true
                    error = null
                    scope.launch {
                        try {
                            val media = org.json.JSONArray()
                            picked?.let { uri ->
                                val bytes = withContext(Dispatchers.IO) {
                                    context.contentResolver.openInputStream(uri)?.use { it.readBytes() }
                                } ?: throw IllegalStateException("cannot read picked video")
                                val name = uri.lastPathSegment ?: "remix.mp4"
                                val url = withContext(Dispatchers.IO) {
                                    api.uploadMedia(BuildConfig.MEDIA_BASE_URL, name, bytes, token)
                                }
                                media.put(JSONObject().put("kind", "video").put("url", url))
                            }
                            val payload = JSONObject()
                                .put("type", "reel")
                                .put("body", body)
                                .put("remix_of", reelId)
                                .put("media", media)
                            if (mode.isNotEmpty()) payload.put("remix_mode", mode)
                            withContext(Dispatchers.IO) { api.post("/api/posts", payload.toString(), token) }
                            onDone()
                        } catch (e: Exception) {
                            error = e.message
                            busy = false
                        }
                    }
                },
            ) { Text(if (busy) "Posting…" else "Post remix") }
        },
        dismissButton = {
            TextButton(onClick = { if (!busy) onDone() }) { Text("Cancel") }
        },
    )
}
