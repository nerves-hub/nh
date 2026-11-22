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

package pki

import (
	"bytes"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
)

// Scheme is the signature scheme a NervesHub signing key belongs to. It
// decides what the key string holds and which firmware it can verify (see
// NervesHub.Accounts.OrgKey in nerves_hub_web).
type Scheme string

const (
	// SchemeEd25519 is an fwup signing key: the standard-base64 encoding of a
	// 32-byte Ed25519 public key. It verifies fwup .fw archives.
	SchemeEd25519 Scheme = "ed25519"
	// SchemeSecureBootV2RSA is a PEM RSA-3072 public key, the public half of an
	// espsecure.py Secure Boot v2 signing key. It verifies ESP-IDF .bin images.
	SchemeSecureBootV2RSA Scheme = "secure_boot_v2_rsa"
	// SchemeX509Certificate is a PEM X.509 certificate (optionally followed by
	// its issuers), the trust anchor for RAUC bundle signatures.
	SchemeX509Certificate Scheme = "x509_certificate"
)

// SecureBootV2RSABits is the only RSA key size Secure Boot v2 (and NervesHub)
// accepts.
const SecureBootV2RSABits = 3072

// Schemes lists every scheme NervesHub accepts, in the server's order.
func Schemes() []Scheme {
	return []Scheme{SchemeEd25519, SchemeSecureBootV2RSA, SchemeX509Certificate}
}

// SchemeNames returns Schemes as a comma-separated string, for help and
// error messages.
func SchemeNames() string {
	names := make([]string, 0, 3)
	for _, s := range Schemes() {
		names = append(names, string(s))
	}
	return strings.Join(names, ", ")
}

// ParseScheme validates a scheme name.
func ParseScheme(s string) (Scheme, error) {
	for _, known := range Schemes() {
		if s == string(known) {
			return known, nil
		}
	}
	return "", fmt.Errorf("unknown signing key scheme %q (expected one of: %s)", s, SchemeNames())
}

// SchemeOrDefault returns s, or ed25519 when s is empty. Servers from before
// signing key schemes existed send no scheme, and every key they hold is an
// Ed25519 key.
func SchemeOrDefault(s string) Scheme {
	if s == "" {
		return SchemeEd25519
	}
	return Scheme(s)
}

// ValidatePublicKey checks key the way the server's OrgKey.key_format_check
// does for scheme, so a bad key is reported before it is sent.
func ValidatePublicKey(scheme Scheme, key string) error {
	switch scheme {
	case SchemeEd25519:
		_, err := parseEd25519PublicKey(key)
		return err
	case SchemeSecureBootV2RSA:
		_, err := decodeRSAPublicKeyPEM([]byte(key))
		return err
	case SchemeX509Certificate:
		_, err := firstCertificate([]byte(key))
		return err
	default:
		_, err := ParseScheme(string(scheme))
		return err
	}
}

// PreparePublicKey turns the contents of a key or certificate file into the
// key string NervesHub stores for scheme, validating it on the way:
//
//   - ed25519: an fwup .pub file (base64 of a 32-byte public key).
//   - secure_boot_v2_rsa: a PEM RSA public key ("PUBLIC KEY" or
//     "RSA PUBLIC KEY"), or a PEM RSA private key such as espsecure.py
//     generate_signing_key writes, whose public half is extracted. Only the
//     public key is ever returned.
//   - x509_certificate: one or more PEM certificates (a leaf, then its
//     issuers). Any other PEM block, a private key in particular, is refused
//     so it is never uploaded.
func PreparePublicKey(scheme Scheme, data []byte) (string, error) {
	switch scheme {
	case SchemeEd25519:
		key := strings.TrimSpace(string(data))
		if _, err := parseEd25519PublicKey(key); err != nil {
			return "", err
		}
		return key, nil
	case SchemeSecureBootV2RSA:
		pub, err := rsaPublicKeyFromPEM(data)
		if err != nil {
			return "", err
		}
		return EncodeRSAPublicKeyPEM(pub)
	case SchemeX509Certificate:
		return certificateChainPEM(data)
	default:
		_, err := ParseScheme(string(scheme))
		return "", err
	}
}

// DetectScheme guesses the scheme of a key or certificate file: a PEM
// certificate is x509_certificate, a PEM RSA key is secure_boot_v2_rsa, and
// anything else is treated as an fwup ed25519 key.
func DetectScheme(data []byte) (Scheme, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		if bytes.Contains(data, []byte("-----BEGIN")) {
			return "", errors.New("could not parse the PEM data in the key file")
		}
		return SchemeEd25519, nil
	}
	switch block.Type {
	case "CERTIFICATE":
		return SchemeX509Certificate, nil
	case "PUBLIC KEY", "RSA PUBLIC KEY", "RSA PRIVATE KEY", "PRIVATE KEY", "ENCRYPTED PRIVATE KEY":
		return SchemeSecureBootV2RSA, nil
	default:
		return "", fmt.Errorf("cannot tell the signing key scheme of a PEM %q block; pass --scheme", block.Type)
	}
}

// KeyPreview returns a one-line summary of a stored key for tables, matching
// what the NervesHub web UI shows: an Ed25519 key in full, the Secure Boot v2
// eFuse digest for an RSA key, and the SHA-256 fingerprint of an X.509
// certificate. A key that does not parse falls back to its first and last
// lines.
func KeyPreview(scheme Scheme, key string) string {
	if digest, ok := KeyDigest(scheme, key); ok {
		return digest
	}
	return boundaryLines(key)
}

// KeyDigest returns the identifying digest KeyPreviewLabel names: the Secure
// Boot v2 eFuse digest of an RSA key or the SHA-256 fingerprint of a
// certificate. ok is false for ed25519 and for a key that does not parse.
func KeyDigest(scheme Scheme, key string) (digest string, ok bool) {
	switch scheme {
	case SchemeSecureBootV2RSA:
		if pub, err := decodeRSAPublicKeyPEM([]byte(key)); err == nil {
			return SecureBootV2KeyDigest(pub), true
		}
	case SchemeX509Certificate:
		if der, err := firstCertificate([]byte(key)); err == nil {
			return CertificateFingerprint(der), true
		}
	}
	return "", false
}

// KeyPreviewLabel names what KeyPreview shows for scheme, or "" when it shows
// the key itself.
func KeyPreviewLabel(scheme Scheme) string {
	switch scheme {
	case SchemeSecureBootV2RSA:
		return "eFuse digest"
	case SchemeX509Certificate:
		return "SHA-256 fingerprint"
	default:
		return ""
	}
}

// CertificateFingerprint formats the SHA-256 fingerprint of a DER
// certificate the way `openssl x509 -fingerprint -sha256` prints it.
func CertificateFingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	hexed := strings.ToUpper(hex.EncodeToString(sum[:]))
	parts := make([]string, 0, len(sum))
	for i := 0; i < len(hexed); i += 2 {
		parts = append(parts, hexed[i:i+2])
	}
	return strings.Join(parts, ":")
}

func parseEd25519PublicKey(key string) ([]byte, error) {
	const msg = "invalid key, please check this is a valid Ed25519 public key"
	// The server decodes with Elixir's strict Base.decode64, which rejects the
	// line breaks Go's decoder would otherwise skip.
	if key == "" || strings.ContainsAny(key, " \t\r\n") {
		return nil, errors.New(msg)
	}
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return nil, errors.New(msg)
	}
	if len(raw) == 64 {
		return nil, errors.New(msg + " (this looks like an fwup private key; pass the .pub file)")
	}
	if len(raw) != 32 {
		return nil, errors.New(msg)
	}
	return raw, nil
}

// decodeRSAPublicKeyPEM mirrors OrgKey.decode_rsa_public_key and the
// secure_boot_v2_rsa branch of key_format_check: the first PEM block must be
// an RSA public key, PKCS#1 or SubjectPublicKeyInfo, of exactly 3072 bits.
func decodeRSAPublicKeyPEM(data []byte) (*rsa.PublicKey, error) {
	const msg = "invalid key, expected a PEM-encoded RSA public key"
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New(msg)
	}
	var pub *rsa.PublicKey
	switch block.Type {
	case "RSA PUBLIC KEY":
		k, err := x509.ParsePKCS1PublicKey(block.Bytes)
		if err != nil {
			return nil, errors.New(msg)
		}
		pub = k
	case "PUBLIC KEY":
		k, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, errors.New(msg)
		}
		rk, ok := k.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New(msg)
		}
		pub = rk
	default:
		return nil, errors.New(msg)
	}
	if err := checkSecureBootV2Size(pub); err != nil {
		return nil, err
	}
	return pub, nil
}

// checkSecureBootV2Size matches the server's bit count, which is the
// modulus's byte length times eight.
func checkSecureBootV2Size(pub *rsa.PublicKey) error {
	bits := (pub.N.BitLen() + 7) / 8 * 8
	if bits != SecureBootV2RSABits {
		return fmt.Errorf("expected an RSA-%d public key, got %d bits", SecureBootV2RSABits, bits)
	}
	return nil
}

// rsaPublicKeyFromPEM reads an RSA public key, or the public half of an
// unencrypted RSA private key, from the first PEM block of data.
func rsaPublicKeyFromPEM(data []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("invalid key, expected a PEM-encoded RSA public or private key")
	}
	switch block.Type {
	case "RSA PUBLIC KEY", "PUBLIC KEY":
		return decodeRSAPublicKeyPEM(data)
	case "RSA PRIVATE KEY", "PRIVATE KEY":
		if _, encrypted := block.Headers["DEK-Info"]; encrypted {
			return nil, errEncryptedRSAKey
		}
		priv, err := ParseRSAPrivateKeyPEM(data)
		if err != nil {
			return nil, err
		}
		pub := &priv.PublicKey
		if err := checkSecureBootV2Size(pub); err != nil {
			return nil, err
		}
		return pub, nil
	case "ENCRYPTED PRIVATE KEY":
		return nil, errEncryptedRSAKey
	default:
		return nil, fmt.Errorf("invalid key, expected a PEM-encoded RSA public or private key, got a %q block", block.Type)
	}
}

var errEncryptedRSAKey = errors.New("the RSA private key is encrypted; pass its public key instead (espsecure.py extract_public_key --version 2 --keyfile <key> <out.pem>)")

// firstCertificate mirrors OrgKey.decode_certificate: the first PEM block
// must be a certificate. It returns that certificate's DER.
func firstCertificate(data []byte) ([]byte, error) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("invalid key, expected a PEM-encoded X.509 certificate")
	}
	if err := checkCertificateDER(block.Bytes); err != nil {
		return nil, err
	}
	return block.Bytes, nil
}

// checkCertificateDER accepts what Erlang's :public_key.pem_entry_decode
// does for a certificate. Go's x509 parser is stricter (it refuses negative
// serial numbers, for one), so a DER that is a single well-formed ASN.1
// SEQUENCE of three elements (tbsCertificate, signatureAlgorithm,
// signatureValue) is accepted even when Go cannot parse it further.
func checkCertificateDER(der []byte) error {
	if _, err := x509.ParseCertificate(der); err == nil {
		return nil
	}
	const msg = "invalid key, expected a PEM-encoded X.509 certificate"
	var cert struct {
		TBS       asn1.RawValue
		Algorithm asn1.RawValue
		Signature asn1.BitString
	}
	rest, err := asn1.Unmarshal(der, &cert)
	if err != nil || len(rest) != 0 || cert.TBS.Tag != asn1.TagSequence || cert.Algorithm.Tag != asn1.TagSequence {
		return errors.New(msg)
	}
	return nil
}

// certificateChainPEM re-encodes every certificate in data, in order, and
// refuses anything that is not a certificate.
func certificateChainPEM(data []byte) (string, error) {
	if _, err := firstCertificate(data); err != nil {
		return "", err
	}
	var out bytes.Buffer
	rest := data
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			return "", fmt.Errorf("the certificate file also holds a %q block; pass a file with only certificates", block.Type)
		}
		if err := checkCertificateDER(block.Bytes); err != nil {
			return "", fmt.Errorf("invalid certificate in chain: %w", err)
		}
		if err := pem.Encode(&out, &pem.Block{Type: "CERTIFICATE", Bytes: block.Bytes}); err != nil {
			return "", err
		}
	}
	return out.String(), nil
}

func boundaryLines(key string) string {
	var lines []string
	for _, l := range strings.Split(key, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	switch len(lines) {
	case 0:
		return ""
	case 1:
		return lines[0]
	default:
		return lines[0] + " … " + lines[len(lines)-1]
	}
}
