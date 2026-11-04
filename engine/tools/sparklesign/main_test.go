package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"
)

func TestSeedAndLegacyKeysSignIdentically(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i)
	}
	full := ed25519.NewKeyFromSeed(seed)
	data := []byte("Tailmulti-v0.6.0-macOS.zip")
	for _, encoded := range []string{base64.StdEncoding.EncodeToString(seed), base64.StdEncoding.EncodeToString(full)} {
		key, err := parseKey(" " + encoded + "\n")
		if err != nil {
			t.Fatal(err)
		}
		if !ed25519.Verify(full.Public().(ed25519.PublicKey), data, ed25519.Sign(key, data)) {
			t.Fatal("signature does not verify with the public key")
		}
	}
}

func TestRejectsMalformedKeys(t *testing.T) {
	for _, encoded := range []string{"", "not base64!", base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, err := parseKey(encoded); err == nil {
			t.Fatalf("accepted %q", encoded)
		}
	}
}
