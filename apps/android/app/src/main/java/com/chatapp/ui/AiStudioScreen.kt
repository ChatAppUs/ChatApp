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

data class DubJob(
    val id: String,
    val available: Boolean,
    val reason: String,
    val targetLang: String,
    val audioUrl: String,
)

data class ClipJob(
    val id: String,
    val available: Boolean,
    val reason: String,
    val title: String,
    val score: Double,
    val approved: Boolean,
)

// Android AI Studio surface (master plan §23): AI dubbing and AI clip
// generation. Both are provider-gated — when no model is configured the API
// returns available=false with the reason, and this screen shows that honest
// state rather than pretending an artifact exists. Clip approvals require an
// explicit human tap (the API never auto-applies).
@Composable
fun AiStudioScreen(api: ApiClient, session: Session) {
    var dubs by remember { mutableStateOf(listOf<DubJob>()) }
    var clips by remember { mutableStateOf(listOf<ClipJob>()) }
    var mediaUrl by remember { mutableStateOf("") }
    var targetLang by remember { mutableStateOf("es") }
    var notice by remember { mutableStateOf<String?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val token = session.accessToken ?: ""

    fun load() {
        scope.launch {
            try {
                val d = withContext(Dispatchers.IO) { api.get("/api/ai/dubs", token) }
                val da = JSONObject(d).getJSONArray("dubs")
                val dp = mutableListOf<DubJob>()
                for (i in 0 until da.length()) {
                    val j = da.getJSONObject(i)
                    dp.add(
                        DubJob(
                            id = j.optString("id"),
                            available = j.optBoolean("available", true),
                            reason = j.optString("reason"),
                            targetLang = j.optString("target_lang"),
                            audioUrl = j.optString("audio_url"),
                        )
                    )
                }
                dubs = dp

                val c = withContext(Dispatchers.IO) { api.get("/api/ai/clips", token) }
                val ca = JSONObject(c).getJSONArray("jobs")
                val cp = mutableListOf<ClipJob>()
                for (i in 0 until ca.length()) {
                    val j = ca.getJSONObject(i)
                    val clipArr = j.optJSONArray("clips")
                    val first = if (clipArr != null && clipArr.length() > 0) clipArr.getJSONObject(0) else null
                    cp.add(
                        ClipJob(
                            id = j.optString("id"),
                            available = j.optBoolean("available", true),
                            reason = j.optString("reason"),
                            title = first?.optString("title") ?: "(no clips)",
                            score = first?.optDouble("score") ?: 0.0,
                            approved = j.optBoolean("approved", false),
                        )
                    )
                }
                clips = cp
            } catch (e: Exception) {
                error = e.message
            }
        }
    }

    LaunchedEffect(Unit) { load() }

    Column(modifier = Modifier.fillMaxSize().padding(16.dp)) {
        Text("AI Studio", style = MaterialTheme.typography.headlineSmall)
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        notice?.let { Text(it, color = MaterialTheme.colorScheme.primary) }

        Card(modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
            Column(modifier = Modifier.padding(12.dp)) {
                Text("Dub a video", style = MaterialTheme.typography.titleMedium)
                OutlinedTextField(value = mediaUrl, onValueChange = { mediaUrl = it }, label = { Text("Media URL") })
                OutlinedTextField(value = targetLang, onValueChange = { targetLang = it }, label = { Text("Target language") })
                Button(onClick = {
                    scope.launch {
                        try {
                            val body = JSONObject().put("media_url", mediaUrl).put("target_lang", targetLang).toString()
                            val resp = withContext(Dispatchers.IO) { api.post("/api/ai/dub", body, token) }
                            val j = JSONObject(resp)
                            notice = if (j.optBoolean("available", false)) {
                                "Dub queued → ${j.optString("audio_url")}"
                            } else {
                                "Dubbing unavailable: ${j.optString("reason")}"
                            }
                            load()
                        } catch (e: Exception) {
                            error = e.message
                        }
                    }
                }, enabled = mediaUrl.isNotBlank()) { Text("Dub") }
            }
        }

        Card(modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
            Column(modifier = Modifier.padding(12.dp)) {
                Text("Generate clips", style = MaterialTheme.typography.titleMedium)
                Button(onClick = {
                    scope.launch {
                        try {
                            val body = JSONObject().put("media_url", mediaUrl).toString()
                            val resp = withContext(Dispatchers.IO) { api.post("/api/ai/clips/analyze", body, token) }
                            val j = JSONObject(resp)
                            val n = j.optJSONArray("clips")?.length() ?: 0
                            notice = if (j.optBoolean("available", false)) {
                                "Found $n candidate clip(s)"
                            } else {
                                "Clip generation unavailable: ${j.optString("reason")}"
                            }
                            load()
                        } catch (e: Exception) {
                            error = e.message
                        }
                    }
                }) { Text("Analyze") }
            }
        }

        Text("Dubs", style = MaterialTheme.typography.titleMedium)
        LazyColumn(verticalArrangement = Arrangement.spacedBy(6.dp)) {
            items(dubs) { d ->
                Card(modifier = Modifier.fillMaxWidth()) {
                    Column(modifier = Modifier.padding(10.dp)) {
                        Text("→ ${d.targetLang.ifEmpty { "?" }}", style = MaterialTheme.typography.titleSmall)
                        if (d.available && d.audioUrl.isNotEmpty()) {
                            Text(d.audioUrl, style = MaterialTheme.typography.bodySmall)
                        } else {
                            Text("Unavailable${if (d.reason.isNotEmpty()) ": ${d.reason}" else ""}",
                                style = MaterialTheme.typography.bodySmall)
                        }
                    }
                }
            }
            item { Text("Clips", style = MaterialTheme.typography.titleMedium) }
            items(clips) { c ->
                Card(modifier = Modifier.fillMaxWidth()) {
                    Column(modifier = Modifier.padding(10.dp)) {
                        Text(c.title, style = MaterialTheme.typography.titleSmall)
                        Text("score ${"%.2f".format(c.score)} · ${if (c.approved) "approved" else "pending approval"}",
                            style = MaterialTheme.typography.bodySmall)
                        if (!c.available && c.reason.isNotEmpty()) {
                            Text(c.reason, style = MaterialTheme.typography.bodySmall)
                        }
                        TextButton(onClick = {
                            scope.launch {
                                try {
                                    val body = JSONObject().put("approve", true).toString()
                                    withContext(Dispatchers.IO) {
                                        api.post("/api/ai/clips/${c.id}/approve", body, token)
                                    }
                                    notice = "Clip ${c.id} approved"
                                    load()
                                } catch (e: Exception) {
                                    error = e.message
                                }
                            }
                        }) { Text("Approve") }
                    }
                }
            }
        }
    }
}
