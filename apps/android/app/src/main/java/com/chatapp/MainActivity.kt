package com.chatapp

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import com.chatapp.ui.MenuBar
import androidx.compose.ui.platform.LocalContext
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import com.chatapp.data.ApiClient
import com.chatapp.data.Session
import com.chatapp.ui.BotsScreen
import com.chatapp.ui.ChatScreen
import com.chatapp.ui.FeedScreen
import com.chatapp.ui.FypScreen
import com.chatapp.ui.GroupsScreen
import com.chatapp.ui.LoginScreen
import com.chatapp.ui.MonetizeScreen
import com.chatapp.ui.PagesScreen
import com.chatapp.ui.PrivacyScreen

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
            Column(modifier = Modifier.fillMaxSize()) {
                MenuBar(
                    onNavigate = { route ->
                        nav.navigate(route) { launchSingleTop = true; popUpTo("login") { inclusive = true } }
                    },
                    onLogout = {
                        session.clear()
                        nav.navigate("login") { popUpTo(0) { inclusive = true } }
                    },
                )
                FeedScreen(api, session, onOpenChat = { nav.navigate("chat") })
            }
        }
        composable("fyp") {
            Column(modifier = Modifier.fillMaxSize()) {
                MenuBar(onNavigate = { route -> nav.navigate(route) { launchSingleTop = true; popUpTo("login") { inclusive = true } } },
                    onLogout = { session.clear(); nav.navigate("login") { popUpTo(0) { inclusive = true } } })
                FypScreen(api, session)
            }
        }
        composable("groups") {
            Column(modifier = Modifier.fillMaxSize()) {
                MenuBar(onNavigate = { route -> nav.navigate(route) { launchSingleTop = true; popUpTo("login") { inclusive = true } } },
                    onLogout = { session.clear(); nav.navigate("login") { popUpTo(0) { inclusive = true } } })
                GroupsScreen(api, session)
            }
        }
        composable("pages") {
            Column(modifier = Modifier.fillMaxSize()) {
                MenuBar(onNavigate = { route -> nav.navigate(route) { launchSingleTop = true; popUpTo("login") { inclusive = true } } },
                    onLogout = { session.clear(); nav.navigate("login") { popUpTo(0) { inclusive = true } } })
                PagesScreen(api, session)
            }
        }
        composable("monetize") {
            Column(modifier = Modifier.fillMaxSize()) {
                MenuBar(onNavigate = { route -> nav.navigate(route) { launchSingleTop = true; popUpTo("login") { inclusive = true } } },
                    onLogout = { session.clear(); nav.navigate("login") { popUpTo(0) { inclusive = true } } })
                MonetizeScreen(api, session)
            }
        }
        composable("bots") {
            Column(modifier = Modifier.fillMaxSize()) {
                MenuBar(onNavigate = { route -> nav.navigate(route) { launchSingleTop = true; popUpTo("login") { inclusive = true } } },
                    onLogout = { session.clear(); nav.navigate("login") { popUpTo(0) { inclusive = true } } })
                BotsScreen(api, session)
            }
        }
        composable("privacy") {
            Column(modifier = Modifier.fillMaxSize()) {
                MenuBar(onNavigate = { route -> nav.navigate(route) { launchSingleTop = true; popUpTo("login") { inclusive = true } } },
                    onLogout = { session.clear(); nav.navigate("login") { popUpTo(0) { inclusive = true } } })
                PrivacyScreen(api, session)
            }
        }
        composable("chat") {
            Column(modifier = Modifier.fillMaxSize()) {
                MenuBar(onNavigate = { route -> nav.navigate(route) { launchSingleTop = true; popUpTo("login") { inclusive = true } } },
                    onLogout = { session.clear(); nav.navigate("login") { popUpTo(0) { inclusive = true } } })
                ChatScreen(api, session, wsBaseUrl = BuildConfig.WS_BASE_URL)
            }
        }
    }
}
