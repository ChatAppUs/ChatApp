package com.chatapp

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.navigation.NavHostController
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import com.chatapp.data.ApiClient
import com.chatapp.data.Session
import com.chatapp.ui.BotsScreen
import com.chatapp.ui.ChatAppTheme
import com.chatapp.ui.ChatScreen
import com.chatapp.ui.FeedScreen
import com.chatapp.ui.FypScreen
import com.chatapp.ui.GroupsScreen
import com.chatapp.ui.LoginScreen
import com.chatapp.ui.MenuBar
import com.chatapp.ui.MonetizeScreen
import com.chatapp.ui.PagesScreen
import com.chatapp.ui.PrivacyScreen

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            val session = remember { Session(applicationContext) }
            var dark by remember { mutableStateOf(session.darkTheme) }
            ChatAppTheme(dark = dark) {
                ChatAppNav(
                    session = session,
                    onToggleTheme = {
                        dark = !dark
                        session.darkTheme = dark
                    },
                )
            }
        }
    }
}

@Composable
fun ChatAppNav(session: Session, onToggleTheme: () -> Unit) {
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
            AuthedScreen(nav, session, onToggleTheme) { FeedScreen(api, session, onOpenChat = { nav.navigate("chat") }) }
        }
        composable("fyp") {
            AuthedScreen(nav, session, onToggleTheme) { FypScreen(api, session) }
        }
        composable("groups") {
            AuthedScreen(nav, session, onToggleTheme) { GroupsScreen(api, session) }
        }
        composable("pages") {
            AuthedScreen(nav, session, onToggleTheme) { PagesScreen(api, session) }
        }
        composable("monetize") {
            AuthedScreen(nav, session, onToggleTheme) { MonetizeScreen(api, session) }
        }
        composable("bots") {
            AuthedScreen(nav, session, onToggleTheme) { BotsScreen(api, session) }
        }
        composable("privacy") {
            AuthedScreen(nav, session, onToggleTheme) { PrivacyScreen(api, session) }
        }
        composable("chat") {
            AuthedScreen(nav, session, onToggleTheme) { ChatScreen(api, session, wsBaseUrl = BuildConfig.WS_BASE_URL) }
        }
    }
}

// Shared scaffold for every authenticated screen: MenuBar + content. Keeps
// navigation/logout wiring in one place so all screens behave identically.
@Composable
private fun AuthedScreen(
    nav: NavHostController,
    session: Session,
    onToggleTheme: () -> Unit,
    content: @Composable () -> Unit,
) {
    Column(modifier = Modifier.fillMaxSize().background(MaterialTheme.colorScheme.background)) {
        MenuBar(
            onNavigate = { route ->
                nav.navigate(route) { launchSingleTop = true; popUpTo("login") { inclusive = true } }
            },
            onLogout = {
                session.clear()
                nav.navigate("login") { popUpTo(0) { inclusive = true } }
            },
            onToggleTheme = onToggleTheme,
        )
        content()
    }
}
