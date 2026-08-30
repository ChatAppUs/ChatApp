package com.chatapp.ui

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier

private data class MenuItem(val route: String, val label: String)

private val ITEMS = listOf(
    MenuItem("feed", "Feed"),
    MenuItem("fyp", "For You"),
    MenuItem("groups", "Groups"),
    MenuItem("pages", "Pages"),
    MenuItem("chat", "Chat"),
    MenuItem("monetize", "Money"),
    MenuItem("wallet", "Wallet"),
    MenuItem("bots", "Bots"),
    MenuItem("privacy", "Privacy"),
)

// Shared top menu across authenticated screens, matching the web Nav.
// Includes the light/dark theme toggle present on every client.
@Composable
fun MenuBar(onNavigate: (String) -> Unit, onLogout: () -> Unit, onToggleTheme: () -> Unit) {
    Row(modifier = Modifier.fillMaxWidth().horizontalScroll(rememberScrollState())) {
        ITEMS.forEach { item ->
            TextButton(onClick = { onNavigate(item.route) }) {
                Text(item.label)
            }
        }
        TextButton(onClick = onToggleTheme) {
            Text("◐ Theme")
        }
        TextButton(onClick = onLogout) {
            Text("Logout")
        }
    }
}
