// ChatApp call managers — fully self-built media stack:
//   - MeshCall: 1:1 / small calls. Signaling rides the ChatApp WebSocket
//     (type: "signal"); media flows peer-to-peer through our own STUN/TURN.
//   - SfuCall: meetings, large group calls and live broadcasting through the
//     ChatApp SFU (services/sfu) — no external kit or hosted media service.
// ICE servers (our own STUN/TURN with ephemeral credentials) come from the
// API when a room is created/joined.

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

// Overridden at runtime by our own STUN/TURN servers from the API; the
// default keeps local development working (loopback reachability only).
let RTC_CONFIG: RTCConfiguration = { iceServers: [] };

export function configureICE(servers: RTCIceServer[]) {
  RTC_CONFIG = { iceServers: servers };
}

export class MeshCall {
  private peers = new Map<string, RTCPeerConnection>();

  /** Replace the video track being sent to every peer (screen share on/off). */
  replaceVideoTrack(track: MediaStreamTrack | null) {
    this.peers.forEach((pc) => {
      const sender = pc.getSenders().find((s) => s.track?.kind === "video");
      sender?.replaceTrack(track).catch(() => {});
    });
  }
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

// ---- SFU client (meetings, group calls, live broadcast) ----
//
// Single RTCPeerConnection to the ChatApp SFU. The client is the "polite"
// peer: it makes the initial offer and accepts renegotiation offers from the
// SFU when tracks are added/removed.

export interface SfuSession {
  room_id: string;
  mode: string; // "meeting" | "live"
  role: string; // "publisher" | "subscriber"
  ticket: string;
  sfu_url: string;
  ice_servers: RTCIceServer[];
}

export class SfuCall {
  private pc: RTCPeerConnection | null = null;
  private ws: WebSocket | null = null;
  private pendingIce: RTCIceCandidateInit[] = [];
  private closed = false;

  constructor(
    private session: SfuSession,
    private localStream: MediaStream | null, // null for live viewers
    private onRemoteStream: (peerId: string, stream: MediaStream) => void,
    private onPeerLeft: (peerId: string) => void
  ) {}

  async join() {
    configureICE(this.session.ice_servers);
    const pc = new RTCPeerConnection(RTC_CONFIG);
    this.pc = pc;
    const url = `${this.session.sfu_url}?ticket=${encodeURIComponent(
      this.session.ticket
    )}&mode=${this.session.mode}`;
    this.ws = new WebSocket(url);

    pc.onicecandidate = (e) => {
      if (e.candidate && this.ws?.readyState === WebSocket.OPEN) {
        this.ws.send(JSON.stringify({ type: "ice", candidate: e.candidate.toJSON() }));
      }
    };
    pc.ontrack = (e) => {
      // The SFU sets the stream id to the publisher's user id.
      const peerId = e.streams[0]?.id ?? "publisher";
      if (e.streams[0]) this.onRemoteStream(peerId, e.streams[0]);
      e.streams[0]?.addEventListener("removetrack", () => this.onPeerLeft(peerId));
    };
    if (this.localStream) {
      this.localStream.getTracks().forEach((t) => pc.addTrack(t, this.localStream!));
    } else {
      // Viewers still need recv m-lines so the SFU can forward media.
      pc.addTransceiver("audio", { direction: "recvonly" });
      pc.addTransceiver("video", { direction: "recvonly" });
    }

    this.ws.onmessage = async (ev) => {
      const msg = JSON.parse(ev.data as string);
      try {
        if (msg.type === "offer") {
          // SFU-initiated renegotiation; we are polite.
          await pc.setRemoteDescription(JSON.parse(msg.sdp));
          const answer = await pc.createAnswer();
          await pc.setLocalDescription(answer);
          await this.flushIce();
          this.ws?.send(JSON.stringify({ type: "answer", sdp: answer }));
        } else if (msg.type === "answer") {
          await pc.setRemoteDescription(JSON.parse(msg.sdp));
          await this.flushIce();
        } else if (msg.type === "ice") {
          const c = JSON.parse(msg.candidate);
          if (pc.remoteDescription) {
            await pc.addIceCandidate(c).catch(() => {});
          } else {
            this.pendingIce.push(c);
          }
        }
      } catch { /* transient glare — polite side recovers on next offer */ }
    };

    await new Promise<void>((resolve, reject) => {
      this.ws!.onopen = () => resolve();
      this.ws!.onerror = () => reject(new Error("sfu connection failed"));
    });

    // Initial negotiation: client offers.
    const offer = await pc.createOffer();
    await pc.setLocalDescription(offer);
    this.ws.send(JSON.stringify({ type: "offer", sdp: offer }));
  }

  private async flushIce() {
    for (const c of this.pendingIce) {
      await this.pc?.addIceCandidate(c).catch(() => {});
    }
    this.pendingIce = [];
  }

  leave() {
    if (this.closed) return;
    this.closed = true;
    this.pc?.close();
    this.ws?.close();
  }
}

