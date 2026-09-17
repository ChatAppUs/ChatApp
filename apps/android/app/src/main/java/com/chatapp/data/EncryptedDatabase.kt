package com.chatapp.data

import android.content.Context
import android.content.SharedPreferences
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKeys
import net.sqlcipher.database.SQLiteDatabase
import net.sqlcipher.database.SupportFactory

object EncryptedDatabase {

    private const val DB_NAME = "chatapp_anonymous.db"
    private const val PREFS_NAME = "chatapp_secure_prefs"
    private const val KEY_ALIAS = "chatapp_anonymous_master_key"

    @Volatile private var masterKey: String? = null

    fun getMasterKey(): String {
        return masterKey ?: synchronized(this) {
            masterKey ?: run {
                val spec = KeyGenParameterSpec.Builder(
                    KEY_ALIAS,
                    KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT
                ).apply {
                    setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                    setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                    setKeySize(256)
                }.build()
                val key = MasterKeys.getOrCreate(spec)
                masterKey = key
                key
            }
        }
    }

    fun getDatabase(context: Context): SQLiteDatabase {
        val factory = SupportFactory(getMasterKey().toByteArray())
        return SQLiteDatabase.openOrCreateDatabase(
            context.getDatabasePath(DB_NAME), getMasterKey(), null, null, factory
        )
    }

    fun getPreferences(context: Context): SharedPreferences {
        return EncryptedSharedPreferences.create(
            PREFS_NAME,
            getMasterKey(),
            context,
            EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
            EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM
        )
    }
}