package com.chatapp.call

import android.Manifest
import android.content.pm.PackageManager
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import com.chatapp.data.ChatSocket
import com.chatapp.data.Session
import com.chatapp.ui.ChatAppTheme
import org.json.JSONObject
import org.webrtc.AudioSource
import org.webrtc.AudioTrack
import org.webrtc.DefaultVideoDecoderFactory
import org.webrtc.DefaultVideoEncoderFactory
import org.webrtc.EglBase
import org.webrtc.IceCandidate
import org.webrtc.MediaConstraints
import org.webrtc.MediaStream
import org.webrtc.PeerConnection
import org.webrtc.PeerConnectionFactory
import org.webrtc.SdpObserver
import org.webrtc.SessionDescription
import org.webrtc.SurfaceViewRenderer
import org.webrtc.VideoCapturer
import org.webrtc.VideoSource
import org.webrtc.VideoTrack

/**
 * WebRTC mesh call: SDP offers/answers and ICE candidates are relayed through
 * the ChatApp WebSocket ("signal" events). Media flows peer-to-peer (DTLS-SRTP
 * encrypted by the WebRTC stack itself).
 */
class CallActivity : ComponentActivity() {

    private var status by mutableStateOf("Connecting…")
    private var inCall by mutableStateOf(false)

    private lateinit var socket: ChatSocket
    private lateinit var factory: PeerConnectionFactory
    private var peerConnection: PeerConnection? = null
    private val eglBase: EglBase = EglBase.create()
    private var conversationId: String = ""
    private var videoEnabled: Boolean = true

    private val permissionLauncher =
        registerForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) { grants ->
            if (grants.values.all { it }) startCall() else {
                status = "Camera/microphone permission required"
            }
        }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        conversationId = intent.getStringExtra("conversation_id") ?: ""
        videoEnabled = intent.getBooleanExtra("video", true)

        setContent {
            // Calls follow the same persisted light/dark choice as every screen.
            ChatAppTheme(dark = Session(applicationContext).darkTheme) {
                Column(
                    modifier = Modifier.fillMaxSize().padding(24.dp),
                    verticalArrangement = Arrangement.Center,
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    Text(status, style = MaterialTheme.typography.headlineSmall)
                    if (inCall) {
                        Button(
                            onClick = { hangUp() },
                            modifier = Modifier.padding(top = 24.dp),
                        ) { Text("Hang up") }
                    }
                }
            }
        }

        val needed = mutableListOf(Manifest.permission.RECORD_AUDIO)
        if (videoEnabled) needed.add(Manifest.permission.CAMERA)
        val missing = needed.filter {
            ContextCompat.checkSelfPermission(this, it) != PackageManager.PERMISSION_GRANTED
        }
        if (missing.isEmpty()) startCall() else permissionLauncher.launch(missing.toTypedArray())
    }

    private fun startCall() {
        val session = Session(this)
        val token = session.accessToken ?: run {
            status = "Not logged in"
            return
        }
        val wsBase = intent.getStringExtra("ws_base_url") ?: "ws://10.0.2.2:8080"

        PeerConnectionFactory.initialize(
            PeerConnectionFactory.InitializationOptions.builder(this).createInitializationOptions()
        )
        factory = PeerConnectionFactory.builder()
            .setVideoEncoderFactory(DefaultVideoEncoderFactory(eglBase.eglBaseContext, true, true))
            .setVideoDecoderFactory(DefaultVideoDecoderFactory(eglBase.eglBaseContext))
            .createPeerConnectionFactory()

        val iceServers = listOf(
            PeerConnection.IceServer.builder("stun:stun.l.google.com:19302").createIceServer()
        )
        peerConnection = factory.createPeerConnection(
            PeerConnection.RTCConfiguration(iceServers).apply {
                sdpSemantics = PeerConnection.SdpSemantics.UNIFIED_PLAN
            },
            object : PeerConnection.Observer {
                override fun onIceCandidate(candidate: IceCandidate) {
                    val signal = JSONObject()
                        .put("kind", "ice")
                        .put("sdpMid", candidate.sdpMid)
                        .put("sdpMLineIndex", candidate.sdpMLineIndex)
                        .put("candidate", candidate.sdp)
                    socket.sendSignal(conversationId, signal)
                }

                override fun onAddStream(stream: MediaStream) {
                    runOnUiThread { status = "Connected" }
                }

                override fun onSignalingChange(state: PeerConnection.SignalingState?) {}
                override fun onIceConnectionChange(state: PeerConnection.IceConnectionState?) {
                    if (state == PeerConnection.IceConnectionState.CONNECTED) {
                        runOnUiThread { status = "Connected" }
                    }
                }
                override fun onIceConnectionReceivingChange(receiving: Boolean) {}
                override fun onIceGatheringChange(state: PeerConnection.IceGatheringState?) {}
                override fun onIceCandidatesRemoved(candidates: Array<out IceCandidate>?) {}
                override fun onRemoveStream(stream: MediaStream?) {}
                override fun onDataChannel(channel: org.webrtc.DataChannel?) {}
                override fun onRenegotiationNeeded() {}
                override fun onAddTrack(receiver: org.webrtc.RtpReceiver?, streams: Array<out MediaStream>?) {}
            },
        )

        // Local audio (+video) tracks.
        val audioSource: AudioSource = factory.createAudioSource(MediaConstraints())
        val audioTrack: AudioTrack = factory.createAudioTrack("audio0", audioSource)
        peerConnection?.addTrack(audioTrack, listOf("stream0"))
        if (videoEnabled) {
            val capturer: VideoCapturer? = createCameraCapturer()
            if (capturer != null) {
                val videoSource: VideoSource = factory.createVideoSource(capturer.isScreencast)
                capturer.startCapture(1280, 720, 30)
                val videoTrack: VideoTrack = factory.createVideoTrack("video0", videoSource)
                peerConnection?.addTrack(videoTrack, listOf("stream0"))
            }
        }

        socket = ChatSocket(wsBase, token, onEvent = { evt ->
            if (evt.optString("type") == "signal" && evt.optString("conversation_id") == conversationId) {
                handleSignal(evt.getJSONObject("signal"))
            }
        })

        // Caller creates the offer.
        peerConnection?.createOffer(object : SimpleSdpObserver() {
            override fun onCreateSuccess(desc: SessionDescription) {
                peerConnection?.setLocalDescription(SimpleSdpObserver(), desc)
                socket.sendSignal(
                    conversationId,
                    JSONObject().put("kind", "offer").put("sdp", desc.description),
                )
                runOnUiThread {
                    status = "Ringing…"
                    inCall = true
                }
            }
        }, MediaConstraints())
    }

    private fun handleSignal(signal: JSONObject) {
        when (signal.optString("kind")) {
            "offer" -> {
                val desc = SessionDescription(
                    SessionDescription.Type.OFFER, signal.getString("sdp")
                )
                peerConnection?.setRemoteDescription(object : SimpleSdpObserver() {
                    override fun onSetSuccess() {
                        peerConnection?.createAnswer(object : SimpleSdpObserver() {
                            override fun onCreateSuccess(answer: SessionDescription) {
                                peerConnection?.setLocalDescription(SimpleSdpObserver(), answer)
                                socket.sendSignal(
                                    conversationId,
                                    JSONObject().put("kind", "answer").put("sdp", answer.description),
                                )
                            }
                        }, MediaConstraints())
                    }
                }, desc)
            }
            "answer" -> {
                peerConnection?.setRemoteDescription(
                    SimpleSdpObserver(),
                    SessionDescription(SessionDescription.Type.ANSWER, signal.getString("sdp")),
                )
            }
            "ice" -> {
                peerConnection?.addIceCandidate(
                    IceCandidate(
                        signal.getString("sdpMid"),
                        signal.getInt("sdpMLineIndex"),
                        signal.getString("candidate"),
                    )
                )
            }
        }
    }

    private fun createCameraCapturer(): VideoCapturer? {
        val enumerator = org.webrtc.Camera2Enumerator(this)
        for (name in enumerator.deviceNames) {
            if (enumerator.isFrontFacing(name)) return enumerator.createCapturer(name, null)
        }
        for (name in enumerator.deviceNames) {
            return enumerator.createCapturer(name, null)
        }
        return null
    }

    private fun hangUp() {
        peerConnection?.close()
        peerConnection = null
        socket.close()
        finish()
    }

    override fun onDestroy() {
        if (::socket.isInitialized) socket.close()
        peerConnection?.dispose()
        if (::factory.isInitialized) factory.dispose()
        eglBase.release()
        super.onDestroy()
    }

    private open class SimpleSdpObserver : SdpObserver {
        override fun onCreateSuccess(desc: SessionDescription) {}
        override fun onSetSuccess() {}
        override fun onCreateFailure(error: String?) {}
        override fun onSetFailure(error: String?) {}
    }
}
