// End-to-end encryption for direct messages.
//
// Each device generates an ECDH P-256 keypair; the private key never leaves
// IndexedDB/localStorage, and only the base64 SPKI public key is published to
// the server. Per-peer AES-GCM 256 keys are derived via ECDH + HKDF(SHA-256).
// The server relays opaque ciphertext only — it cannot read E2EE messages.

import { api } from "./api";

const PRIV_KEY = "chatapp.e2e.priv";
const PUB_KEY = "chatapp.e2e.pub";

const ECDH_PARAMS: EcKeyGenParams = { name: "ECDH", namedCurve: "P-256" };

function b64encode(buf: ArrayBuffer | Uint8Array): string {
  const bytes = buf instanceof Uint8Array ? buf : new Uint8Array(buf);
  let s = "";
  bytes.forEach((b) => (s += String.fromCharCode(b)));
  return btoa(s);
}

function b64decode(s: string): Uint8Array {
  return Uint8Array.from(atob(s), (c) => c.charCodeAt(0));
}

async function ensureKeyPair(): Promise<CryptoKeyPair> {
  const storedPriv = localStorage.getItem(PRIV_KEY);
  const storedPub = localStorage.getItem(PUB_KEY);
  if (storedPriv && storedPub) {
    const [privateKey, publicKey] = await Promise.all([
      crypto.subtle.importKey("pkcs8", b64decode(storedPriv) as BufferSource, ECDH_PARAMS, false, ["deriveBits"]),
      crypto.subtle.importKey("spki", b64decode(storedPub) as BufferSource, ECDH_PARAMS, true, []),
    ]);
    return { privateKey, publicKey };
  }
  const pair = await crypto.subtle.generateKey(ECDH_PARAMS, true, ["deriveBits"]);
  const [privRaw, pubRaw] = await Promise.all([
    crypto.subtle.exportKey("pkcs8", pair.privateKey),
    crypto.subtle.exportKey("spki", pair.publicKey),
  ]);
  localStorage.setItem(PRIV_KEY, b64encode(privRaw));
  localStorage.setItem(PUB_KEY, b64encode(pubRaw));
  return pair as CryptoKeyPair;
}

/** Publish this device's public key to the server (idempotent). */
export async function publishIdentityKey(): Promise<void> {
  const pair = await ensureKeyPair();
  const pubRaw = await crypto.subtle.exportKey("spki", pair.publicKey);
  await api("/api/e2e/key", {
    method: "PUT",
    body: JSON.stringify({ identity_key: b64encode(pubRaw) }),
  });
}

export async function hasIdentityKey(): Promise<boolean> {
  return !!(localStorage.getItem(PRIV_KEY) && localStorage.getItem(PUB_KEY));
}

const sharedCache = new Map<string, Promise<CryptoKey>>();

async function sharedKeyFor(peerId: string): Promise<CryptoKey> {
  let pending = sharedCache.get(peerId);
  if (!pending) {
    pending = (async () => {
      const { keys } = await api<{ keys: Record<string, string> }>(
        `/api/e2e/keys?user_ids=${encodeURIComponent(peerId)}`
      );
      const peerPubB64 = keys[peerId];
      if (!peerPubB64) throw new Error("peer has no E2EE key published");
      const pair = await ensureKeyPair();
      const peerPub = await crypto.subtle.importKey(
        "spki", b64decode(peerPubB64) as BufferSource, ECDH_PARAMS, false, []
      );
      const bits = await crypto.subtle.deriveBits(
        { name: "ECDH", public: peerPub }, pair.privateKey, 256
      );
      // HKDF-SHA256 with per-conversation info binding.
      const hkdfKey = await crypto.subtle.importKey("raw", bits, "HKDF", false, ["deriveKey"]);
      return crypto.subtle.deriveKey(
        {
          name: "HKDF",
          hash: "SHA-256",
          salt: new TextEncoder().encode("chatapp-e2e-v1"),
          info: new TextEncoder().encode(`dm:${peerId}`),
        },
        hkdfKey,
        { name: "AES-GCM", length: 256 },
        false,
        ["encrypt", "decrypt"]
      );
    })();
    sharedCache.set(peerId, pending);
  }
  return pending;
}

interface Envelope {
  v: 1;
  iv: string;
  ct: string;
}

export async function encryptFor(peerId: string, plaintext: string): Promise<string> {
  const key = await sharedKeyFor(peerId);
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const ct = await crypto.subtle.encrypt(
    { name: "AES-GCM", iv: iv as BufferSource },
    key,
    new TextEncoder().encode(plaintext)
  );
  const env: Envelope = { v: 1, iv: b64encode(iv), ct: b64encode(ct) };
  return JSON.stringify(env);
}

export async function decryptFrom(peerId: string, payload: string): Promise<string> {
  try {
    const env = JSON.parse(payload) as Envelope;
    if (env.v !== 1) return payload;
    const key = await sharedKeyFor(peerId);
    const pt = await crypto.subtle.decrypt(
      { name: "AES-GCM", iv: b64decode(env.iv) as BufferSource },
      key,
      b64decode(env.ct) as BufferSource
    );
    return new TextDecoder().decode(pt);
  } catch {
    return "[encrypted message — cannot decrypt on this device]";
  }
}

export function looksEncrypted(payload: string): boolean {
  return payload.startsWith('{"v":1,');
}
