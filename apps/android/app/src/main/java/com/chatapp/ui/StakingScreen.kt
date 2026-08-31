package com.chatapp.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TextField
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.chatapp.data.ApiClient
import com.chatapp.data.Session
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONObject

data class StakingAsset(val asset: String, val chain: String, val apy: String,
                        val durations: List<Int>, val minAmount: String, val maxAmount: String,
                        val priceUsd: String?)
data class StakingPosition(val id: String, val asset: String, val chain: String,
                           val amount: String, val apy: String, val durationDays: Int,
                           val endsAt: String, val status: String,
                           val reward: String?, val accrued: String?)

@Composable
fun StakingScreen(api: ApiClient, session: Session) {
    var assets by remember { mutableStateOf<List<StakingAsset>>(emptyList()) }
    var positions by remember { mutableStateOf<List<StakingPosition>>(emptyList()) }
    var msg by remember { mutableStateOf("") }
    var err by remember { mutableStateOf("") }
    var pickIdx by remember { mutableStateOf(0) }
    var amount by remember { mutableStateOf("") }

    fun load() {
        try {
            val aResp = api.get("/api/staking/assets", session.accessToken)
            val aArr = JSONObject(aResp).optJSONArray("assets") ?: org.json.JSONArray()
            assets = (0 until aArr.length()).map { i ->
                val o = aArr.getJSONObject(i)
                StakingAsset(
                    asset = o.optString("asset"),
                    chain = o.optString("chain"),
                    apy = o.optString("apy"),
                    durations = (o.optJSONArray("durations") ?: org.json.JSONArray())
                        .let { a -> (0 until a.length()).map { a.getInt(it) } },
                    minAmount = o.optString("min_amount"),
                    maxAmount = o.optString("max_amount"),
                    priceUsd = if (o.isNull("price_usd")) null else o.optString("price_usd"),
                )
            }
        } catch (e: Exception) { err = e.message ?: "load error" }
        try {
            val pResp = api.get("/api/staking/positions", session.accessToken)
            val pArr = JSONObject(pResp).optJSONArray("positions") ?: org.json.JSONArray()
            positions = (0 until pArr.length()).map { i ->
                val o = pArr.getJSONObject(i)
                StakingPosition(
                    id = o.optString("id"),
                    asset = o.optString("asset"),
                    chain = o.optString("chain"),
                    amount = o.optString("amount"),
                    apy = o.optString("apy"),
                    durationDays = o.optInt("duration_days"),
                    endsAt = o.optString("ends_at"),
                    status = o.optString("status"),
                    reward = if (o.isNull("reward")) null else o.optString("reward"),
                    accrued = if (o.isNull("accrued_estimate")) null else o.optString("accrued_estimate"),
                )
            }
        } catch (e: Exception) { err = e.message ?: "load error" }
    }

    LaunchedEffect(Unit) {
        withContext(Dispatchers.IO) { try { load() } catch (_: Exception) {} }
    }

    fun open(asset: StakingAsset) {
        err = ""; msg = ""
        try {
            val body = JSONObject()
                .put("asset", asset.asset)
                .put("chain", asset.chain)
                .put("amount", amount)
                .put("duration_days", asset.durations.first())
            api.post("/api/staking/positions", body.toString(), session.accessToken)
            amount = ""
            load()
        } catch (e: Exception) { err = e.message ?: "error" }
    }

    fun unlock(pos: StakingPosition) {
        err = ""; msg = ""
        try {
            val resp = api.post("/api/staking/positions/${pos.id}/unlock", "{}", session.accessToken)
            msg = JSONObject(resp).optString("message", "queued")
            load()
        } catch (e: Exception) { err = e.message ?: "error" }
    }

    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(12.dp)) {
        Text("Staking", style = MaterialTheme.typography.headlineSmall)
        if (err.isNotEmpty()) Text(err, color = MaterialTheme.colorScheme.error)
        if (msg.isNotEmpty()) Text(msg, color = MaterialTheme.colorScheme.primary)
        assets.forEachIndexed { idx, a ->
            Card(Modifier.fillMaxWidth().padding(vertical = 6.dp)) {
                Column(Modifier.padding(12.dp)) {
                    Text("${a.asset} (${a.chain}) · APY ${a.apy}", style = MaterialTheme.typography.titleSmall)
                    Text(
                        "durations: ${a.durations.joinToString(", ")} · min ${a.minAmount} · max ${a.maxAmount}" +
                        (a.priceUsd?.let { " · live \$${it}" } ?: ""),
                        style = MaterialTheme.typography.bodySmall,
                    )
                    Row(Modifier.padding(top = 8.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                        TextField(
                            value = if (pickIdx == idx) amount else "",
                            onValueChange = { amount = it; pickIdx = idx },
                            modifier = Modifier.weight(1f),
                            placeholder = { Text("amount") },
                            singleLine = true,
                        )
                        Button(onClick = { open(a) }) { Text("Stake") }
                    }
                }
            }
        }
        if (assets.isEmpty()) Text("No staking assets", style = MaterialTheme.typography.bodySmall)
        Text("Your positions", style = MaterialTheme.typography.titleSmall, modifier = Modifier.padding(top = 10.dp))
        positions.forEach { p ->
            Card(Modifier.fillMaxWidth().padding(vertical = 6.dp)) {
                Column(Modifier.padding(12.dp)) {
                    Text("${p.asset}/${p.chain} · ${p.amount}", style = MaterialTheme.typography.titleSmall)
                    Text("APY ${p.apy} · ${p.durationDays}d · matures ${p.endsAt.take(10)} · ${p.status}", style = MaterialTheme.typography.bodySmall)
                    (p.accrued ?: p.reward)?.let { Text("reward: $it", style = MaterialTheme.typography.bodySmall) }
                    if (p.status == "active") {
                        TextButton(onClick = { unlock(p) }) { Text("Unlock") }
                    } else if (p.status == "unlock_requested") {
                        Text("Queued for settlement", style = MaterialTheme.typography.bodySmall)
                    }
                }
            }
        }
    }
}
