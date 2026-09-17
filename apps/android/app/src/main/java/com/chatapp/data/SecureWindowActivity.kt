package com.chatapp.data

import android.os.Bundle
import android.view.WindowManager
import androidx.appcompat.app.AppCompatActivity

open class SecureWindowActivity : AppCompatActivity() {

    private var screenProtectionEnabled = true

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableScreenProtection()
    }

    fun enableScreenProtection() {
        screenProtectionEnabled = true
        window.setFlags(
            WindowManager.LayoutParams.FLAG_SECURE,
            WindowManager.LayoutParams.FLAG_SECURE
        )
    }

    fun disableScreenProtection() {
        screenProtectionEnabled = false
        window.clearFlags(WindowManager.LayoutParams.FLAG_SECURE)
    }

    fun isScreenProtected(): Boolean = screenProtectionEnabled

    override fun onPause() {
        super.onPause()
        window.clearFlags(WindowManager.LayoutParams.FLAG_SECURE)
    }

    override fun onResume() {
        super.onResume()
        if (screenProtectionEnabled) enableScreenProtection()
    }
}