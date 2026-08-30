package com.chatapp.data

import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import org.json.JSONObject

// OkHttp WebSocket matching the Go API socket contract: text frames carry
// JSON events. auth travels on the URL query since WS upgrade can't inject
// Authorization headers here.
class ChatSocket(
    wsBaseUrl: String,
    token: String,
    private val onEvent: (JSONObject) -> Unit,
) {
    private val client = OkHttpClient()
    private var socket: WebSocket? = null

    init {
        val url = "$wsBaseUrl/ws?token=$token"
        val request = Request.Builder().url(url).build()
        socket = client.newWebSocket(request, object : WebSocketListener() {
            override fun onMessage(webSocket: WebSocket, text: String) {
                try {
                    onEvent(JSONObject(text))
                } catch (_: Exception) { /* malformed frame */ }
            }
        })
    }

    fun sendMessage(conversationId: String, body: String) {
        send(
            JSONObject()
                .put("type", "message")
                .put("conversation_id", conversationId)
                .put("body", body)
                .toString()
        )
    }

    fun sendSignal(conversationId: String, signal: JSONObject) {
        send(
            JSONObject()
                .put("type", "signal")
                .put("conversation_id", conversationId)
                .put("signal", signal)
                .toString()
        )
    }

    fun sendTyping(conversationId: String) {
        send(
            JSONObject()
                .put("type", "typing")
                .put("conversation_id", conversationId)
                .toString()
        )
    }

    fun send(payload: String) {
        socket?.send(payload)
    }

    fun close() {
        socket?.close(1000, "bye")
        socket = null
    }
}
