package com.chatapp.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.chatapp.data.ApiClient
import com.chatapp.data.Session
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.json.JSONObject

data class TierItem(val id: String, val name: String, val priceUsd: Double, val subscribers: Int)
data class SubItem(val id: String, val tierName: String, val priceUsd: Double, val creator: String, val status: String)

@Composable
fun MonetizeScreen(api: ApiClient, session: Session) {
    var tiers by remember { mutableStateOf(listOf<TierItem>()) }
    var subs by remember { mutableStateOf(listOf<SubItem>()) }
    var earningsTotal by remember { mutableStateOf(0.0) }
    var earningsAvailable by remember { mutableStateOf(0.0) }
    var title by remember { mutableStateOf("") }
    var price by remember { mutableStateOf("") }
    var benefits by remember { mutableStateOf("") }
    var tipTo by remember { mutableStateOf("") }
    var tipAmount by remember { mutableStateOf("") }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val token = session.accessToken ?: ""

    fun load() {
        scope.launch {
            try {
                val t = withContext(Dispatchers.IO) { api.get("/api/creator/tiers", token) }
                val tArr = JSONObject(t).getJSONArray("tiers")
                tiers = (0 until tArr.length()).map { i ->
                    val o = tArr.getJSONObject(i)
                    TierItem(o.getString("id"), o.getString("name"), o.getDouble("price_usd"), o.optInt("subscriber_count"))
                }
                val s = withContext(Dispatchers.IO) { api.get("/api/subscriptions", token) }
                val sArr = JSONObject(s).getJSONArray("subscriptions")
                subs = (0 until sArr.length()).map { i ->
                    val o = sArr.getJSONObject(i)
                    SubItem(
                        o.getString("id"), o.getString("tier_name"),
                        o.getDouble("price_usd"), o.getString("creator_username"), o.getString("status"),
                    )
                }
                val e = withContext(Dispatchers.IO) { api.get("/api/creator/earnings", token) }
                val eObj = JSONObject(e)
                earningsTotal = eObj.optDouble("earned", 0.0)
                earningsAvailable = eObj.optDouble("available", 0.0)
            } catch (e: Exception) {
                error = e.message
            }
        }
    }

    LaunchedEffect(Unit) { load() }

    Column(modifier = Modifier.fillMaxSize().padding(16.dp)) {
        Text("Monetization", style = MaterialTheme.typography.headlineSmall)
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }

        LazyColumn(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            item {
                Card(modifier = Modifier.fillMaxWidth()) {
                    Column(modifier = Modifier.padding(12.dp)) {
                        Text("New tier", style = MaterialTheme.typography.titleMedium)
                        OutlinedTextField(value = title, onValueChange = { title = it }, label = { Text("Title") })
                        OutlinedTextField(value = price, onValueChange = { price = it }, label = { Text("Price USD") })
                        OutlinedTextField(value = benefits, onValueChange = { benefits = it }, label = { Text("Benefits") })
                        Button(onClick = {
                            scope.launch {
                                try {
                                    val usd = price.toDouble()
                                    val body = JSONObject().put("name", title).put("price_usd", usd)
                                        .put("perks", benefits).toString()
                                    withContext(Dispatchers.IO) { api.post("/api/creator/tiers", body, token) }
                                    title = ""; price = ""; benefits = ""
                                    load()
                                } catch (e: Exception) {
                                    error = e.message
                                }
                            }
                        }, enabled = title.isNotBlank()) { Text("Create tier") }
                    }
                }
            }

            item {
                Text("My tiers", style = MaterialTheme.typography.titleMedium)
            }
            items(tiers) { t ->
                Card(modifier = Modifier.fillMaxWidth()) {
                    Column(modifier = Modifier.padding(12.dp)) {
                        Text("${t.name} - ${"$%.2f".format(t.priceUsd)} USD/mo (${t.subscribers} subs)")
                    }
                }
            }

            item {
                Text("My memberships", style = MaterialTheme.typography.titleMedium)
                Card(modifier = Modifier.fillMaxWidth()) {
                    Column(modifier = Modifier.padding(12.dp)) {
                        Text("Tip a creator", style = MaterialTheme.typography.titleMedium)
                        OutlinedTextField(value = tipTo, onValueChange = { tipTo = it }, label = { Text("To username") })
                        OutlinedTextField(value = tipAmount, onValueChange = { tipAmount = it }, label = { Text("Amount USD") })
                        Button(onClick = {
                            scope.launch {
                                try {
                                    val found = withContext(Dispatchers.IO) {
                                        api.get("/api/users/search?q=$tipTo", token)
                                    }
                                    val users = JSONObject(found).getJSONArray("users")
                                    if (users.length() == 0) throw IllegalStateException("user not found")
                                    val uid = users.getJSONObject(0).getString("id")
                                    val payload = JSONObject().put("amount_usd", tipAmount.toDouble())
                                        .put("message", "").toString()
                                    withContext(Dispatchers.IO) { api.post("/api/users/$uid/tip", payload, token) }
                                    tipTo = ""; tipAmount = ""
                                    load()
                                } catch (e: Exception) {
                                    error = e.message
                                }
                            }
                        }) { Text("Send tip") }
                    }
                }
            }
            items(subs) { s ->
                Card(modifier = Modifier.fillMaxWidth()) {
                    Column(modifier = Modifier.padding(12.dp)) {
                        Row {
                            Text("${s.tierName} (${"$%.2f".format(s.priceUsd)} USD)")
                            Text("@${s.creator} · ${s.status}")
                        }
                        TextButton(onClick = {
                            scope.launch {
                                try {
                                    withContext(Dispatchers.IO) {
                                        api.delete("/api/subscriptions/${s.id}", "{}", token)
                                    }
                                    load()
                                } catch (e: Exception) {
                                    error = e.message
                                }
                            }
                        }) { Text("Unsubscribe") }
                    }
                }
            }

            item {
                Text("Earnings: ${"$%.2f".format(earningsTotal)} USD (available ${"$%.2f".format(earningsAvailable)})",
                    style = MaterialTheme.typography.titleMedium)
            }
        }
    }
}
