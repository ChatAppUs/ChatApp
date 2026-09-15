package com.chatapp.mesh

import org.json.JSONObject
import java.util.concurrent.ConcurrentHashMap

// MeshRevocation.kt — network-wide distribution of revocation decisions.
//
// Mirrors services/mesh/revocdist.go: a locally revoked device disappears on
// one node only until a signed revocation notice is flooded through the mesh.
// The notice is signed by the revoker's Ed25519 identity key, the revoker must
// be a pinned identity, and the revoked key must match the pinned identity for
// the device — so a revoker cannot revoke a different key than the mesh
// trusts. Notices are applied exactly once per device and re-flooded to other
// relays.

data class RevocationNotice(
    val revoker: String,
    val deviceId: String,
    val publicKey: ByteArray,
    val issuedAt: Long,
    val sig: ByteArray,
)

object MeshRevocation {

    /**
     * The canonical, signature-covered projection. Byte-identical with Go's
     * revocationPayload JSON marshal (field order revoker, device_id,
     * public_key, issued_at; public_key is std base64).
     */
    fun payload(r: RevocationNotice): ByteArray {
        val pub = java.util.Base64.getEncoder().encodeToString(r.publicKey)
        val s = "{\"revoker\":\"${r.revoker}\",\"device_id\":\"${r.deviceId}\",\"public_key\":\"$pub\",\"issued_at\":${r.issuedAt}}"
        return s.toByteArray()
    }

    /** Signs a revocation notice with the revoker's identity key. */
    fun sign(identity: MeshIdentity, revoker: String, deviceId: String, publicKey: ByteArray, issuedAt: Long): RevocationNotice {
        val r = RevocationNotice(revoker, deviceId, publicKey.copyOf(), issuedAt, ByteArray(0))
        val sig = identity.signBytes(payload(r))
        return r.copy(sig = sig)
    }

    /**
     * Verifies a notice against the pinned device-key map: the revoker must be
     * pinned, the signature must verify, and the revoked key must match the
     * pinned identity for the device.
     */
    fun verify(r: RevocationNotice, pinned: Map<String, ByteArray>): RevocationNotice? {
        if (r.revoker.isEmpty() || r.deviceId.isEmpty() || r.publicKey.size != 32 || r.sig.isEmpty()) return null
        val revokerPub = pinned[r.revoker] ?: return null
        if (!MeshIdentity.verify(revokerPub, payload(r), r.sig)) return null
        pinned[r.deviceId]?.let { prev ->
            if (!prev.contentEquals(r.publicKey)) return null
        }
        return r
    }

    /** Marshals a notice to its wire JSON (field order matches Go's struct). */
    fun marshal(r: RevocationNotice): ByteArray = JSONObject()
        .put("revoker", r.revoker)
        .put("device_id", r.deviceId)
        .put("public_key", java.util.Base64.getEncoder().encodeToString(r.publicKey))
        .put("issued_at", r.issuedAt)
        .put("sig", java.util.Base64.getEncoder().encodeToString(r.sig))
        .toString()
        .toByteArray()

    /** Parses a wire JSON; null when the shape is not a revocation notice. */
    fun unmarshal(data: ByteArray): RevocationNotice? = try {
        val o = JSONObject(String(data))
        val revoker = o.optString("revoker", "")
        val deviceId = o.optString("device_id", "")
        val sig = o.optString("sig", "")
        if (revoker.isEmpty() || deviceId.isEmpty() || sig.isEmpty()) null
        else RevocationNotice(
            revoker = revoker,
            deviceId = deviceId,
            publicKey = java.util.Base64.getDecoder().decode(o.optString("public_key", "")),
            issuedAt = o.optLong("issued_at", 0),
            sig = java.util.Base64.getDecoder().decode(sig),
        )
    } catch (_: Exception) {
        null
    }
}

/** Applies flooded revocation notices exactly once per revoked device. */
class RevocationStore {
    private val notices = ConcurrentHashMap<String, RevocationNotice>()

    /** Applies a verified notice; reports false for a duplicate device. */
    fun apply(r: RevocationNotice): Boolean {
        val prev = notices.putIfAbsent(r.deviceId, r) ?: return true
        return false
    }

    fun isRevoked(deviceId: String): Boolean = notices.containsKey(deviceId)
}
