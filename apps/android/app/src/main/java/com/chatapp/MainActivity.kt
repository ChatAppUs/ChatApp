package com.chatapp

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import com.chatapp.data.ApiClient
import com.chatapp.data.Session
import com.chatapp.ui.ChatScreen
import com.chatapp.ui.FeedScreen
import com.chatapp.ui.LoginScreen

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            MaterialTheme(colorScheme = darkColorScheme()) {
                ChatAppNav()
            }
        }
    }
}

@Composable
fun ChatAppNav() {
    val context = LocalContext.current
    val session = remember { Session(context) }
    val api = remember { ApiClient(BuildConfig.API_BASE_URL) }
    val nav = rememberNavController()
    val start = if (session.accessToken != null) "feed" else "login"

    NavHost(navController = nav, startDestination = start) {
        composable("login") {
            LoginScreen(api, session, onLoggedIn = {
                nav.navigate("feed") { popUpTo("login") { inclusive = true } }
            })
        }
        composable("feed") {
            FeedScreen(api, session, onOpenChat = { nav.navigate("chat") })
        }
        composable("chat") {
            ChatScreen(api, session, wsBaseUrl = BuildConfig.WS_BASE_URL)
        }
    }
}
