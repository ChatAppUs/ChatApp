package main

import "testing"

// FuzzParseJWT ensures untrusted Authorization input cannot panic the parser.
// The seed cases exercise valid and structurally invalid paths.
func FuzzParseJWT(f *testing.F) {
	secret := []byte("fuzz-secret-key-32-bytes-minimum!!")
	valid, err := signJWT(secret, Claims{Sub: "fuzzer", Type: "access", Exp: 4_000_000_000})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add("")
	f.Add(".")
	f.Add("eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eA.invalid")
	f.Fuzz(func(t *testing.T, token string) {
		_, _ = parseJWT(secret, token)
	})
}
