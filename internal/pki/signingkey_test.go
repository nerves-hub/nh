package pki

import (
	"crypto/ed25519"
	"encoding/base64"
	"strings"
	"testing"
)

// exampleKey is a real NervesCloud-produced encrypted private key (no password),
// used to prove nh's format matches the server's exactly.
const exampleKey = "g3QAAAAFdwlwcm90ZWN0ZWRtAAAAYIN0AAAABHcDYWxnbQAAAAtQQkVTMi1IUzUxMncDZW5jbQAAAAdBMjU2R0NNdwNwMmNiAAAQAHcDcDJzbQAAACCjSng51psGAz5KhldHvWfsm5hy8RrM_zZRh7aaZVDokHcCaXZtAAAADG8HQySjiUifsjqCPncNZW5jcnlwdGVkX2tleW0AAAAAdwtjaXBoZXJfdGV4dG0AAABYB-qeqzw7G4JHpr2FAS7GzcVfyha7bBNQ_4JZMew1U0W1Ev79WIpbJOEnkFCebyKYTw89ZdZTbzFkX8VeVO1MdFJUmTmlXvuk2hLY8bNezlSnYYDSlMQpF3cKY2lwaGVyX3RhZ20AAAAQ-5Ai5nJXEXwssMSeI7nGSw"

// exampleKeyPublic is the public key embedded in exampleKey (base64 std).
const exampleKeyPublic = "cL8ksJftfY6NFIBsVaLM8Xhpge5y3BqWRVCuTZ4DpFI="

// TestDecryptRealExample proves interop: nh decrypts a real server-produced
// key and recovers the correct keypair.
func TestDecryptRealExample(t *testing.T) {
	priv, err := DecryptPrivateKey(exampleKey, "")
	if err != nil {
		t.Fatalf("DecryptPrivateKey: %v", err)
	}
	pub := priv.Public().(ed25519.PublicKey)
	if got := PublicKeyString(pub); got != exampleKeyPublic {
		t.Errorf("public key: got %q, want %q", got, exampleKeyPublic)
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	_, priv, err := GenerateSigningKey()
	if err != nil {
		t.Fatal(err)
	}

	for _, password := range []string{"", "hunter2", "a longer pass phrase 🔐"} {
		blob, err := EncryptPrivateKey(priv, password)
		if err != nil {
			t.Fatalf("EncryptPrivateKey(%q): %v", password, err)
		}
		got, err := DecryptPrivateKey(blob, password)
		if err != nil {
			t.Fatalf("DecryptPrivateKey(%q): %v", password, err)
		}
		if !got.Equal(priv) {
			t.Errorf("round trip with password %q did not recover the key", password)
		}
	}
}

// TestEncryptIsDecryptableLikeTheServer verifies our encryptor produces output
// that the same (server-verified) decryptor reads back — i.e. our format
// matches.
func TestEncryptIsDecryptableLikeTheServer(t *testing.T) {
	_, priv, _ := GenerateSigningKey()
	blob, err := EncryptPrivateKey(priv, "pw")
	if err != nil {
		t.Fatal(err)
	}

	// Structural sanity: it decodes as the expected ETF envelope.
	raw, err := base64.RawURLEncoding.DecodeString(blob)
	if err != nil {
		t.Fatalf("output is not valid base64url: %v", err)
	}
	outer, err := decodeETFMap(raw)
	if err != nil {
		t.Fatalf("decodeETFMap: %v", err)
	}
	for _, k := range []string{"protected", "iv", "encrypted_key", "cipher_text", "cipher_tag"} {
		if _, ok := outer[k]; !ok {
			t.Errorf("envelope missing field %q", k)
		}
	}
	header, err := decodeETFMap(outer["protected"].([]byte))
	if err != nil {
		t.Fatalf("decode protected: %v", err)
	}
	if string(header["alg"].([]byte)) != keyAlg || string(header["enc"].([]byte)) != keyEnc {
		t.Errorf("header alg/enc = %q/%q", header["alg"], header["enc"])
	}
	if header["p2c"].(int) != pbkdf2Iters {
		t.Errorf("p2c = %v", header["p2c"])
	}
}

func TestDecryptWrongPassword(t *testing.T) {
	_, priv, _ := GenerateSigningKey()
	blob, err := EncryptPrivateKey(priv, "correct")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptPrivateKey(blob, "wrong"); err == nil {
		t.Error("expected error decrypting with the wrong password")
	}
}

func TestDecryptGarbage(t *testing.T) {
	if _, err := DecryptPrivateKey("not-valid-base64url!!", ""); err == nil {
		t.Error("expected error on garbage input")
	}
	if _, err := DecryptPrivateKey(strings.Repeat("A", 8), ""); err == nil {
		t.Error("expected error on non-ETF input")
	}
}
