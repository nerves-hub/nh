/*
Copyright © 2026 NervesHub

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/

// Package pki handles nh's security-sensitive key material: Ed25519 signing
// keys and the password-protected on-disk format NervesCloud uses for private
// keys.
//
// The private-key envelope is a JWE (PBES2-HS512 key derivation + A256GCM
// content encryption) serialized as an Erlang term and base64url-encoded. The
// plaintext is the standard-base64 encoding of the 64-byte Ed25519 private key
// (seed || public). The format and parameters were reverse-engineered from, and
// verified against, a real NervesCloud-produced key (see signingkey_test.go).
package pki

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"strings"
)

const (
	keyAlg      = "PBES2-HS512" // PBKDF2-HMAC-SHA512 key derivation
	keyEnc      = "A256GCM"     // AES-256-GCM content encryption
	pbkdf2Iters = 4096
	saltLen     = 32
	ivLen       = 12
	cekLen      = 32 // 256-bit content encryption key
	gcmTagLen   = 16
)

// GenerateSigningKey creates a new Ed25519 signing keypair.
func GenerateSigningKey() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

// PublicKeyString returns the standard-base64 encoding of an Ed25519 public
// key — the value uploaded to and stored by NervesCloud.
func PublicKeyString(pub ed25519.PublicKey) string {
	return base64.StdEncoding.EncodeToString(pub)
}

// PrivateKeyString returns the standard-base64 encoding of an Ed25519 private
// key — the form fwup accepts via --private-key.
func PrivateKeyString(priv ed25519.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(priv)
}

// ParsePrivateKeyString parses the standard-base64 encoding of a 64-byte
// Ed25519 private key (the inverse of PrivateKeyString).
func ParsePrivateKeyString(s string) (ed25519.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("pki: private key is not valid base64: %w", err)
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("pki: private key must be %d bytes, got %d", ed25519.PrivateKeySize, len(raw))
	}
	return ed25519.PrivateKey(raw), nil
}

// EncryptPrivateKey encrypts an Ed25519 private key into NervesCloud's
// password-protected envelope and returns it base64url-encoded. The password
// may be empty (the key is still encrypted, with an empty passphrase).
func EncryptPrivateKey(priv ed25519.PrivateKey, password string) (string, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("pki: invalid private key length %d", len(priv))
	}

	// The stored plaintext is the standard-base64 text of the 64-byte key.
	plaintext := []byte(base64.StdEncoding.EncodeToString(priv))

	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	iv := make([]byte, ivLen)
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}

	protected := encodeProtected(salt)
	cek, err := deriveKey(keyAlg, password, salt)
	if err != nil {
		return "", err
	}
	gcm, err := newGCM(cek)
	if err != nil {
		return "", err
	}

	// The protected header is the additional authenticated data.
	sealed := gcm.Seal(nil, iv, plaintext, protected)
	split := len(sealed) - gcmTagLen
	cipherText, cipherTag := sealed[:split], sealed[split:]

	outer := encodeOuter(protected, iv, cipherText, cipherTag)
	return base64.RawURLEncoding.EncodeToString(outer), nil
}

// DecryptPrivateKey reverses EncryptPrivateKey, returning the Ed25519 private
// key. The password must match the one used to encrypt (empty if none).
func DecryptPrivateKey(blob, password string) (ed25519.PrivateKey, error) {
	raw, err := base64.RawURLEncoding.DecodeString(trimPadding(strings.TrimSpace(blob)))
	if err != nil {
		return nil, fmt.Errorf("pki: decoding key: %w", err)
	}

	outer, err := decodeETFMap(raw)
	if err != nil {
		return nil, fmt.Errorf("pki: parsing key: %w", err)
	}
	protected, err := mapBytes(outer, "protected")
	if err != nil {
		return nil, err
	}
	iv, err := mapBytes(outer, "iv")
	if err != nil {
		return nil, err
	}
	cipherText, err := mapBytes(outer, "cipher_text")
	if err != nil {
		return nil, err
	}
	cipherTag, err := mapBytes(outer, "cipher_tag")
	if err != nil {
		return nil, err
	}

	header, err := decodeETFMap(protected)
	if err != nil {
		return nil, fmt.Errorf("pki: parsing key header: %w", err)
	}
	alg, err := mapBytes(header, "alg")
	if err != nil {
		return nil, err
	}
	enc, err := mapBytes(header, "enc")
	if err != nil {
		return nil, err
	}
	if string(alg) != keyAlg || string(enc) != keyEnc {
		return nil, fmt.Errorf("pki: unsupported key encryption (alg=%q enc=%q)", alg, enc)
	}
	salt, err := mapBytes(header, "p2s")
	if err != nil {
		return nil, err
	}
	iters, err := mapInt(header, "p2c")
	if err != nil {
		return nil, err
	}

	cek, err := pbkdf2.Key(sha512.New, password, pbes2Salt(string(alg), salt), iters, cekLen)
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(cek)
	if err != nil {
		return nil, err
	}

	plaintext, err := gcm.Open(nil, iv, append(append([]byte(nil), cipherText...), cipherTag...), protected)
	if err != nil {
		return nil, fmt.Errorf("pki: decrypting key (wrong password?): %w", err)
	}

	keyBytes, err := base64.StdEncoding.DecodeString(string(plaintext))
	if err != nil {
		return nil, fmt.Errorf("pki: decoding key material: %w", err)
	}
	if len(keyBytes) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("pki: unexpected private key length %d", len(keyBytes))
	}
	return ed25519.PrivateKey(keyBytes), nil
}

func encodeProtected(salt []byte) []byte {
	var w etfWriter
	w.version()
	w.mapHeader(4)
	w.atom("alg")
	w.binary([]byte(keyAlg))
	w.atom("enc")
	w.binary([]byte(keyEnc))
	w.atom("p2c")
	w.integer(pbkdf2Iters)
	w.atom("p2s")
	w.binary(salt)
	return w.buf
}

func encodeOuter(protected, iv, cipherText, cipherTag []byte) []byte {
	var w etfWriter
	w.version()
	w.mapHeader(5)
	w.atom("protected")
	w.binary(protected)
	w.atom("iv")
	w.binary(iv)
	w.atom("encrypted_key")
	w.binary(nil)
	w.atom("cipher_text")
	w.binary(cipherText)
	w.atom("cipher_tag")
	w.binary(cipherTag)
	return w.buf
}

func deriveKey(alg, password string, salt []byte) ([]byte, error) {
	return pbkdf2.Key(sha512.New, password, pbes2Salt(alg, salt), pbkdf2Iters, cekLen)
}

// pbes2Salt builds the PBKDF2 salt per RFC 7518 §4.8.1.1: UTF8(alg) || 0x00 ||
// salt input.
func pbes2Salt(alg string, salt []byte) []byte {
	out := make([]byte, 0, len(alg)+1+len(salt))
	out = append(out, alg...)
	out = append(out, 0x00)
	return append(out, salt...)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func mapBytes(m map[string]any, key string) ([]byte, error) {
	v, ok := m[key].([]byte)
	if !ok {
		return nil, fmt.Errorf("pki: missing or non-binary field %q", key)
	}
	return v, nil
}

func mapInt(m map[string]any, key string) (int, error) {
	v, ok := m[key].(int)
	if !ok {
		return 0, fmt.Errorf("pki: missing or non-integer field %q", key)
	}
	return v, nil
}

func trimPadding(s string) string {
	for len(s) > 0 && s[len(s)-1] == '=' {
		s = s[:len(s)-1]
	}
	return s
}
