package com.chatapp.ui

import android.graphics.Bitmap
import android.graphics.Color
import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.unit.dp
import androidx.camera.core.CameraSelector
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.Preview
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.LocalLifecycleOwner
import com.chatapp.data.ApiClient
import com.chatapp.data.Session
import com.google.mlkit.vision.barcode.BarcodeScanning
import com.google.mlkit.vision.common.InputImage
import com.google.zxing.BarcodeFormat
import com.google.zxing.qrcode.QRCodeWriter
import java.util.concurrent.Executors
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.json.JSONObject

// Wallet: balances, deterministic multi-chain deposit addresses (QR + copy),
// signed withdrawals (paste or camera-scan the destination QR), convert and
// the escrowed P2P market — same feature set as the web/desktop wallet.

data class WalletAcct(val id: String, val asset: String, val chain: String, val balance: String)
data class P2POfferItem(val id: String, val owner: String, val side: String, val asset: String, val chain: String,
    val price: String, val fiat: String, val min: String, val max: String, val methods: String)
data class P2PTradeItem(val id: String, val asset: String, val amount: String, val fiat: String,
    val method: String, val status: String, val buyer: String, val seller: String)

private fun qrBitmap(content: String, size: Int = 512): Bitmap {
    val matrix = QRCodeWriter().encode(content, BarcodeFormat.QR_CODE, size, size)
    val bmp = Bitmap.createBitmap(size, size, Bitmap.Config.RGB_565)
    for (x in 0 until size) for (y in 0 until size) {
        bmp.setPixel(x, y, if (matrix.get(x, y)) Color.BLACK else Color.WHITE)
    }
    return bmp
}

private fun parseAddressPayload(raw: String): String {
    val m = Regex("^(?:bitcoin|ethereum|tron|solana|litecoin|dogecoin):([a-zA-Z0-9]+)").find(raw.trim())
    return m?.groupValues?.get(1) ?: raw.trim()
}

@Composable
fun WalletScreen(api: ApiClient, session: Session) {
    var tab by remember { mutableStateOf("wallet") }
    var accounts by remember { mutableStateOf(listOf<WalletAcct>()) }
    var depositAddress by remember { mutableStateOf("") }
    var depositUri by remember { mutableStateOf("") }
    var depAsset by remember { mutableStateOf("USDT") }
    var depChain by remember { mutableStateOf("tron") }
    var wdTo by remember { mutableStateOf("") }
    var wdAmount by remember { mutableStateOf("") }
    var convAmount by remember { mutableStateOf("") }
    var convResult by remember { mutableStateOf("") }
    var offers by remember { mutableStateOf(listOf<P2POfferItem>()) }
    var trades by remember { mutableStateOf(listOf<P2PTradeItem>()) }
    var scanning by remember { mutableStateOf(false) }
    var message by remember { mutableStateOf<String?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    val clipboard = LocalClipboardManager.current
    val scope = rememberCoroutineScope()
    val token = session.accessToken ?: ""

    fun load() {
        scope.launch {
            try {
                val a = withContext(Dispatchers.IO) { api.get("/api/wallet/accounts", token) }
                val arr = JSONObject(a).getJSONArray("accounts")
                accounts = (0 until arr.length()).map { i ->
                    val o = arr.getJSONObject(i)
                    WalletAcct(o.getString("id"), o.getString("asset"), o.getString("chain"), o.getString("balance"))
                }
                val tr = withContext(Dispatchers.IO) { api.get("/api/p2p/trades", token) }
                val tArr = JSONObject(tr).getJSONArray("trades")
                trades = (0 until tArr.length()).map { i ->
                    val o = tArr.getJSONObject(i)
                    P2PTradeItem(
                        o.getString("id"), o.getString("asset"), o.getString("crypto_amount"),
                        o.getString("fiat_amount") + " " + o.getString("fiat_currency"),
                        o.getString("payment_method"), o.getString("status"),
                        o.getString("buyer_username"), o.getString("seller_username"),
                    )
                }
            } catch (_: Exception) { /* lists stay empty until backend reachable */ }
        }
    }

    LaunchedEffect(Unit) { load() }

    fun loadOffers() {
        scope.launch {
            try {
                val o = withContext(Dispatchers.IO) { api.get("/api/p2p/offers", token) }
                val arr = JSONObject(o).getJSONArray("offers")
                offers = (0 until arr.length()).map { i ->
                    val ob = arr.getJSONObject(i)
                    val methods = ob.getJSONArray("payment_methods")
                    P2POfferItem(
                        ob.getString("id"), ob.getString("owner_username"), ob.getString("side"),
                        ob.getString("asset"), ob.getString("chain"), ob.getString("price"),
                        ob.getString("fiat_currency"), ob.getString("min_amount"), ob.getString("max_amount"),
                        (0 until methods.length()).joinToString(", ") { methods.getString(it) },
                    )
                }
            } catch (e: Exception) { error = e.message }
        }
    }

    Column(modifier = Modifier.fillMaxSize().padding(12.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            listOf("wallet", "deposit", "withdraw", "convert", "p2p").forEach { tb ->
                TextButton(onClick = {
                    tab = tb
                    if (tb == "p2p") loadOffers()
                }) { Text(tb.replaceFirstChar { it.uppercase() }) }
            }
        }
        message?.let { Text(it, color = MaterialTheme.colorScheme.primary) }
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }

        when (tab) {
            "wallet" -> LazyColumn {
                items(accounts) { a ->
                    Card(modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp)) {
                        Row(modifier = Modifier.padding(12.dp)) {
                            Text("${a.asset} · ${a.chain}", modifier = Modifier.weight(1f))
                            Text(a.balance)
                        }
                    }
                }
            }

            "deposit" -> Column {
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedTextField(value = depAsset, onValueChange = { depAsset = it.uppercase() },
                        label = { Text("Asset") }, modifier = Modifier.weight(1f))
                    OutlinedTextField(value = depChain, onValueChange = { depChain = it.lowercase() },
                        label = { Text("Chain") }, modifier = Modifier.weight(1f))
                }
                Button(onClick = {
                    scope.launch {
                        error = null
                        try {
                            val body = JSONObject().put("asset", depAsset).put("chain", depChain).toString()
                            val r = withContext(Dispatchers.IO) { api.post("/api/wallet/deposit-address", body, token) }
                            val o = JSONObject(r)
                            depositAddress = o.getString("address")
                            depositUri = o.optString("uri").ifEmpty { depositAddress }
                        } catch (e: Exception) { error = e.message }
                    }
                }, modifier = Modifier.padding(vertical = 8.dp)) { Text("Get deposit address") }
                if (depositAddress.isNotEmpty()) {
                    Image(bitmap = qrBitmap(depositUri).asImageBitmap(), contentDescription = "Deposit QR",
                        modifier = Modifier.size(180.dp))
                    Text(depositAddress, style = MaterialTheme.typography.bodySmall)
                    TextButton(onClick = {
                        clipboard.setText(AnnotatedString(depositAddress))
                        message = "Copied"
                    }) { Text("Copy address") }
                }
            }

            "withdraw" -> Column {
                OutlinedTextField(value = wdTo, onValueChange = { wdTo = it },
                    label = { Text("Destination address") }, modifier = Modifier.fillMaxWidth())
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    TextButton(onClick = {
                        clipboard.getText()?.let { wdTo = parseAddressPayload(it.text) }
                    }) { Text("Paste") }
                    TextButton(onClick = { scanning = !scanning }) { Text(if (scanning) "Stop scan" else "Scan QR") }
                }
                OutlinedTextField(value = wdAmount, onValueChange = { wdAmount = it },
                    label = { Text("Amount") }, modifier = Modifier.fillMaxWidth())
                Button(onClick = {
                    scope.launch {
                        error = null; message = null
                        try {
                            val body = JSONObject()
                                .put("asset", depAsset).put("chain", depChain)
                                .put("to_address", wdTo).put("amount", wdAmount).toString()
                            val r = withContext(Dispatchers.IO) { api.post("/api/wallet/withdraw", body, token) }
                            val o = JSONObject(r)
                            message = "Withdrawal ${o.getString("status")}" +
                                if (o.optBoolean("auto_approved")) " — auto-approved in ${o.optLong("approved_in_ms")}ms" else ""
                            wdTo = ""; wdAmount = ""
                            load()
                        } catch (e: Exception) { error = e.message }
                    }
                }, modifier = Modifier.padding(vertical = 8.dp)) { Text("Withdraw") }
                if (scanning) {
                    QRScanner(onResult = { wdTo = parseAddressPayload(it); scanning = false })
                }
            }

            "convert" -> Column {
                OutlinedTextField(value = convAmount, onValueChange = { convAmount = it },
                    label = { Text("Amount (source asset set in Wallet tab)") }, modifier = Modifier.fillMaxWidth())
                Button(onClick = {
                    scope.launch {
                        error = null
                        try {
                            val body = JSONObject()
                                .put("from_asset", depAsset).put("from_chain", depChain)
                                .put("to_asset", "USD").put("to_chain", "internal")
                                .put("amount", convAmount).toString()
                            val r = withContext(Dispatchers.IO) { api.post("/api/convert", body, token) }
                            convResult = "Received " + JSONObject(r).getString("to_amount") + " USD"
                            load()
                        } catch (e: Exception) { error = e.message }
                    }
                }, modifier = Modifier.padding(vertical = 8.dp)) { Text("Convert to USD") }
                if (convResult.isNotEmpty()) Text(convResult)
            }

            "p2p" -> LazyColumn {
                items(offers) { o ->
                    Card(modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp)) {
                        Column(modifier = Modifier.padding(12.dp)) {
                            Text("${o.side.uppercase()} ${o.asset} @ ${o.price} ${o.fiat} — @${o.owner}")
                            Text("Limits ${o.min}–${o.max} · ${o.methods}", style = MaterialTheme.typography.bodySmall)
                        }
                    }
                }
                item {
                    Text("My trades", style = MaterialTheme.typography.titleMedium, modifier = Modifier.padding(vertical = 8.dp))
                }
                items(trades) { tr ->
                    Card(modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp)) {
                        Column(modifier = Modifier.padding(12.dp)) {
                            Text("${tr.amount} ${tr.asset} ⇄ ${tr.fiat} [${tr.status}]")
                            Text("${tr.method} · buyer @${tr.buyer} seller @${tr.seller}",
                                style = MaterialTheme.typography.bodySmall)
                            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                if (tr.status == "open") {
                                    TextButton(onClick = { tradeAction(api, token, tr.id, "paid", scope, ::load) { error = it } }) { Text("Paid") }
                                    TextButton(onClick = { tradeAction(api, token, tr.id, "cancel", scope, ::load) { error = it } }) { Text("Cancel") }
                                }
                                if (tr.status == "paid") {
                                    TextButton(onClick = { tradeAction(api, token, tr.id, "release", scope, ::load) { error = it } }) { Text("Release") }
                                    TextButton(onClick = { tradeAction(api, token, tr.id, "dispute", scope, ::load) { error = it } }) { Text("Dispute") }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

private fun tradeAction(
    api: ApiClient, token: String, id: String, action: String,
    scope: kotlinx.coroutines.CoroutineScope, reload: () -> Unit, onError: (String?) -> Unit,
) {
    scope.launch {
        try {
            withContext(Dispatchers.IO) { api.post("/api/p2p/trades/$id/$action", "{}", token) }
            reload()
        } catch (e: Exception) { onError(e.message) }
    }
}

// CameraX + ML Kit QR scanner for withdrawal addresses.
@Composable
fun QRScanner(onResult: (String) -> Unit) {
    val context = LocalContext.current
    val lifecycleOwner = LocalLifecycleOwner.current
    val scanner = remember { BarcodeScanning.getClient() }
    val executor = remember { Executors.newSingleThreadExecutor() }

    DisposableEffect(Unit) {
        onDispose {
            scanner.close()
            executor.shutdown()
        }
    }

    AndroidView(
        modifier = Modifier.fillMaxWidth().size(260.dp),
        factory = { ctx ->
            val view = PreviewView(ctx)
            val future = ProcessCameraProvider.getInstance(ctx)
            future.addListener({
                val provider = future.get()
                val preview = Preview.Builder().build().also { it.surfaceProvider = view.surfaceProvider }
                val analysis = ImageAnalysis.Builder()
                    .setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST)
                    .build()
                analysis.setAnalyzer(executor) { proxy ->
                    @androidx.camera.core.ExperimentalGetImage
                    val media = proxy.image
                    if (media != null) {
                        val image = InputImage.fromMediaImage(media, proxy.imageInfo.rotationDegrees)
                        scanner.process(image)
                            .addOnSuccessListener { codes ->
                                codes.firstOrNull()?.rawValue?.let(onResult)
                            }
                            .addOnCompleteListener { proxy.close() }
                    } else {
                        proxy.close()
                    }
                }
                provider.unbindAll()
                provider.bindToLifecycle(lifecycleOwner, CameraSelector.DEFAULT_BACK_CAMERA, preview, analysis)
            }, ContextCompat.getMainExecutor(ctx))
            view
        },
    )
}
