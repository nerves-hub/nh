package pki

import (
	"bytes"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"
)

func TestGenerateDeviceKeyAndCSR(t *testing.T) {
	keyPEM, csrPEM, err := GenerateDeviceKeyAndCSR("acme", "dev-001")
	if err != nil {
		t.Fatalf("GenerateDeviceKeyAndCSR: %v", err)
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil || keyBlock.Type != "EC PRIVATE KEY" {
		t.Fatalf("key block: %+v", keyBlock)
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		t.Fatalf("parsing key: %v", err)
	}
	if key.Curve != elliptic.P256() {
		t.Errorf("curve: got %s, want P-256 (secp256r1)", key.Curve.Params().Name)
	}

	csrBlock, _ := pem.Decode(csrPEM)
	if csrBlock == nil || csrBlock.Type != "CERTIFICATE REQUEST" {
		t.Fatalf("csr block: %+v", csrBlock)
	}
	csr, err := x509.ParseCertificateRequest(csrBlock.Bytes)
	if err != nil {
		t.Fatalf("parsing csr: %v", err)
	}
	if err := csr.CheckSignature(); err != nil {
		t.Errorf("csr signature: %v", err)
	}
	if csr.Subject.CommonName != "dev-001" {
		t.Errorf("CN: got %q", csr.Subject.CommonName)
	}
	if len(csr.Subject.Organization) != 1 || csr.Subject.Organization[0] != "acme" {
		t.Errorf("O: got %v", csr.Subject.Organization)
	}
}

func TestGenerateDeviceKeyAndCSRValidation(t *testing.T) {
	if _, _, err := GenerateDeviceKeyAndCSR("", "dev-001"); err == nil {
		t.Error("empty org should error")
	}
	if _, _, err := GenerateDeviceKeyAndCSR("acme", ""); err == nil {
		t.Error("empty identifier should error")
	}
}

func TestValidateCertificatePEM(t *testing.T) {
	if _, err := ValidateCertificatePEM([]byte("not pem")); err == nil {
		t.Error("non-PEM input should error")
	}

	// A CSR is PEM, but not a certificate.
	_, csrPEM, err := GenerateDeviceKeyAndCSR("acme", "dev-001")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateCertificatePEM(csrPEM); err == nil {
		t.Error("a CSR should be rejected as not a certificate")
	}
}

func TestGenerateSelfSignedDeviceCertificate(t *testing.T) {
	validFor := 90 * 24 * time.Hour
	keyPEM, certPEM, err := GenerateSelfSignedDeviceCertificate("acme", "dev-001", validFor)
	if err != nil {
		t.Fatalf("GenerateSelfSignedDeviceCertificate: %v", err)
	}

	cert, err := ValidateCertificatePEM(certPEM)
	if err != nil {
		t.Fatalf("parsing certificate: %v", err)
	}

	// Subject and a non-CA client leaf.
	if cert.Subject.CommonName != "dev-001" || cert.Subject.Organization[0] != "acme" {
		t.Errorf("subject: %+v", cert.Subject)
	}
	if cert.IsCA {
		t.Error("device certificate must not be a CA")
	}
	if len(cert.ExtKeyUsage) != 1 || cert.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		t.Errorf("ext key usage: got %v", cert.ExtKeyUsage)
	}

	// Self-issued and verifiable with its own key.
	if !bytes.Equal(cert.RawIssuer, cert.RawSubject) {
		t.Error("certificate should be self-issued")
	}
	if err := cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature); err != nil {
		t.Errorf("certificate is not self-signed: %v", err)
	}

	// NervesHub requires SKI and AKI; on a self-signed cert AKI == SKI.
	if len(cert.SubjectKeyId) == 0 {
		t.Error("missing SubjectKeyId extension")
	}
	if len(cert.AuthorityKeyId) == 0 {
		t.Error("missing AuthorityKeyId extension")
	}
	if !bytes.Equal(cert.SubjectKeyId, cert.AuthorityKeyId) {
		t.Error("AKI should equal SKI on a self-signed certificate")
	}

	// The cert's public key matches the returned private key.
	key, err := parsePrivateKeyPEM(keyPEM)
	if err != nil {
		t.Fatalf("parsing key: %v", err)
	}
	if !publicKeysEqual(key.Public(), cert.PublicKey) {
		t.Error("certificate public key does not match the private key")
	}
	if span := cert.NotAfter.Sub(cert.NotBefore); span != validFor+time.Hour {
		t.Errorf("validity span: got %v", span)
	}
}

func TestGenerateSelfSignedDeviceCertificateValidation(t *testing.T) {
	if _, _, err := GenerateSelfSignedDeviceCertificate("", "dev-001", time.Hour); err == nil {
		t.Error("empty org should error")
	}
	if _, _, err := GenerateSelfSignedDeviceCertificate("acme", "", time.Hour); err == nil {
		t.Error("empty identifier should error")
	}
	if _, _, err := GenerateSelfSignedDeviceCertificate("acme", "dev-001", 0); err == nil {
		t.Error("non-positive validity should error")
	}
}
