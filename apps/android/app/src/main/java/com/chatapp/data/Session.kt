package com.chatapp.data

import android.content.Context
import android.util.Base64
import java.security.SecureRandom
import java.util.UUID

class Session(context: Context) {
    private val prefs = context.applicationContext
        .getSharedPreferences("chatapp.session", Context.MODE_PRIVATE)

    var accessToken: String?
        get() = prefs.getString(KEY_ACCESS, null)
        set(v) = prefs.edit().apply {
            if (v == null) remove(KEY_ACCESS) else putString(KEY_ACCESS, v)
        }.apply()

    var refreshToken: String?
        get() = prefs.getString(KEY_REFRESH, null)
        set(v) = prefs.edit().apply {
            if (v == null) remove(KEY_REFRESH) else putString(KEY_REFRESH, v)
        }.apply()

    var userId: String?
        get() = prefs.getString(KEY_USER, null)
        set(v) = prefs.edit().apply {
            if (v == null) remove(KEY_USER) else putString(KEY_USER, v)
        }.apply()

    val meshDeviceKey: String
        get() = prefs.getString(KEY_MESH_DEVICE, null) ?: run {
            val id = "android_" + UUID.randomUUID().toString()
            prefs.edit().putString(KEY_MESH_DEVICE, id).apply()
            id
        }

    val meshIdentityKey: ByteArray
        get() {
            val stored = prefs.getString(KEY_MESH_KEY, null)
            if (stored != null) return Base64.decode(stored, Base64.NO_WRAP)
            val bytes = ByteArray(32)
            SecureRandom().nextBytes(bytes)
            prefs.edit().putString(KEY_MESH_KEY, Base64.encodeToString(bytes, Base64.NO_WRAP)).apply()
            return bytes
        }

    // Identity spec §3.1 item 5: 30-day trusted-device login token. Only the
    // server-side hash matters; here we keep the raw token in private prefs.
    var deviceTrustToken: String?
        get() = prefs.getString(KEY_DEVICE_TRUST, null)
        set(v) = prefs.edit().apply {
            if (v == null) remove(KEY_DEVICE_TRUST) else putString(KEY_DEVICE_TRUST, v)
        }.apply()

    var darkTheme: Boolean
        get() = prefs.getBoolean(KEY_DARK, true)
        set(v) = prefs.edit().putBoolean(KEY_DARK, v).apply()

    fun clear() {
        val dark = darkTheme
        prefs.edit().clear().apply()
        darkTheme = dark
    }

    private companion object {
        const val KEY_ACCESS = "access_token"
        const val KEY_REFRESH = "refresh_token"
        const val KEY_USER = "user_id"
        const val KEY_MESH_DEVICE = "mesh_device_key"
        const val KEY_MESH_KEY = "mesh_identity_key"
        const val KEY_DARK = "dark_theme"
        const val KEY_DEVICE_TRUST = "device_trust_token"
    }
}
