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
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"time"
)

// Verification-certificate parameters used to prove ownership of a CA when
// registering it with NervesCloud.
const (
	verificationSubjectCN = "certificate-verification"
	verificationSANScheme = "urn:nerveshub:verify:"
)

// GenerateCA generates an organization certificate authority on the secp256r1
// (NIST P-256) curve: a private key and a self-signed CA certificate with the
// given common name and the organization name, valid for validFor. It returns
// the private key and certificate as PEM.
//
// The certificate mirrors the root CA profile NervesHub registers (Elixir
// x509's :root_ca template): CA basic constraints with path length 1, cert/CRL
// signing key usage, and — critically — both subject and authority key
// identifier extensions. NervesHub requires the SKI and AKI to match device
// certificates to their CA, and Go does not emit an AKI on self-signed
// certificates unless it is set explicitly.
func GenerateCA(org, commonName string, validFor time.Duration) (keyPEM, certPEM []byte, err error) {
	if org == "" {
		return nil, nil, errors.New("pki: org is required for the CA certificate")
	}
	if commonName == "" {
		return nil, nil, errors.New("pki: a common name is required for the CA certificate")
	}
	if validFor <= 0 {
		return nil, nil, errors.New("pki: certificate validity must be positive")
	}

	key, keyPEM, err := newECKey()
	if err != nil {
		return nil, nil, err
	}

	serial, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("pki: generating certificate serial: %w", err)
	}

	skid, err := subjectKeyID(&key.PublicKey)
	if err != nil {
		return nil, nil, err
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName, Organization: []string{org}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(validFor),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
		// On a self-signed root the authority is the subject itself.
		SubjectKeyId:   skid,
		AuthorityKeyId: skid,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("pki: creating CA certificate: %w", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	return keyPEM, certPEM, nil
}

// subjectKeyID computes the RFC 5280 method-1 subject key identifier: the
// SHA-1 hash of the DER-encoded subject public key bit string. (SHA-1 is the
// standard derivation here, not a security boundary.)
func subjectKeyID(pub *ecdsa.PublicKey) ([]byte, error) {
	spkiDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("pki: encoding public key: %w", err)
	}
	var spki struct {
		Algorithm        pkix.AlgorithmIdentifier
		SubjectPublicKey asn1.BitString
	}
	if _, err := asn1.Unmarshal(spkiDER, &spki); err != nil {
		return nil, fmt.Errorf("pki: parsing public key: %w", err)
	}
	sum := sha1.Sum(spki.SubjectPublicKey.Bytes)
	return sum[:], nil
}

// SignDeviceCertificate generates a device keypair on the secp256r1 (NIST
// P-256) curve and a TLS client certificate signed by the given CA, with the
// device identifier as the common name and the organization name. It returns
// the device private key and certificate as PEM.
func SignDeviceCertificate(caCertPEM, caKeyPEM []byte, org, identifier string, validFor time.Duration) (keyPEM, certPEM []byte, err error) {
	if org == "" {
		return nil, nil, errors.New("pki: org is required for the certificate")
	}
	if identifier == "" {
		return nil, nil, errors.New("pki: device identifier is required for the certificate")
	}
	if validFor <= 0 {
		return nil, nil, errors.New("pki: certificate validity must be positive")
	}

	caCert, caKey, err := loadCAPair(caCertPEM, caKeyPEM)
	if err != nil {
		return nil, nil, err
	}

	key, keyPEM, err := newECKey()
	if err != nil {
		return nil, nil, err
	}

	serial, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("pki: generating certificate serial: %w", err)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               deviceSubject(org, identifier),
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(validFor),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("pki: signing device certificate: %w", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	return keyPEM, certPEM, nil
}

// SignVerificationCert generates a throwaway keypair and a certificate signed
// by the given CA, proving ownership of the CA's private key. The certificate
// carries the common name "certificate-verification" and a URI subject
// alternative name of urn:nerveshub:verify:<token>, where token comes from the
// server. Only the certificate PEM is returned; the keypair is discarded.
func SignVerificationCert(caCertPEM, caKeyPEM []byte, token string, validFor time.Duration) ([]byte, error) {
	if token == "" {
		return nil, errors.New("pki: verification token is required")
	}
	if validFor <= 0 {
		return nil, errors.New("pki: certificate validity must be positive")
	}

	caCert, caKey, err := loadCAPair(caCertPEM, caKeyPEM)
	if err != nil {
		return nil, err
	}

	sanURI, err := url.Parse(verificationSANScheme + token)
	if err != nil {
		return nil, fmt.Errorf("pki: building verification SAN: %w", err)
	}

	key, _, err := newECKey()
	if err != nil {
		return nil, err
	}

	serial, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, fmt.Errorf("pki: generating certificate serial: %w", err)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: verificationSubjectCN},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(validFor),
		URIs:         []*url.URL{sanURI},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("pki: signing verification certificate: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}

// loadCAPair parses a CA certificate and private key, verifying that the
// certificate is a CA and that the key matches it.
func loadCAPair(caCertPEM, caKeyPEM []byte) (*x509.Certificate, crypto.Signer, error) {
	caCert, err := ValidateCertificatePEM(caCertPEM)
	if err != nil {
		return nil, nil, err
	}
	if !caCert.IsCA {
		return nil, nil, errors.New("pki: the signing certificate is not a CA")
	}
	caKey, err := parsePrivateKeyPEM(caKeyPEM)
	if err != nil {
		return nil, nil, err
	}
	if !publicKeysEqual(caKey.Public(), caCert.PublicKey) {
		return nil, nil, errors.New("pki: the CA private key does not match the CA certificate")
	}
	return caCert, caKey, nil
}

// parsePrivateKeyPEM parses a PEM-encoded private key, accepting SEC1 EC,
// PKCS#1 RSA, and PKCS#8 encodings, so externally generated CAs (e.g. RSA from
// openssl) work alongside ncctl's own EC keys.
func parsePrivateKeyPEM(data []byte) (crypto.Signer, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("pki: no PEM data found in private key")
	}

	var key any
	var err error
	switch block.Type {
	case "EC PRIVATE KEY":
		key, err = x509.ParseECPrivateKey(block.Bytes)
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	default:
		return nil, fmt.Errorf("pki: unsupported private key PEM block %q", block.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("pki: parsing private key: %w", err)
	}

	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, errors.New("pki: private key cannot be used for signing")
	}
	return signer, nil
}

// publicKeysEqual reports whether two public keys are equal, using the Equal
// method that ecdsa/rsa/ed25519 public keys provide.
func publicKeysEqual(a, b crypto.PublicKey) bool {
	type equaler interface{ Equal(crypto.PublicKey) bool }
	eq, ok := a.(equaler)
	return ok && eq.Equal(b)
}
