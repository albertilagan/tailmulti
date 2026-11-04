// Command sparklesign creates Sparkle EdDSA (Ed25519) update keys and signs release archives.
// The private key is the base64 32-byte seed, as Sparkle's generate_keys exports it.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "sparklesign:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("sparklesign", flag.ContinueOnError)
	generate := flags.Bool("generate", false, "print a new key pair as JSON")
	sign := flags.String("sign", "", "file to sign with the key in SPARKLE_ED_PRIVATE_KEY")
	public := flags.Bool("public", false, "print the public key for SPARKLE_ED_PRIVATE_KEY")
	if err := flags.Parse(args); err != nil {
		return err
	}
	switch {
	case *public && !*generate && *sign == "":
		key, err := parseKey(os.Getenv("SPARKLE_ED_PRIVATE_KEY"))
		if err != nil {
			return err
		}
		fmt.Println(base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)))
		return nil
	case *generate && *sign == "" && !*public:
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]string{
			"public":  base64.StdEncoding.EncodeToString(pub),
			"private": base64.StdEncoding.EncodeToString(priv.Seed()),
		})
	case *sign != "" && !*generate && !*public:
		key, err := parseKey(os.Getenv("SPARKLE_ED_PRIVATE_KEY"))
		if err != nil {
			return err
		}
		data, err := os.ReadFile(*sign)
		if err != nil {
			return err
		}
		fmt.Println(base64.StdEncoding.EncodeToString(ed25519.Sign(key, data)))
		return nil
	}
	return errors.New("use exactly one of -generate, -public or -sign FILE")
}

// parseKey accepts a 32-byte seed or Sparkle's older 64-byte private key encoding.
func parseKey(encoded string) (ed25519.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, errors.New("SPARKLE_ED_PRIVATE_KEY is not base64")
	}
	switch len(raw) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(raw), nil
	case ed25519.PrivateKeySize:
		return ed25519.NewKeyFromSeed(raw[:ed25519.SeedSize]), nil
	}
	return nil, errors.New("SPARKLE_ED_PRIVATE_KEY must be a 32-byte Ed25519 seed")
}
