package pki

import (
	"bytes"
	"crypto/x509"
	"testing"
	"time"
)

func TestGenerateCA(t *testing.T) {
	validFor := 31 * 365 * 24 * time.Hour
	keyPEM, certPEM, err := GenerateCA("acme", "acme-ca", validFor)
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}

	key, err := parsePrivateKeyPEM(keyPEM)
	if err != nil {
		t.Fatalf("parsing key: %v", err)
	}

	cert, err := ValidateCertificatePEM(certPEM)
	if err != nil {
		t.Fatalf("parsing certificate: %v", err)
	}

	// Self-signed CA matching the :root_ca profile.
	if !cert.IsCA {
		t.Error("certificate should be a CA")
	}
	if err := cert.CheckSignatureFrom(cert); err != nil {
		t.Errorf("self-signature: %v", err)
	}
	if cert.MaxPathLen != 1 || cert.MaxPathLenZero {
		t.Errorf("basic constraints: want path length 1, got MaxPathLen=%d MaxPathLenZero=%v",
			cert.MaxPathLen, cert.MaxPathLenZero)
	}
	if cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Errorf("key usage should include CertSign, got %v", cert.KeyUsage)
	}

	// NervesHub requires both key identifiers; on a self-signed root the
	// AKI equals the SKI.
	if len(cert.SubjectKeyId) == 0 {
		t.Error("missing SubjectKeyId extension")
	}
	if len(cert.AuthorityKeyId) == 0 {
		t.Error("missing AuthorityKeyId extension")
	}
	if !bytes.Equal(cert.SubjectKeyId, cert.AuthorityKeyId) {
		t.Error("AKI should equal SKI on a self-signed root CA")
	}
	if cert.Subject.CommonName != "acme-ca" {
		t.Errorf("CN: got %q", cert.Subject.CommonName)
	}
	if len(cert.Subject.Organization) != 1 || cert.Subject.Organization[0] != "acme" {
		t.Errorf("O: got %v", cert.Subject.Organization)
	}
	if !publicKeysEqual(key.Public(), cert.PublicKey) {
		t.Error("CA public key does not match the generated private key")
	}
	if span := cert.NotAfter.Sub(cert.NotBefore); span != validFor+time.Hour {
		t.Errorf("validity span: got %v, want %v", span, validFor+time.Hour)
	}
}

func TestSignDeviceCertificate(t *testing.T) {
	caKeyPEM, caCertPEM, err := GenerateCA("acme", "acme-ca", time.Hour*24)
	if err != nil {
		t.Fatal(err)
	}

	validFor := 90 * 24 * time.Hour
	keyPEM, certPEM, err := SignDeviceCertificate(caCertPEM, caKeyPEM, "acme", "dev-001", validFor)
	if err != nil {
		t.Fatalf("SignDeviceCertificate: %v", err)
	}

	caCert, err := ValidateCertificatePEM(caCertPEM)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := ValidateCertificatePEM(certPEM)
	if err != nil {
		t.Fatalf("parsing device certificate: %v", err)
	}

	// Signed by the CA, and linked to it by AKI -> SKI (how NervesHub
	// matches device certificates to a registered CA).
	if err := cert.CheckSignatureFrom(caCert); err != nil {
		t.Errorf("device certificate is not signed by the CA: %v", err)
	}
	if !bytes.Equal(cert.AuthorityKeyId, caCert.SubjectKeyId) {
		t.Error("device certificate AKI should equal the CA's SKI")
	}

	if cert.Subject.CommonName != "dev-001" {
		t.Errorf("CN: got %q", cert.Subject.CommonName)
	}
	if len(cert.Subject.Organization) != 1 || cert.Subject.Organization[0] != "acme" {
		t.Errorf("O: got %v", cert.Subject.Organization)
	}
	if cert.IsCA {
		t.Error("device certificate must not be a CA")
	}
	if len(cert.ExtKeyUsage) != 1 || cert.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		t.Errorf("ext key usage: got %v", cert.ExtKeyUsage)
	}

	key, err := parsePrivateKeyPEM(keyPEM)
	if err != nil {
		t.Fatalf("parsing device key: %v", err)
	}
	if !publicKeysEqual(key.Public(), cert.PublicKey) {
		t.Error("device certificate public key does not match the device private key")
	}
	if span := cert.NotAfter.Sub(cert.NotBefore); span != validFor+time.Hour {
		t.Errorf("validity span: got %v, want %v", span, validFor+time.Hour)
	}
}

func TestSignDeviceCertificateRejectsBadCA(t *testing.T) {
	caKeyPEM, caCertPEM, err := GenerateCA("acme", "acme-ca", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	// Garbage CA cert.
	if _, _, err := SignDeviceCertificate([]byte("nope"), caKeyPEM, "acme", "dev-001", time.Hour); err == nil {
		t.Error("garbage CA certificate should error")
	}

	// A leaf certificate is not a valid signer.
	_, leafPEM, err := SignDeviceCertificate(caCertPEM, caKeyPEM, "acme", "leaf", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := SignDeviceCertificate(leafPEM, caKeyPEM, "acme", "dev-001", time.Hour); err == nil {
		t.Error("a non-CA signer should error")
	}

	// A key that does not match the CA certificate.
	otherKeyPEM, _, err := GenerateCA("acme", "other-ca", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := SignDeviceCertificate(caCertPEM, otherKeyPEM, "acme", "dev-001", time.Hour); err == nil {
		t.Error("a mismatched CA key should error")
	}
}

func TestSignVerificationCert(t *testing.T) {
	caKeyPEM, caCertPEM, err := GenerateCA("acme", "acme-ca", 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	caCert, _ := ValidateCertificatePEM(caCertPEM)

	const token = "SFMyNTY.g2gDbQAAAAwxLU5lcnZlc1RlYW1uBgA1Ybm0ngFiAAAOEA.4xQ47wHRpIwP_ueQHuB0l9ldJqAA5uw2GDSTnkmQukY"
	vPEM, err := SignVerificationCert(caCertPEM, caKeyPEM, token, time.Hour)
	if err != nil {
		t.Fatalf("SignVerificationCert: %v", err)
	}

	cert, err := ValidateCertificatePEM(vPEM)
	if err != nil {
		t.Fatalf("parsing verification cert: %v", err)
	}
	if err := cert.CheckSignatureFrom(caCert); err != nil {
		t.Errorf("verification cert is not signed by the CA: %v", err)
	}
	if cert.Subject.CommonName != "certificate-verification" {
		t.Errorf("CN: got %q", cert.Subject.CommonName)
	}
	if len(cert.URIs) != 1 || cert.URIs[0].String() != "urn:nerveshub:verify:"+token {
		t.Errorf("SAN URI: got %v, want urn:nerveshub:verify:%s", cert.URIs, token)
	}
}

func TestSignVerificationCertWithRSACA(t *testing.T) {
	// An externally generated RSA CA (e.g. from openssl) must also work.
	caKeyPEM, caCertPEM := generateRSACA(t)

	vPEM, err := SignVerificationCert(caCertPEM, caKeyPEM, "tok-123", time.Hour)
	if err != nil {
		t.Fatalf("SignVerificationCert with RSA CA: %v", err)
	}
	caCert, _ := ValidateCertificatePEM(caCertPEM)
	cert, _ := ValidateCertificatePEM(vPEM)
	if err := cert.CheckSignatureFrom(caCert); err != nil {
		t.Errorf("verification cert is not signed by the RSA CA: %v", err)
	}
}

func TestSignVerificationCertValidation(t *testing.T) {
	caKeyPEM, caCertPEM, _ := GenerateCA("acme", "acme-ca", time.Hour)
	if _, err := SignVerificationCert(caCertPEM, caKeyPEM, "", time.Hour); err == nil {
		t.Error("empty token should error")
	}
	if _, err := SignVerificationCert([]byte("nope"), caKeyPEM, "tok", time.Hour); err == nil {
		t.Error("garbage CA cert should error")
	}
}
