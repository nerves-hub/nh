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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"time"
)

// newECKey generates a private key on the secp256r1 (NIST P-256) curve along
// with its PEM encoding ("EC PRIVATE KEY").
func newECKey() (*ecdsa.PrivateKey, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("pki: generating device key: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("pki: encoding device key: %w", err)
	}
	return key, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), nil
}

// deviceSubject is the certificate/CSR subject for a device: the identifier as
// the common name plus the organization.
func deviceSubject(org, identifier string) pkix.Name {
	return pkix.Name{
		CommonName:   identifier,
		Organization: []string{org},
	}
}

// GenerateDeviceKeyAndCSR generates a device keypair on the secp256r1 (NIST
// P-256) curve and a certificate signing request carrying the device
// identifier as the common name and the organization name. It returns the
// private key and CSR as PEM ("EC PRIVATE KEY" and "CERTIFICATE REQUEST").
func GenerateDeviceKeyAndCSR(org, identifier string) (keyPEM, csrPEM []byte, err error) {
	if org == "" {
		return nil, nil, errors.New("pki: org is required for the CSR")
	}
	if identifier == "" {
		return nil, nil, errors.New("pki: device identifier is required for the CSR")
	}

	key, keyPEM, err := newECKey()
	if err != nil {
		return nil, nil, err
	}

	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: deviceSubject(org, identifier),
	}, key)
	if err != nil {
		return nil, nil, fmt.Errorf("pki: creating certificate request: %w", err)
	}
	csrPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	return keyPEM, csrPEM, nil
}

// GenerateSelfSignedDeviceCertificate generates a device keypair on the
// secp256r1 (NIST P-256) curve and a self-signed TLS client certificate
// carrying the device identifier as the common name and the organization name.
// It returns the private key and certificate as PEM.
//
// Because the certificate is self-signed, Go does not emit subject/authority
// key identifier extensions, but NervesHub expects both, so they are set
// explicitly. On a self-signed certificate the authority is the subject
// itself, so the AKI equals the SKI.
func GenerateSelfSignedDeviceCertificate(org, identifier string, validFor time.Duration) (keyPEM, certPEM []byte, err error) {
	if org == "" {
		return nil, nil, errors.New("pki: org is required for the certificate")
	}
	if identifier == "" {
		return nil, nil, errors.New("pki: device identifier is required for the certificate")
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
		Subject:               deviceSubject(org, identifier),
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(validFor),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		SubjectKeyId:          skid,
		AuthorityKeyId:        skid,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("pki: creating self-signed device certificate: %w", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	return keyPEM, certPEM, nil
}

// serialNumberLimit bounds randomly generated certificate serials to 128 bits,
// comfortably within RFC 5280's 20-octet maximum.
var serialNumberLimit = new(big.Int).Lsh(big.NewInt(1), 128)

// ValidateCertificatePEM checks that data contains a PEM-encoded X.509
// certificate, returning the parsed certificate.
func ValidateCertificatePEM(data []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("pki: no PEM data found")
	}
	if block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("pki: expected a CERTIFICATE PEM block, found %q", block.Type)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("pki: parsing certificate: %w", err)
	}
	return cert, nil
}
