package com.chatapp.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import com.chatapp.data.ApiClient
import com.chatapp.data.Session
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.json.JSONObject

@Composable
fun LoginScreen(api: ApiClient, session: Session, onLoggedIn: () -> Unit) {
    var identifier by remember { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    var totp by remember { mutableStateOf("") }
    var needs2fa by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    Column(
        modifier = Modifier.fillMaxSize().padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text("ChatApp", style = MaterialTheme.typography.headlineLarge)
        OutlinedTextField(
            value = identifier,
            onValueChange = { identifier = it },
            label = { Text("Username / email / phone") },
            modifier = Modifier.fillMaxWidth(),
        )
        OutlinedTextField(
            value = password,
            onValueChange = { password = it },
            label = { Text("Password") },
            visualTransformation = PasswordVisualTransformation(),
            modifier = Modifier.fillMaxWidth(),
        )
        if (needs2fa) {
            OutlinedTextField(
                value = totp,
                onValueChange = { totp = it },
                label = { Text("2FA code") },
                modifier = Modifier.fillMaxWidth(),
            )
        }
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        Button(
            enabled = !busy,
            modifier = Modifier.fillMaxWidth(),
            onClick = {
                busy = true
                error = null
                scope.launch {
                    try {
                        val body = JSONObject()
                            .put("identifier", identifier)
                            .put("password", password)
                            .put("totp_code", totp)
                            .toString()
                        val resp = withContext(Dispatchers.IO) { api.post("/api/auth/login", body, null) }
                        val json = JSONObject(resp)
                        session.accessToken = json.getString("access_token")
                        session.refreshToken = json.getString("refresh_token")
                        session.userId = json.getString("user_id")
                        onLoggedIn()
                    } catch (e: ApiClient.ApiException) {
                        if (e.message?.contains("totp_required") == true) {
                            needs2fa = true
                            error = "Enter your authenticator code"
                        } else {
                            error = "Login failed (${e.status})"
                        }
                    } catch (e: Exception) {
                        error = "Network error: ${e.message}"
                    } finally {
                        busy = false
                    }
                }
            },
        ) {
            Text(if (busy) "…" else "Log in")
        }
    }
}
