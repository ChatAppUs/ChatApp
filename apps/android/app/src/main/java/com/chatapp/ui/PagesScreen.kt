package com.chatapp.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
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

data class PublicPage(
    val id: String,
    val name: String,
    val category: String,
    val bio: String?,
    val followers: Int,
    val isFollowing: Boolean?,
)

@Composable
fun PagesScreen(api: ApiClient, session: Session) {
    var pages by remember { mutableStateOf(listOf<PublicPage>()) }
    var name by remember { mutableStateOf("") }
    var category by remember { mutableStateOf("") }
    var website by remember { mutableStateOf("") }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val token = session.accessToken ?: ""

    fun load() {
        scope.launch {
            try {
                val resp = withContext(Dispatchers.IO) { api.get("/api/pages?limit=50", token) }
                val arr = JSONObject(resp).getJSONArray("pages")
                val parsed = mutableListOf<PublicPage>()
                for (i in 0 until arr.length()) {
                    val p = arr.getJSONObject(i)
                    parsed.add(
                        PublicPage(
                            id = p.getString("id"),
                            name = p.getString("name"),
                            category = p.getString("category"),
                            bio = if (p.isNull("description")) null else p.getString("description"),
                            followers = if (p.has("follower_count")) p.optInt("follower_count") else 0,
                            isFollowing = pages.firstOrNull { it.id == p.getString("id") }?.isFollowing,
                        )
                    )
                }
                pages = parsed
            } catch (e: Exception) {
                error = e.message
            }
        }
    }

    LaunchedEffect(Unit) { load() }

    fun toggle(pageId: String, following: Boolean?) {
        scope.launch {
            try {
                if (following == true) {
                    withContext(Dispatchers.IO) { api.delete("/api/pages/$pageId/follow", "{}", token) }
                } else {
                    withContext(Dispatchers.IO) { api.post("/api/pages/$pageId/follow", "{}", token) }
                }
                val nowFollowing = following != true
                pages = pages.map { p ->
                    if (p.id == pageId) p.copy(
                        isFollowing = nowFollowing,
                        followers = (p.followers + if (nowFollowing) 1 else -1).coerceAtLeast(0),
                    ) else p
                }
            } catch (e: Exception) {
                error = e.message
            }
        }
    }

    Column(modifier = Modifier.fillMaxSize().padding(16.dp)) {
        Text("Pages", style = MaterialTheme.typography.headlineSmall)
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }

        Card(modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
            Column(modifier = Modifier.padding(12.dp)) {
                Text("New page", style = MaterialTheme.typography.titleMedium)
                OutlinedTextField(value = name, onValueChange = { name = it }, label = { Text("Name") })
                OutlinedTextField(value = category, onValueChange = { category = it }, label = { Text("Category") })
                OutlinedTextField(value = website, onValueChange = { website = it }, label = { Text("Description") })
                Button(onClick = {
                    scope.launch {
                        try {
                            val body = JSONObject().put("name", name).put("category", category)
                                .put("description", website).toString()
                            withContext(Dispatchers.IO) { api.post("/api/pages", body, token) }
                            name = ""; category = ""; website = ""
                            load()
                        } catch (e: Exception) {
                            error = e.message
                        }
                    }
                }, enabled = name.isNotBlank() && category.isNotBlank()) { Text("Create") }
            }
        }

        LazyColumn(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            items(pages) { p ->
                Card(modifier = Modifier.fillMaxWidth()) {
                    Column(modifier = Modifier.padding(12.dp)) {
                        Text(p.name, style = MaterialTheme.typography.titleMedium)
                        Text(p.category, style = MaterialTheme.typography.bodySmall)
                        p.bio?.let { Text(it) }
                        Button(onClick = { toggle(p.id, p.isFollowing) }) {
                            Text(if (p.isFollowing == true) "Unfollow" else "Follow")
                        }
                    }
                }
            }
        }
    }
}
