// WebRTC mesh call manager. Signaling rides the ChatApp WebSocket
// (type: "signal"); media flows peer-to-peer via STUN. For meetings
// beyond ~6 participants, front with an SFU (LiveKit) — the signaling
// contract stays the same.

export interface SignalEnvelope {
  type: "signal";
  conversation_id: string;
  sender_id: string;
  signal: SignalPayload;
}

export type SignalPayload =
  | { kind: "join" }
  | { kind: "leave" }
  | { kind: "offer"; target: string; sdp: RTCSessionDescriptionInit }
  | { kind: "answer"; target: string; sdp: RTCSessionDescriptionInit }
  | { kind: "ice"; target: string; candidate: RTCIceCandidateInit };

const RTC_CONFIG: RTCConfiguration = {
  iceServers: [{ urls: ["stun:stun.l.google.com:19302"] }],
};

export class MeshCall {
  private peers = new Map<string, RTCPeerConnection>();
  private pendingIce = new Map<string, RTCIceCandidateInit[]>();

  constructor(
    private conversationId: string,
    private selfId: string,
    private send: (payload: SignalPayload) => void,
    private localStream: MediaStream,
    private onRemoteStream: (peerId: string, stream: MediaStream) => void,
    private onPeerLeft: (peerId: string) => void
  ) {}

  async join() {
    this.send({ kind: "join" });
  }

  async handleSignal(from: string, payload: SignalPayload) {
    switch (payload.kind) {
      case "join":
        await this.createOffer(from);
        break;
      case "leave":
        this.dropPeer(from);
        this.onPeerLeft(from);
        break;
      case "offer":
        if (payload.target === this.selfId) await this.acceptOffer(from, payload.sdp);
        break;
      case "answer":
        if (payload.target === this.selfId) await this.acceptAnswer(from, payload.sdp);
        break;
      case "ice":
        if (payload.target === this.selfId) await this.addIce(from, payload.candidate);
        break;
    }
  }

  private ensurePeer(peerId: string): RTCPeerConnection {
    let pc = this.peers.get(peerId);
    if (pc) return pc;
    pc = new RTCPeerConnection(RTC_CONFIG);
    this.localStream.getTracks().forEach((t) => pc!.addTrack(t, this.localStream));
    pc.onicecandidate = (e) => {
      if (e.candidate) {
        this.send({ kind: "ice", target: peerId, candidate: e.candidate.toJSON() });
      }
    };
    pc.ontrack = (e) => {
      if (e.streams[0]) this.onRemoteStream(peerId, e.streams[0]);
    };
    this.peers.set(peerId, pc);
    return pc;
  }

  private async createOffer(peerId: string) {
    const pc = this.ensurePeer(peerId);
    const offer = await pc.createOffer();
    await pc.setLocalDescription(offer);
    this.send({ kind: "offer", target: peerId, sdp: offer });
  }

  private async acceptOffer(peerId: string, sdp: RTCSessionDescriptionInit) {
    const pc = this.ensurePeer(peerId);
    await pc.setRemoteDescription(sdp);
    await this.flushIce(peerId, pc);
    const answer = await pc.createAnswer();
    await pc.setLocalDescription(answer);
    this.send({ kind: "answer", target: peerId, sdp: answer });
  }

  private async acceptAnswer(peerId: string, sdp: RTCSessionDescriptionInit) {
    const pc = this.peers.get(peerId);
    if (!pc) return;
    await pc.setRemoteDescription(sdp);
    await this.flushIce(peerId, pc);
  }

  private async addIce(peerId: string, candidate: RTCIceCandidateInit) {
    const pc = this.peers.get(peerId);
    if (pc?.remoteDescription) {
      await pc.addIceCandidate(candidate).catch(() => {});
    } else {
      const list = this.pendingIce.get(peerId) ?? [];
      list.push(candidate);
      this.pendingIce.set(peerId, list);
    }
  }

  private async flushIce(peerId: string, pc: RTCPeerConnection) {
    const list = this.pendingIce.get(peerId) ?? [];
    for (const c of list) await pc.addIceCandidate(c).catch(() => {});
    this.pendingIce.delete(peerId);
  }

  private dropPeer(peerId: string) {
    this.peers.get(peerId)?.close();
    this.peers.delete(peerId);
    this.pendingIce.delete(peerId);
  }

  leave() {
    this.send({ kind: "leave" });
    this.peers.forEach((pc) => pc.close());
    this.peers.clear();
    this.pendingIce.clear();
  }
}
