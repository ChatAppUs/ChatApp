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

data class ShopRoom(
    val id: String,
    val title: String,
    val status: String,
    val viewerCount: Int,
)

data class ShopProduct(
    val id: String,
    val name: String,
    val priceCents: Int,
    val stock: Int,
)

private fun money(cents: Int): String = "$" + (cents / 100) + "." + ("%02d".format(cents % 100))

// Android Live Shopping surface (master plan §20): pick a live room, list and
// create pinned products, apply a coupon and check out. Same endpoints as the
// web /live-shop page; checkout is settled server-side on the double-entry
// ledger with row locking, so oversell is impossible here too.
@Composable
fun LiveShopScreen(api: ApiClient, session: Session) {
    var rooms by remember { mutableStateOf(listOf<ShopRoom>()) }
    var selected by remember { mutableStateOf<ShopRoom?>(null) }
    var products by remember { mutableStateOf(listOf<ShopProduct>()) }
    var pname by remember { mutableStateOf("") }
    var pprice by remember { mutableStateOf("") }
    var pstock by remember { mutableStateOf("") }
    var coupon by remember { mutableStateOf("") }
    var buyers by remember { mutableStateOf(mapOf<String, Int>()) }
    var status by remember { mutableStateOf<String?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val token = session.accessToken ?: ""

    fun loadRooms() {
        scope.launch {
            try {
                val resp = withContext(Dispatchers.IO) { api.get("/api/live-rooms?limit=50", token) }
                val arr = JSONObject(resp).getJSONArray("rooms")
                val parsed = mutableListOf<ShopRoom>()
                for (i in 0 until arr.length()) {
                    val r = arr.getJSONObject(i)
                    parsed.add(
                        ShopRoom(
                            id = r.getString("id"),
                            title = r.optString("title"),
                            status = r.optString("status", "live"),
                            viewerCount = r.optInt("viewer_count"),
                        )
                    )
                }
                rooms = parsed
                if (selected == null && parsed.isNotEmpty()) {
                    selected = parsed.first()
                    loadProducts(parsed.first().id)
                }
            } catch (e: Exception) {
                error = e.message
            }
        }
    }

    fun loadProducts(roomId: String) {
        scope.launch {
            try {
                val resp = withContext(Dispatchers.IO) { api.get("/api/live-rooms/$roomId/products", token) }
                val arr = JSONObject(resp).getJSONArray("products")
                val parsed = mutableListOf<ShopProduct>()
                for (i in 0 until arr.length()) {
                    val p = arr.getJSONObject(i)
                    parsed.add(
                        ShopProduct(
                            id = p.getString("id"),
                            name = p.optString("name"),
                            priceCents = p.optInt("price_cents"),
                            stock = p.optInt("stock"),
                        )
                    )
                }
                products = parsed
            } catch (e: Exception) {
                error = e.message
            }
        }
    }

    LaunchedEffect(Unit) { loadRooms() }

    Column(modifier = Modifier.fillMaxSize().padding(16.dp)) {
        Text("Live Shop", style = MaterialTheme.typography.headlineSmall)
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        status?.let { Text(it, color = MaterialTheme.colorScheme.primary) }

        LazyColumn(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            item {
                Text("Rooms", style = MaterialTheme.typography.titleMedium)
                rooms.forEach { r ->
                    Row {
                        TextButton(onClick = {
                            selected = r
                            status = null
                            loadProducts(r.id)
                        }) { Text("${r.title} (${r.status})") }
                    }
                }
            }

            item {
                Card(modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
                    Column(modifier = Modifier.padding(12.dp)) {
                        Text("Add product", style = MaterialTheme.typography.titleMedium)
                        OutlinedTextField(value = pname, onValueChange = { pname = it }, label = { Text("Name") })
                        OutlinedTextField(value = pprice, onValueChange = { pprice = it }, label = { Text("Price (cents)") })
                        OutlinedTextField(value = pstock, onValueChange = { pstock = it }, label = { Text("Stock") })
                        Button(
                            onClick = {
                                val room = selected ?: return@Button
                                scope.launch {
                                    try {
                                        val body = JSONObject()
                                            .put("name", pname)
                                            .put("price_cents", pprice.toIntOrNull() ?: 0)
                                            .put("stock", pstock.toIntOrNull() ?: 0)
                                            .toString()
                                        withContext(Dispatchers.IO) {
                                            api.post("/api/live-rooms/${room.id}/products", body, token)
                                        }
                                        pname = ""; pprice = ""; pstock = ""
                                        loadProducts(room.id)
                                    } catch (e: Exception) {
                                        error = e.message
                                    }
                                }
                            },
                            enabled = selected != null && pname.isNotBlank(),
                        ) { Text("Create") }
                    }
                }
            }

            item {
                Row {
                    OutlinedTextField(value = coupon, onValueChange = { coupon = it }, label = { Text("Coupon code") })
                }
            }

            items(products) { p ->
                Card(modifier = Modifier.fillMaxWidth()) {
                    Column(modifier = Modifier.padding(12.dp)) {
                        Text(p.name, style = MaterialTheme.typography.titleMedium)
                        Text("${money(p.priceCents)} · ${p.stock} in stock", style = MaterialTheme.typography.bodySmall)
                        Row {
                            TextButton(onClick = {
                                val room = selected ?: return@TextButton
                                scope.launch {
                                    try {
                                        val body = JSONObject().put("product_id", p.id).toString()
                                        withContext(Dispatchers.IO) {
                                            api.post("/api/live-rooms/${room.id}/pin", body, token)
                                        }
                                        status = "Pinned ${p.name}"
                                    } catch (e: Exception) {
                                        error = e.message
                                    }
                                }
                            }) { Text("Pin") }
                            TextButton(onClick = {
                                val room = selected ?: return@TextButton
                                scope.launch {
                                    try {
                                        val body = JSONObject().put("product_id", p.id).put("quantity", 1)
                                        if (coupon.isNotBlank()) body.put("coupon_code", coupon)
                                        val resp = withContext(Dispatchers.IO) {
                                            api.post("/api/live-rooms/${room.id}/checkout", body.toString(), token)
                                        }
                                        val j = JSONObject(resp)
                                        status = "Order " + j.optString("status", "placed") +
                                            (if (j.has("total_cents")) " · ${money(j.optInt("total_cents"))}" else "")
                                        buyers = buyers + (p.id to ((buyers[p.id] ?: 0) + 1))
                                        loadProducts(room.id)
                                    } catch (e: Exception) {
                                        error = e.message
                                    }
                                }
                            }) { Text("Buy") }
                        }
                        buyers[p.id]?.let { Text("You bought $it", style = MaterialTheme.typography.bodySmall) }
                    }
                }
            }
        }
    }
}
