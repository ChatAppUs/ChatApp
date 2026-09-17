package com.chatapp.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
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
fun LoginScreen(
    api: ApiClient,
    session: Session,
    onLoggedIn: () -> Unit,
) {
    var identifier by remember { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    var totp by remember { mutableStateOf("") }
    var needs2fa by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    var rememberMe by remember { mutableStateOf(true) }
    var trustedLogin by remember { mutableStateOf(false) }
    var notFound by remember { mutableStateOf(false) }
    // Identity spec §3.1 item 5: a 30-day trusted-device token allows signing
    // in without typing the password at all.
    val hasTrustedToken = session.deviceTrustToken != null
    val scope = rememberCoroutineScope()
    val context = androidx.compose.ui.platform.LocalContext.current

    // Identity spec §3.2/§4.2: the Apple OAuth round-trip lands in
    // MainActivity via the chatapp://auth/apple deep link; finish the login
    // here whenever a pending token appears.
    androidx.compose.runtime.LaunchedEffect(Unit) {
        while (true) {
            val pending = com.chatapp.MainActivity.AppleAuth.pendingIdToken
            if (!pending.isNullOrEmpty() && !busy) {
                com.chatapp.MainActivity.AppleAuth.pendingIdToken = null
                busy = true
                error = null
                try {
                    val body = org.json.JSONObject().put("id_token", pending).put("totp_code", totp).toString()
                    val resp = withContext(Dispatchers.IO) { api.post("/api/auth/apple", body, null) }
                    applyTokens(session, JSONObject(resp))
                    onLoggedIn()
                } catch (e: java.io.IOException) {
                    if (e.message?.contains("totp_required") == true) {
                        needs2fa = true
                        error = "Enter your authenticator code"
                    } else {
                        error = "Apple sign-in failed"
                    }
                } catch (e: Exception) {
                    error = "Apple sign-in failed"
                } finally {
                    busy = false
                }
            }
            kotlinx.coroutines.delay(500)
        }
    }

    Column(
        modifier = Modifier.fillMaxSize().padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text("ChatApp", style = MaterialTheme.typography.headlineLarge)
        OutlinedTextField(
            value = identifier,
            onValueChange = {
                identifier = it
                notFound = false
            },
            label = { Text("Username / email / phone") },
            modifier = Modifier.fillMaxWidth(),
        )
        if (!trustedLogin) {
            OutlinedTextField(
                value = password,
                onValueChange = { password = it },
                label = { Text("Password") },
                visualTransformation = PasswordVisualTransformation(),
                modifier = Modifier.fillMaxWidth(),
            )
        }
        if (needs2fa) {
            OutlinedTextField(
                value = totp,
                onValueChange = { totp = it },
                label = { Text("2FA code") },
                modifier = Modifier.fillMaxWidth(),
            )
        }
        if (notFound) {
            Text(
                "No account found for \"$identifier\". Create one in Settings → Sign up on chatapp.zo.computer, then come back.",
                color = MaterialTheme.colorScheme.tertiary,
            )
        }
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        // Identity spec §3.2/§4.2: federated Apple sign-in through the system
        // browser; the web callback bounces the id_token back via deep link.
        if (!com.chatapp.BuildConfig.APPLE_CLIENT_ID.isNullOrEmpty()) {
            OutlinedButton(
                enabled = !busy,
                modifier = Modifier.fillMaxWidth(),
                onClick = {
                    val redirect = java.net.URLEncoder.encode(
                        com.chatapp.BuildConfig.WEB_BASE_URL.trimEnd('/') + "/auth/apple/callback", "UTF-8",
                    )
                    val url = "https://appleid.apple.com/auth/authorize" +
                        "?response_type=id_token&response_mode=form_post" +
                        "&client_id=" + com.chatapp.BuildConfig.APPLE_CLIENT_ID +
                        "&scope=name%20email&redirect_uri=" + redirect
                    try {
                        context.startActivity(android.content.Intent(android.content.Intent.ACTION_VIEW, android.net.Uri.parse(url)))
                    } catch (_: Exception) {
                        error = "No browser available for Apple sign-in"
                    }
                },
            ) {
                Text(" Sign in with Apple")
            }
        }
        Button(
            enabled = !busy,
            modifier = Modifier.fillMaxWidth(),
            onClick = {
                busy = true
                error = null
                scope.launch {
                    try {
                        if (trustedLogin) {
                            val body = JSONObject()
                                .put("device_token", session.deviceTrustToken ?: "")
                                .put("totp_code", totp)
                                .toString()
                            val resp = withContext(Dispatchers.IO) { api.post("/api/auth/trusted-device/login", body, null) }
                            applyTokens(session, JSONObject(resp))
                            onLoggedIn()
                            return@launch
                        }
                        // Identity spec §3.1 step 2: probe the identifier first so an
                        // unknown account gets a friendly sign-up pointer, not a dead end.
                        val probe = JSONObject()
                            .put("identifier", identifier.trim())
                            .toString()
                        val check = withContext(Dispatchers.IO) { api.post("/api/auth/identifier/check", probe, null) }
                        if (!JSONObject(check).optBoolean("exists", false)) {
                            notFound = true
                            busy = false
                            return@launch
                        }
                        val body = JSONObject()
                            .put("identifier", identifier)
                            .put("password", password)
                            .put("totp_code", totp)
                            .toString()
                        val resp = withContext(Dispatchers.IO) { api.post("/api/auth/login", body, null) }
                        applyTokens(session, JSONObject(resp))
                        // §3.1 item 5: enroll this device for 30-day passwordless login.
                        if (rememberMe) {
                            try {
                                val enroll = withContext(Dispatchers.IO) {
                                    api.post("/api/auth/trusted-device/enroll", "{}", JSONObject(resp).getString("access_token"))
                                }
                                session.deviceTrustToken = JSONObject(enroll).getString("device_token")
                            } catch (_: Exception) {
                                // Trusted-device login is an enhancement; never block login on it.
                            }
                        }
                        onLoggedIn()
                    } catch (e: java.io.IOException) {
                        if (e.message?.contains("totp_required") == true) {
                            needs2fa = true
                            error = "Enter your authenticator code"
                        } else {
                            error = e.message ?: "Login failed"
                        }
                    } catch (e: Exception) {
                        error = "Network error: ${e.message}"
                    } finally {
                        busy = false
                    }
                }
            },
        ) {
            Text(if (busy) "…" else if (trustedLogin) "Log in without password" else "Log in")
        }
        RowCheckbox(rememberMe) { rememberMe = it }
        if (hasTrustedToken) {
            OutlinedButton(
                enabled = !busy,
                modifier = Modifier.fillMaxWidth(),
                onClick = {
                    trustedLogin = !trustedLogin
                    error = null
                },
            ) {
                Text(if (trustedLogin) "Use password instead" else "This device is trusted — sign in without password")
            }
        }
    }
}

@Composable
private fun RowCheckbox(checked: Boolean, onChange: (Boolean) -> Unit) {
    androidx.compose.foundation.layout.Row(
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Checkbox(checked = checked, onCheckedChange = onChange)
        Text("Remember me and trust this device for 30 days")
    }
}

private fun applyTokens(session: Session, json: JSONObject) {
    session.accessToken = json.getString("access_token")
    session.refreshToken = json.getString("refresh_token")
    session.userId = json.getString("user_id")
}
