package com.chatapp.ui

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

// Brand palette shared with the web app (apps/web/src/app/globals.css).
private val DarkColors = darkColorScheme(
    primary = Color(0xFF4F7CFF),
    secondary = Color(0xFF22C58B),
    background = Color(0xFF0B0F17),
    surface = Color(0xFF141A26),
    surfaceVariant = Color(0xFF1B2333),
    onPrimary = Color.White,
    onBackground = Color(0xFFE8ECF3),
    onSurface = Color(0xFFE8ECF3),
    error = Color(0xFFEF4D5E),
)

private val LightColors = lightColorScheme(
    primary = Color(0xFF3560D8),
    secondary = Color(0xFF0F9D6E),
    background = Color(0xFFF4F6FB),
    surface = Color(0xFFFFFFFF),
    surfaceVariant = Color(0xFFECEFF6),
    onPrimary = Color.White,
    onBackground = Color(0xFF1A2233),
    onSurface = Color(0xFF1A2233),
    error = Color(0xFFD3354A),
)

// Single theme entry point. `dark` is persisted in Session and toggled from
// the MenuBar, so every screen switches at once — same behavior as the web,
// desktop and extension clients.
@Composable
fun ChatAppTheme(dark: Boolean, content: @Composable () -> Unit) {
    MaterialTheme(colorScheme = if (dark) DarkColors else LightColors, content = content)
}
