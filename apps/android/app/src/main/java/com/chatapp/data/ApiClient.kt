package com.chatapp.data

import java.io.IOException
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody

// Thin OkHttp wrapper matching the Go API contract: JSON bodies, bearer
// tokens, string error payload from {"error": ...}. Callers run these on
// Dispatchers.IO.
class ApiClient(private val baseUrl: String) {

    private val client = OkHttpClient()
    private val jsonMedia = "application/json".toMediaType()

    fun get(path: String, token: String? = null): String =
        execute(newRequest(path, token).get().build())

    fun post(path: String, body: String = "{}", token: String? = null): String =
        execute(newRequest(path, token).post(body.toRequestBody(jsonMedia)).build())

    fun put(path: String, body: String = "{}", token: String? = null): String =
        execute(newRequest(path, token).put(body.toRequestBody(jsonMedia)).build())

    fun delete(path: String, body: String = "{}", token: String? = null): String =
        execute(newRequest(path, token).delete(body.toRequestBody(jsonMedia)).build())

    private fun newRequest(path: String, token: String?): Request.Builder {
        val builder = Request.Builder()
            .url("$baseUrl$path")
            .header("Content-Type", "application/json")
        if (!token.isNullOrEmpty()) builder.header("Authorization", "Bearer $token")
        return builder
    }

    private fun execute(request: Request): String {
        client.newCall(request).execute().use { resp ->
            val body = resp.body?.string().orEmpty()
            if (!resp.isSuccessful) {
                val msg = try {
                    org.json.JSONObject(body).optString("error").ifEmpty { "HTTP ${resp.code}" }
                } catch (_: Exception) { "HTTP ${resp.code}" }
                throw IOException(msg)
            }
            return body
        }
    }
}
