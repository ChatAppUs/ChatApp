package mesh

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestCrossPlatformPacketEnvelopeAndAESGCM(t *testing.T) {
	fixedKey := IdentityKey{}
	for i := range fixedKey {
		fixedKey[i] = byte(i)
	}
	fixedNonce := make([]byte, NonceSize)
	for i := range fixedNonce {
		fixedNonce[i] = byte(0xa0 + i)
	}
	fixture, err := base64.StdEncoding.DecodeString("hWoTXjbmctMDEeG8dRfgsxXfMTD03joY6XxDN0yTEZ2bExZ2qC0Haueuvg==")
	if err != nil {
		t.Fatal(err)
	}
	openedFixture, err := Decrypt(&fixedKey, fixture, fixedNonce)
	if err != nil || string(openedFixture) != "cross-platform mesh fixture" {
		t.Fatalf("native AES-GCM fixture did not decrypt: %q, %v", openedFixture, err)
	}

	key, err := NewIdentityKey()
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("cross-platform mesh")
	ciphertext, nonce, err := Encrypt(&key, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if len(nonce) != 12 {
		t.Fatalf("nonce length = %d, want 12", len(nonce))
	}
	opened, err := Decrypt(&key, ciphertext, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if string(opened) != string(plaintext) {
		t.Fatalf("plaintext = %q, want %q", opened, plaintext)
	}

	packet := NewPacket(KindGroupMessage, "android-device", "", 64)
	packet.GroupID = "group-1"
	packet.Payload = ciphertext
	packet.Nonce = nonce
	wire, err := packet.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(wire, &envelope); err != nil {
		t.Fatal(err)
	}
	if got := envelope["group_id"]; got != "group-1" {
		t.Fatalf("group_id = %v, want group-1", got)
	}
	if got := envelope["nonce"]; got != base64.StdEncoding.EncodeToString(nonce) {
		t.Fatalf("nonce encoding = %v, want standard base64", got)
	}
	decoded, err := UnmarshalPacket(wire)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.GroupID != packet.GroupID || string(decoded.Nonce) != string(nonce) {
		t.Fatalf("decoded envelope lost group or nonce: %#v", decoded)
	}
}
