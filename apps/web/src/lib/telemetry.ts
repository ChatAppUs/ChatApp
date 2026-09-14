// Client-side telemetry beacons for the platform's QoE plane (§72/§73):
// video playback quality and call quality. Fire-and-forget: telemetry must
// never block or break playback, so failures are swallowed after one retry.
import { api } from "./api";

export interface QoEReport {
  video_id?: string;
  startup_ms?: number;
  buffer_ms?: number;
  buffering_events?: number;
  playback_failed?: boolean;
  resolution?: string;
  bitrate_kbps?: number;
  completed?: boolean;
  watch_ms?: number;
}

export interface CallQualityReport {
  room_id?: string;
  packet_loss_pct?: number;
  jitter_ms?: number;
  rtt_ms?: number;
  bitrate_kbps?: number;
  frame_rate?: number;
  resolution?: string;
}

function beacon(path: string, payload: Record<string, unknown>) {
  api<{ status: string }>(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  }).catch(() => {});
}

export function reportQoE(report: QoEReport) {
  beacon("/api/telemetry/qoe", report as Record<string, unknown>);
}

export function reportCallQuality(report: CallQualityReport) {
  beacon("/api/telemetry/call-quality", report as Record<string, unknown>);
}
