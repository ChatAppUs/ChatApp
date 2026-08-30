package com.chatapp.data

import android.content.Context

// SharedPreferences-backed session store. Tokens survive process death;
// logout clears them. Mirrors the web app's localStorage token handling.
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

    fun clear() {
        prefs.edit().clear().apply()
    }

    private companion object {
        const val KEY_ACCESS = "access_token"
        const val KEY_REFRESH = "refresh_token"
        const val KEY_USER = "user_id"
    }
}
