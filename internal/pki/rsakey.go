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
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
)

// GenerateSecureBootV2Key creates a new RSA-3072 keypair for ESP-IDF Secure
// Boot v2, with the public exponent 65537 espsecure.py uses.
func GenerateSecureBootV2Key() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, SecureBootV2RSABits)
}

// EncodeRSAPrivateKeyPEM encodes priv as an unencrypted PKCS#1 PEM block
// ("BEGIN RSA PRIVATE KEY"), the format `espsecure.py generate_signing_key
// --version 2 --scheme rsa3072` writes and `espsecure.py sign_data --version 2
// --keyfile` reads.
func EncodeRSAPrivateKeyPEM(priv *rsa.PrivateKey) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
}

// EncodeRSAPublicKeyPEM encodes pub as a SubjectPublicKeyInfo PEM block
// ("BEGIN PUBLIC KEY"), what `espsecure.py extract_public_key --version 2` and
// `openssl rsa -pubout` write, and what NervesHub stores for a
// secure_boot_v2_rsa key.
func EncodeRSAPublicKeyPEM(pub *rsa.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), nil
}

// ParseRSAPrivateKeyPEM reads an unencrypted RSA private key from the first
// PEM block of data, PKCS#1 ("RSA PRIVATE KEY") or PKCS#8 ("PRIVATE KEY").
func ParseRSAPrivateKeyPEM(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("pki: no PEM data in RSA private key")
	}
	switch block.Type {
	case "RSA PRIVATE KEY":
		if _, encrypted := block.Headers["DEK-Info"]; encrypted {
			return nil, errEncryptedRSAKey
		}
		k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("pki: parsing RSA private key: %w", err)
		}
		return k, nil
	case "PRIVATE KEY":
		k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("pki: parsing private key: %w", err)
		}
		rk, ok := k.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("pki: private key is %T, not RSA", k)
		}
		return rk, nil
	case "ENCRYPTED PRIVATE KEY":
		return nil, errEncryptedRSAKey
	default:
		return nil, fmt.Errorf("pki: expected an RSA private key, got a PEM %q block", block.Type)
	}
}

// SecureBootV2KeyDigest returns the Secure Boot v2 public key digest as
// lowercase hex: the value `espsecure.py digest_sbv2_public_key` prints, that
// is burned into eFuse, and that the NervesHub web UI shows for the key.
//
// It is a SHA-256 over the public key section of a signature block: the
// modulus, the exponent, and the two Montgomery constants the ESP32 RSA
// peripheral needs (R^2 mod n and -n^-1 mod 2^32), all little-endian.
func SecureBootV2KeyDigest(pub *rsa.PublicKey) string {
	n := pub.N
	size := (n.BitLen() + 7) / 8

	rr := new(big.Int).Exp(big.NewInt(2), big.NewInt(int64(size*8*2)), n)

	mod32 := new(big.Int).Lsh(big.NewInt(1), 32)
	inv := new(big.Int).ModInverse(new(big.Int).Mod(n, mod32), mod32)
	var mPrime uint32
	if inv != nil { // n is odd for any valid RSA key, so the inverse exists
		mPrime = uint32(-inv.Uint64())
	}

	blob := make([]byte, 0, size*2+8)
	blob = append(blob, littleEndian(n, size)...)
	blob = binary.LittleEndian.AppendUint32(blob, uint32(pub.E))
	blob = append(blob, littleEndian(rr, size)...)
	blob = binary.LittleEndian.AppendUint32(blob, mPrime)

	sum := sha256.Sum256(blob)
	return hex.EncodeToString(sum[:])
}

func littleEndian(x *big.Int, size int) []byte {
	b := x.FillBytes(make([]byte, size))
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return b
}

// KeyDir is where nh keeps an organization's signing keys:
// <dataDir>/keys/<org>.
func KeyDir(dataDir, org string) string {
	return filepath.Join(dataDir, "keys", org)
}

// Ed25519PrivateKeyPath is the encrypted fwup private key nh writes for an
// ed25519 key: <dataDir>/keys/<org>/<name>.priv.
func Ed25519PrivateKeyPath(dataDir, org, name string) string {
	return filepath.Join(KeyDir(dataDir, org), name+".priv")
}

// Ed25519PublicKeyPath is the base64 public key nh writes for an ed25519 key:
// <dataDir>/keys/<org>/<name>.pub.
func Ed25519PublicKeyPath(dataDir, org, name string) string {
	return filepath.Join(KeyDir(dataDir, org), name+".pub")
}

// RSAPrivateKeyPath is the unencrypted PKCS#1 PEM private key nh writes for a
// secure_boot_v2_rsa key: <dataDir>/keys/<org>/<name>.rsa.pem. Firmware
// signing for ESP-IDF images reads it from here, so the path must not change.
func RSAPrivateKeyPath(dataDir, org, name string) string {
	return filepath.Join(KeyDir(dataDir, org), name+".rsa.pem")
}

// RSAPublicKeyPath is the PEM public key nh writes beside an RSA private key:
// <dataDir>/keys/<org>/<name>.rsa.pub.pem.
func RSAPublicKeyPath(dataDir, org, name string) string {
	return filepath.Join(KeyDir(dataDir, org), name+".rsa.pub.pem")
}

// LocalKeyPaths lists every file nh may keep for a key name, across schemes.
// Key names are unique within an organization whatever their scheme, so any
// of these existing means the name is taken locally.
func LocalKeyPaths(dataDir, org, name string) []string {
	return []string{
		Ed25519PrivateKeyPath(dataDir, org, name),
		Ed25519PublicKeyPath(dataDir, org, name),
		RSAPrivateKeyPath(dataDir, org, name),
		RSAPublicKeyPath(dataDir, org, name),
	}
}
