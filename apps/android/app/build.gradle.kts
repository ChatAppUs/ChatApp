plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
}

android {
    namespace = "com.chatapp"
    compileSdk = 34

    defaultConfig {
        applicationId = "com.chatapp"
        minSdk = 26
        targetSdk = 34
        versionCode = 1
        versionName = "1.0.0"
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions { jvmTarget = "17" }
    buildFeatures {
        compose = true
        buildConfig = true
    }

    defaultConfig {
        // Override at build time: -PCHATAPP_API_URL=https://api.example.com
        buildConfigField("String", "API_BASE_URL", "\"${project.findProperty("CHATAPP_API_URL") ?: "http://10.0.2.2:8080"}\"")
        buildConfigField("String", "WS_BASE_URL", "\"${project.findProperty("CHATAPP_WS_URL") ?: "ws://10.0.2.2:8080"}\"")
        buildConfigField("String", "MEDIA_BASE_URL", "\"${project.findProperty("CHATAPP_MEDIA_URL") ?: "http://10.0.2.2:8100"}\"")
    }
}

dependencies {
    val composeBom = platform("androidx.compose:compose-bom:2024.09.00")
    implementation(composeBom)
    implementation("androidx.core:core-ktx:1.13.1")
    implementation("androidx.activity:activity-compose:1.9.2")
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.navigation:navigation-compose:2.8.0")
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.8.5")
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
    implementation("org.json:json:20240303")
    implementation("org.webrtc:google-webrtc:1.0.32006")
    implementation("androidx.security:security-crypto:1.1.0-alpha06")
    implementation("com.google.zxing:core:3.5.3")
    implementation("androidx.camera:camera-camera2:1.3.4")
    implementation("androidx.camera:camera-lifecycle:1.3.4")
    implementation("androidx.camera:camera-view:1.3.4")
    implementation("com.google.mlkit:barcode-scanning:17.3.0")
}
