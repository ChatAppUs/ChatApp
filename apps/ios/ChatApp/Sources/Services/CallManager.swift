import Foundation
import WebRTC

/// WebRTC mesh call manager. SDP/ICE are relayed through the ChatApp
/// WebSocket as "signal" events; media flows peer-to-peer with DTLS-SRTP
/// encryption handled by the WebRTC stack.
final class CallManager: NSObject {
    enum State {
        case idle, ringing, connecting, connected, ended
    }

    var onStateChange: (State) -> Void = { _ in }

    private let socket: ChatSocket
    private let conversationId: String
    private let factory: RTCPeerConnectionFactory
    private var peerConnection: RTCPeerConnection?
    private var localVideoTrack: RTCVideoTrack?
    private var videoCapturer: RTCCameraVideoCapturer?

    init(socket: ChatSocket, conversationId: String) {
        self.socket = socket
        self.conversationId = conversationId
        let decoder = RTCDefaultVideoDecoderFactory()
        let encoder = RTCDefaultVideoEncoderFactory()
        self.factory = RTCPeerConnectionFactory(
            encoderFactory: encoder, decoderFactory: decoder
        )
        super.init()
    }

    func start(video: Bool) {
        let config = RTCConfiguration()
        config.iceServers = [RTCIceServer(urlStrings: ["stun:stun.l.google.com:19302"])]
        config.sdpSemantics = .unifiedPlan
        let constraints = RTCMediaConstraints(
            mandatoryConstraints: nil, optionalConstraints: nil
        )
        peerConnection = factory.peerConnection(
            with: config, constraints: constraints, delegate: self
        )

        let audioTrack = factory.audioTrack(withTrackId: "audio0")
        peerConnection?.add(audioTrack, streamIds: ["stream0"])

        if video {
            let videoSource = factory.videoSource()
            let capturer = RTCCameraVideoCapturer(delegate: videoSource)
            videoCapturer = capturer
            if let device = RTCCameraVideoCapturer.captureDevices()
                .first(where: { $0.position == .front }) {
                capturer.startCapture(with: device, format: preferredFormat(for: device), fps: 30)
            }
            let track = factory.videoTrack(with: videoSource, trackId: "video0")
            localVideoTrack = track
            peerConnection?.add(track, streamIds: ["stream0"])
        }

        onStateChange(.ringing)
        peerConnection?.offer(for: constraints) { [weak self] sdp, _ in
            guard let self, let sdp else { return }
            self.peerConnection?.setLocalDescription(sdp) { _ in }
            self.socket.sendSignal(conversationId: self.conversationId, signal: [
                "kind": "offer", "sdp": sdp.sdp,
            ])
        }
    }

    func handleSignal(_ signal: [String: Any]) {
        switch signal["kind"] as? String {
        case "offer":
            guard let sdp = signal["sdp"] as? String else { return }
            let desc = RTCSessionDescription(type: .offer, sdp: sdp)
            peerConnection?.setRemoteDescription(desc) { [weak self] _ in
                guard let self else { return }
                let constraints = RTCMediaConstraints(
                    mandatoryConstraints: nil, optionalConstraints: nil
                )
                self.peerConnection?.answer(for: constraints) { answer, _ in
                    guard let answer else { return }
                    self.peerConnection?.setLocalDescription(answer) { _ in }
                    self.socket.sendSignal(conversationId: self.conversationId, signal: [
                        "kind": "answer", "sdp": answer.sdp,
                    ])
                }
            }
        case "answer":
            guard let sdp = signal["sdp"] as? String else { return }
            peerConnection?.setRemoteDescription(
                RTCSessionDescription(type: .answer, sdp: sdp)
            ) { _ in }
        case "ice":
            guard let mid = signal["sdpMid"] as? String,
                  let line = signal["sdpMLineIndex"] as? Int,
                  let candidate = signal["candidate"] as? String else { return }
            peerConnection?.add(RTCIceCandidate(
                sdp: candidate, sdpMLineIndex: Int32(line), sdpMid: mid
            ))
        default:
            break
        }
    }

    func hangUp() {
        videoCapturer?.stopCapture()
        peerConnection?.close()
        peerConnection = nil
        onStateChange(.ended)
    }

    private func preferredFormat(for device: AVCaptureDevice) -> AVCaptureDevice.Format {
        // 1280x720 where available, otherwise the first supported format.
        let formats = RTCCameraVideoCapturer.supportedFormats(for: device)
        return formats.first(where: {
            CMVideoFormatDescriptionGetDimensions($0.formatDescription).width == 1280
        }) ?? formats[0]
    }
}

extension CallManager: RTCPeerConnectionDelegate {
    func peerConnection(_ peerConnection: RTCPeerConnection, didChange stateChanged: RTCSignalingState) {}
    func peerConnection(_ peerConnection: RTCPeerConnection, didAdd stream: RTCMediaStream) {}
    func peerConnection(_ peerConnection: RTCPeerConnection, didRemove stream: RTCMediaStream) {}
    func peerConnectionShouldNegotiate(_ peerConnection: RTCPeerConnection) {}
    func peerConnection(_ peerConnection: RTCPeerConnection, didChange newState: RTCIceConnectionState) {
        if newState == .connected { onStateChange(.connected) }
        if newState == .disconnected || newState == .failed { onStateChange(.ended) }
    }
    func peerConnection(_ peerConnection: RTCPeerConnection, didChange newState: RTCIceGatheringState) {}
    func peerConnection(_ peerConnection: RTCPeerConnection, didGenerate candidate: RTCIceCandidate) {
        socket.sendSignal(conversationId: conversationId, signal: [
            "kind": "ice",
            "sdpMid": candidate.sdpMid ?? "",
            "sdpMLineIndex": Int(candidate.sdpMLineIndex),
            "candidate": candidate.sdp,
        ])
    }
    func peerConnection(_ peerConnection: RTCPeerConnection, didRemove candidates: [RTCIceCandidate]) {}
    func peerConnection(_ peerConnection: RTCPeerConnection, didOpen dataChannel: RTCDataChannel) {}
}
