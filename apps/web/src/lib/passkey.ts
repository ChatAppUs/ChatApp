"use client";

// WebAuthn/passkey client helpers. Platform authenticators (Touch ID,
// Windows Hello, Android biometrics, device passcode) gate key usage —
// biometric data never leaves the user's device.

import { api, saveTokens, Tokens } from "./api";

function b64uToBytes(s: string): Uint8Array {
  const pad = "=".repeat((4 - (s.length % 4)) % 4);
  const b64 = (s + pad).replace(/-/g, "+").replace(/_/g, "/");
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

function bytesToB64u(buf: ArrayBuffer): string {
  const bytes = new Uint8Array(buf);
  let bin = "";
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

export function passkeySupported(): boolean {
  return typeof window !== "undefined" && !!window.PublicKeyCredential;
}

export async function registerPasskey(name: string): Promise<void> {
  const opts = await api<any>("/api/auth/passkey/register/begin", {
    method: "POST",
    body: JSON.stringify({}),
  });
  const cred = (await navigator.credentials.create({
    publicKey: {
      challenge: b64uToBytes(opts.challenge) as BufferSource,
      rp: opts.rp,
      user: {
        id: b64uToBytes(opts.user.id) as BufferSource,
        name: opts.user.name,
        displayName: opts.user.displayName,
      },
      pubKeyCredParams: opts.pubKeyCredParams,
      timeout: opts.timeout,
      attestation: opts.attestation,
      excludeCredentials: (opts.excludeCredentials ?? []).map((c: any) => ({
        type: "public-key",
        id: b64uToBytes(c.id) as BufferSource,
        transports: c.transports,
      })),
      authenticatorSelection: opts.authenticatorSelection,
    },
  })) as PublicKeyCredential;
  if (!cred) throw new Error("ceremony cancelled");
  const resp = cred.response as AuthenticatorAttestationResponse;
  await api("/api/auth/passkey/register/finish", {
    method: "POST",
    body: JSON.stringify({
      name,
      response: {
        clientDataJSON: bytesToB64u(resp.clientDataJSON),
        attestationObject: bytesToB64u(resp.attestationObject),
        transports: resp.getTransports?.() ?? [],
      },
    }),
  });
}

export async function loginWithPasskey(
  username: string,
  totpCode?: string
): Promise<Tokens> {
  const opts = await api<any>(
    "/api/auth/passkey/login/begin",
    { method: "POST", body: JSON.stringify({ username }) },
    false
  );
  const assertion = (await navigator.credentials.get({
    publicKey: {
      challenge: b64uToBytes(opts.challenge) as BufferSource,
      rpId: opts.rpId,
      timeout: opts.timeout,
      userVerification: opts.userVerification,
      allowCredentials: (opts.allowCredentials ?? []).map((c: any) => ({
        type: "public-key",
        id: b64uToBytes(c.id) as BufferSource,
        transports: c.transports,
      })),
    },
  })) as PublicKeyCredential;
  if (!assertion) throw new Error("ceremony cancelled");
  const resp = assertion.response as AuthenticatorAssertionResponse;
  const tokens = await api<Tokens>(
    "/api/auth/passkey/login/finish",
    {
      method: "POST",
      body: JSON.stringify({
        id: bytesToB64u(assertion.rawId),
        totp_code: totpCode ?? "",
        response: {
          clientDataJSON: bytesToB64u(resp.clientDataJSON),
          authenticatorData: bytesToB64u(resp.authenticatorData),
          signature: bytesToB64u(resp.signature),
          userHandle: resp.userHandle ? bytesToB64u(resp.userHandle) : "",
        },
      }),
    },
    false
  );
  saveTokens(tokens);
  return tokens;
}
