package com.chatapp.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.chatapp.data.ApiClient
import com.chatapp.data.ChatSocket
import com.chatapp.data.Session
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.json.JSONObject

data class ChatMessage(
    val id: String,
    val senderId: String,
    val body: String,
    val encrypted: Boolean,
)

@Composable
fun ChatScreen(api: ApiClient, session: Session, wsBaseUrl: String) {
    var conversationId by remember { mutableStateOf<String?>(null) }
    var messages by remember { mutableStateOf(listOf<ChatMessage>()) }
    var draft by remember { mutableStateOf("") }
    var typing by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val token = session.accessToken ?: ""
    val myId = session.userId ?: ""
    val listState = rememberLazyListState()

    val socket = remember {
        ChatSocket(wsBaseUrl, token, onEvent = { evt ->
            when (evt.optString("type")) {
                "message" -> {
                    if (evt.optString("conversation_id") == conversationId) {
                        messages = messages + ChatMessage(
                            id = evt.getString("id"),
                            senderId = evt.getString("sender_id"),
                            body = evt.optString("body"),
                            encrypted = evt.optBoolean("is_encrypted"),
                        )
                    }
                }
                "typing" -> typing = true
            }
        })
    }

    DisposableEffect(Unit) {
        onDispose { socket.close() }
    }

    LaunchedEffect(Unit) {
        // Open the most recent conversation.
        try {
            val resp = withContext(Dispatchers.IO) { api.get("/api/conversations", token) }
            val arr = JSONObject(resp).getJSONArray("conversations")
            if (arr.length() > 0) {
                val convId = arr.getJSONObject(0).getString("id")
                conversationId = convId
                val history = withContext(Dispatchers.IO) {
                    api.get("/api/conversations/$convId/messages", token)
                }
                val msgs = JSONObject(history).getJSONArray("messages")
                val parsed = mutableListOf<ChatMessage>()
                for (i in msgs.length() - 1 downTo 0) {
                    val m = msgs.getJSONObject(i)
                    parsed.add(
                        ChatMessage(
                            id = m.getString("id"),
                            senderId = m.getString("sender_id"),
                            body = m.optString("body"),
                            encrypted = m.optBoolean("is_encrypted"),
                        )
                    )
                }
                messages = parsed
                withContext(Dispatchers.IO) {
                    api.post("/api/conversations/$convId/read", "{}", token)
                }
            }
        } catch (e: Exception) {
            error = "Failed to load chat"
        }
    }

    LaunchedEffect(messages.size) {
        if (messages.isNotEmpty()) listState.animateScrollToItem(messages.size - 1)
    }

    Column(modifier = Modifier.fillMaxSize().padding(16.dp)) {
        Text("Chat", style = MaterialTheme.typography.headlineSmall)
        if (typing) Text("typing…", style = MaterialTheme.typography.bodySmall)
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        LazyColumn(
            state = listState,
            verticalArrangement = Arrangement.spacedBy(6.dp),
            modifier = Modifier.weight(1f).padding(vertical = 8.dp),
        ) {
            items(messages, key = { it.id }) { m ->
                val mine = m.senderId == myId
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = if (mine) Arrangement.End else Arrangement.Start,
                ) {
                    Card {
                        Text(
                            (if (m.encrypted) "🔒 " else "") + m.body,
                            modifier = Modifier.padding(10.dp),
                        )
                    }
                }
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedTextField(
                value = draft,
                onValueChange = {
                    draft = it
                    conversationId?.let(socket::sendTyping)
                },
                placeholder = { Text("Message") },
                modifier = Modifier.weight(1f),
            )
            Button(onClick = {
                conversationId?.let { socket.sendMessage(it, draft) }
                draft = ""
            }) { Text("Send") }
        }
    }
}
