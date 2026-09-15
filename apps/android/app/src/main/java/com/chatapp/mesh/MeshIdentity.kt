package com.chatapp.mesh

import org.json.JSONObject
import java.security.KeyFactory
import java.security.KeyPair
import java.security.KeyPairGenerator
import java.security.Signature
import java.security.spec.PKCS8EncodedKeySpec
import java.security.spec.X509EncodedKeySpec
import java.util.concurrent.ConcurrentHashMap
import javax.crypto.KeyAgreement
import javax.crypto.Mac
import javax.crypto.spec.SecretKeySpec

// MeshIdentity.kt — per-peer session keys, signed device identity, key
// revocation and anti-replay for the native Android mesh client.
//
// This mirrors services/mesh (session.go, signing.go, replay.go) so a device
// behaves identically to the Go engine: discovery beacons are Ed25519-signed
// and advertise an X25519 key-agreement public key; unicast payloads are
// sealed with a per-peer AES-256 session key derived via ECDH + HKDF-SHA256;
// a device that never held a peer's private key cannot derive the session
// key. It deliberately depends only on the JDK (X25519/Ed25519/HKDF are all
// standard), so the logic is unit-testable on the JVM without radio hardware.
//
// The wire format matches the Go engine's SignedBeacon JSON so a native
// device and a Go node interoperate.

/**
 * A device's mesh identity: an Ed25519 beacon-signing key pair and an X25519
 * key-agreement pair (with an epoch for rotation). The public halves are
 * advertised inside signed beacons; peers derive per-peer session keys from
 * the X25519 pair.
 */
class MeshIdentity {

    /** Ed25519 beacon-signing pair (stable for the device lifetime). */
    private val signer: KeyPair = KeyPairGenerator.getInstance("Ed25519").generateKeyPair()

    /** X25519 key-agreement pair; replaced on [rotate]. Guarded by [lock]. */
    private var kem: KeyPair = KeyPairGenerator.getInstance("X25519").generateKeyPair()

    /** Rotation epoch; bumped on [rotate]. */
    @Volatile var epoch: Long = 0
        private set

    private val lock = Any()

    /** The Ed25519 public key (raw 32 bytes). */
    fun signPublic(): ByteArray = rawPublic(signer.public.encoded)

    /** The current X25519 public key (raw 32 bytes). */
    fun kemPublic(): ByteArray = synchronized(lock) { rawPublic(kem.public.encoded) }

    /** Regenerates the key-agreement pair and bumps the epoch. */
    fun rotate() {
        val newKem = KeyPairGenerator.getInstance("X25519").generateKeyPair()
        synchronized(lock) {
            kem = newKem
            epoch += 1
        }
    }

    /**
     * Signs a beacon (with this device's key-agreement advertisement) and
     * returns the SignedBeacon JSON, byte-compatible with the Go engine.
     */
    fun signBeacon(deviceId: String, kind: String, transport: String, addr: String, seq: Long): ByteArray {
        val beacon = JSONObject()
            .put("device_id", deviceId)
            .put("kind", kind)
            .put("transport", transport)
            .put("addr", addr)
            .put("seq", seq)
        val kemPub = kemPublic()
        val payload = canonicalPayload(beacon, signPublic(), kemPub, epoch)
        val sig = ed25519Sign(signer.private.encoded, payload)
        val sb = JSONObject()
            .put("beacon", beacon)
            .put("pub_key", b64(signPublic()))
            .put("kem_pub", b64(kemPub))
            .put("kem_epoch", epoch)
            .put("sig", b64(sig))
        return sb.toString().toByteArray()
    }

    /**
     * Verifies a signed beacon against the pinned device-key map (TOFU) and
     * returns the parsed beacon fields, or null when the signature is invalid,
     * the key is revoked, or a pinned identity is claimed by a different key.
     */
    fun verifySignedBeacon(
        data: ByteArray,
        pinned: MutableMap<String, ByteArray>,
        revoked: Map<String, ByteArray>,
    ): SignedBeaconInfo? {
        val sb = try { JSONObject(String(data)) } catch (_: Exception) { return null }
        val beacon = sb.optJSONObject("beacon") ?: return null
        val deviceId = beacon.optString("device_id", "")
        if (deviceId.isEmpty()) return null
        val pubKey = try { unB64(sb.optString("pub_key", "")) } catch (_: Exception) { return null }
        val sig = try { unB64(sb.optString("sig", "")) } catch (_: Exception) { return null }
        if (pubKey.size != 32 || sig.isEmpty()) return null
        // Revoked identity may not re-enter.
        if (revoked.containsKey(deviceId)) return null
        // Pinned identity may not be claimed by a different key.
        val prev = pinned[deviceId]
        if (prev != null && !prev.contentEquals(pubKey)) return null
        val kemPub = try { unB64(sb.optString("kem_pub", "")) } catch (_: Exception) { ByteArray(0) }
        val kemEpoch = sb.optLong("kem_epoch", 0)
        val payload = canonicalPayload(beacon, pubKey, kemPub, kemEpoch)
        if (!ed25519Verify(pubKey, payload, sig)) return null
        pinned[deviceId] = pubKey
        return SignedBeaconInfo(
            deviceId = deviceId,
            kind = beacon.optString("kind", "member"),
            transport = beacon.optString("transport", "local_wifi"),
            addr = beacon.optString("addr", ""),
            seq = beacon.optLong("seq", 0),
            kemPub = kemPub,
            kemEpoch = kemEpoch,
        )
    }

    /**
     * Derives the per-peer AES-256 session key shared with a remote device.
     * Both sides compute the same key: X25519 is symmetric and the HKDF salt
     * is the sorted device-id pair, so derivation is order-independent.
     */
    fun sessionKey(localId: String, remotePub: ByteArray, remoteId: String): ByteArray? {
        if (remotePub.size != 32) return null
        return try {
            val pubSpec = X509EncodedKeySpec(wrapX25519Public(remotePub))
            val remoteKey = KeyFactory.getInstance("X25519").generatePublic(pubSpec)
            val ka = KeyAgreement.getInstance("X25519")
            ka.init(synchronized(lock) { kem.private })
            ka.doPhase(remoteKey, true)
            val shared = ka.generateSecret()
            val a = localId
            val b = remoteId
            val salt = if (a <= b) "$a|$b" else "$b|$a"
            hkdfSha256(shared, salt.toByteArray(), SESSION_INFO.toByteArray(), KEY_SIZE)
        } catch (_: Exception) {
            null
        }
    }

    companion object {
        const val SESSION_INFO = "chatapp-mesh-session-v2"
        const val KEY_SIZE = 32

        /** Canonical, signature-covered projection of a SignedBeacon. */
        private fun canonicalPayload(
            beacon: JSONObject,
            pubKey: ByteArray,
            kemPub: ByteArray,
            kemEpoch: Long,
        ): ByteArray {
            val o = JSONObject()
                .put("beacon", beacon)
                .put("pub_key", b64(pubKey))
                .put("kem_pub", b64(kemPub))
                .put("kem_epoch", kemEpoch)
            return o.toString().toByteArray()
        }

        /** Raw 32-byte public key from a SubjectPublicKeyInfo encoding. */
        fun rawPublic(spki: ByteArray): ByteArray {
            // X25519/Ed25519 SPKI: 12-byte header + 32-byte raw key.
            return spki.copyOfRange(spki.size - 32, spki.size)
        }

        /** Wraps a raw 32-byte X25519 public key into an SPKI encoding. */
        private fun wrapX25519Public(raw: ByteArray): ByteArray {
            // X25519 SPKI prefix (RFC 8410, OID 1.3.101.110).
            val prefix = byteArrayOf(
                0x30, 0x2a, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x6e,
                0x03, 0x21, 0x00,
            )
            return prefix + raw
        }

        private fun ed25519Sign(privateKey: ByteArray, data: ByteArray): ByteArray {
            val spec = PKCS8EncodedKeySpec(privateKey)
            val kf = KeyFactory.getInstance("Ed25519")
            val priv = kf.generatePrivate(spec)
            val s = Signature.getInstance("Ed25519")
            s.initSign(priv)
            s.update(data)
            return s.sign()
        }

        private fun ed25519Verify(publicKey: ByteArray, data: ByteArray, sig: ByteArray): Boolean {
            return try {
                val spec = X509EncodedKeySpec(wrapEd25519Public(publicKey))
                val kf = KeyFactory.getInstance("Ed25519")
                val pub = kf.generatePublic(spec)
                val s = Signature.getInstance("Ed25519")
                s.initVerify(pub)
                s.update(data)
                s.verify(sig)
            } catch (_: Exception) {
                false
            }
        }

        private fun wrapEd25519Public(raw: ByteArray): ByteArray {
            val prefix = byteArrayOf(
                0x30, 0x2a, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x70,
                0x03, 0x21, 0x00,
            )
            return prefix + raw
        }

        /** HKDF-SHA256 (RFC 5869) with the given salt, info and length. */
        fun hkdfSha256(ikm: ByteArray, salt: ByteArray, info: ByteArray, length: Int): ByteArray {
            val mac = Mac.getInstance("HmacSHA256")
            // Extract.
            mac.init(SecretKeySpec(salt, "HmacSHA256"))
            val prk = mac.doFinal(ikm)
            // Expand.
            val out = ByteArray(length)
            var t = ByteArray(0)
            var counter = 1
            var pos = 0
            while (pos < length) {
                mac.init(SecretKeySpec(prk, "HmacSHA256"))
                mac.update(t)
                mac.update(info)
                mac.update(counter.toByte())
                t = mac.doFinal()
                val n = minOf(t.size, length - pos)
                System.arraycopy(t, 0, out, pos, n)
                pos += n
                counter++
            }
            return out
        }

        fun b64(b: ByteArray): String = android.util.Base64.encodeToString(b, android.util.Base64.NO_WRAP)
        fun unB64(s: String): ByteArray = android.util.Base64.decode(s, android.util.Base64.NO_WRAP)
    }
}

/** Parsed, verified signed-beacon fields. */
data class SignedBeaconInfo(
    val deviceId: String,
    val kind: String,
    val transport: String,
    val addr: String,
    val seq: Long,
    val kemPub: ByteArray,
    val kemEpoch: Long,
)

/**
 * Anti-replay protection: a sliding bitmap window per source device id,
 * mirroring services/mesh/replay.go. Sequence numbers are monotonic per
 * sender; anything older than the window or already accepted is a replay.
 */
class ReplayFilter(private val windowBits: Int = 1024) {

    private class Window(val bits: Int) {
        var base: Long = 1
        val bitmap = LongArray(bits / 64)

        fun accept(seq: Long): Boolean {
            if (seq <= 0 || seq < base) return false
            if (seq >= base + bits) {
                val shift = seq - (base + bits - 1)
                if (shift >= bits) {
                    for (i in bitmap.indices) bitmap[i] = 0
                } else {
                    for (i in bitmap.indices) {
                        var v = 0L
                        val ni = i + (shift / 64).toInt()
                        if (ni < bitmap.size) v = bitmap[ni] ushr (shift % 64).toInt()
                        val ni2 = ni + 1
                        if (shift % 64 != 0L && ni2 < bitmap.size) {
                            v = v or (bitmap[ni2] shl (64 - (shift % 64).toInt()))
                        }
                        bitmap[i] = v
                    }
                }
                base = seq - bits + 1
            }
            val idx = (seq - base).toInt()
            val word = idx / 64
            val bit = idx % 64
            if ((bitmap[word] and (1L shl bit)) != 0L) return false
            bitmap[word] = bitmap[word] or (1L shl bit)
            return true
        }
    }

    private val windows = ConcurrentHashMap<String, Window>()

    /** Reports whether (src, seq) is new, recording it when it is. */
    fun check(src: String, seq: Long): Boolean {
        val w = windows.computeIfAbsent(src) { Window(windowBits) }
        return w.accept(seq)
    }
}
