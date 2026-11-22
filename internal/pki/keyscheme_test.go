package pki

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	rsaOnce sync.Once
	rsaKey  *rsa.PrivateKey
)

// testRSAKey returns one RSA-3072 key shared by the tests; generating one
// takes long enough to be worth doing once.
func testRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	rsaOnce.Do(func() {
		k, err := GenerateSecureBootV2Key()
		if err != nil {
			t.Fatal(err)
		}
		rsaKey = k
	})
	return rsaKey
}

func testCertPEM(t *testing.T, cn string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestParseScheme(t *testing.T) {
	for _, s := range []string{"ed25519", "secure_boot_v2_rsa", "x509_certificate"} {
		if got, err := ParseScheme(s); err != nil || string(got) != s {
			t.Errorf("ParseScheme(%q) = %q, %v", s, got, err)
		}
	}
	if _, err := ParseScheme("rsa"); err == nil || !strings.Contains(err.Error(), "expected one of") {
		t.Errorf("unknown scheme: %v", err)
	}
	if SchemeOrDefault("") != SchemeEd25519 || SchemeOrDefault("x509_certificate") != SchemeX509Certificate {
		t.Error("SchemeOrDefault")
	}
}

func TestValidateEd25519(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	if err := ValidatePublicKey(SchemeEd25519, PublicKeyString(pub)); err != nil {
		t.Errorf("valid key: %v", err)
	}
	err := ValidatePublicKey(SchemeEd25519, PrivateKeyString(priv))
	if err == nil || !strings.Contains(err.Error(), "private key") {
		t.Errorf("private key should be refused with a hint, got %v", err)
	}
	enc := PublicKeyString(pub)
	for _, bad := range []string{"", "not base64!", base64.StdEncoding.EncodeToString(make([]byte, 31)), enc[:20] + "\n" + enc[20:]} {
		if err := ValidatePublicKey(SchemeEd25519, bad); err == nil {
			t.Errorf("ValidatePublicKey(%q) should fail", bad)
		}
	}
}

func TestValidateRSA(t *testing.T) {
	k := testRSAKey(t)
	spki, err := EncodeRSAPublicKeyPEM(&k.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pkcs1 := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&k.PublicKey)}))
	for _, ok := range []string{spki, pkcs1} {
		if err := ValidatePublicKey(SchemeSecureBootV2RSA, ok); err != nil {
			t.Errorf("valid RSA key refused: %v", err)
		}
	}

	small, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	smallPEM, _ := EncodeRSAPublicKeyPEM(&small.PublicKey)
	if err := ValidatePublicKey(SchemeSecureBootV2RSA, smallPEM); err == nil || !strings.Contains(err.Error(), "got 2048 bits") {
		t.Errorf("2048-bit key: %v", err)
	}

	ec, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ecDER, _ := x509.MarshalPKIXPublicKey(&ec.PublicKey)
	ecPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: ecDER}))
	// A private key is not a public key: the server rejects one, so must we.
	privPEM := string(EncodeRSAPrivateKeyPEM(k))
	for _, bad := range []string{ecPEM, privPEM, "garbage"} {
		if err := ValidatePublicKey(SchemeSecureBootV2RSA, bad); err == nil {
			t.Errorf("should refuse %.30q", bad)
		}
	}
}

func TestValidateX509(t *testing.T) {
	cert := testCertPEM(t, "rauc")
	if err := ValidatePublicKey(SchemeX509Certificate, string(cert)); err != nil {
		t.Errorf("valid cert: %v", err)
	}
	privFirst := string(EncodeRSAPrivateKeyPEM(testRSAKey(t))) + string(cert)
	if err := ValidatePublicKey(SchemeX509Certificate, privFirst); err == nil {
		t.Error("a file starting with a private key is not a certificate")
	}
}

func TestPreparePublicKeyRSAFromPrivateKey(t *testing.T) {
	k := testRSAKey(t)
	want, _ := EncodeRSAPublicKeyPEM(&k.PublicKey)

	pkcs8, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{
		"pkcs1": EncodeRSAPrivateKeyPEM(k),
		"pkcs8": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}),
		"spki":  []byte("leading text\n" + want),
	}
	for name, in := range inputs {
		got, err := PreparePublicKey(SchemeSecureBootV2RSA, in)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != want {
			t.Errorf("%s: got\n%s\nwant\n%s", name, got, want)
		}
		if strings.Contains(got, "PRIVATE") {
			t.Errorf("%s: private key material in the result", name)
		}
	}

	enc := []byte("-----BEGIN ENCRYPTED PRIVATE KEY-----\nAAAA\n-----END ENCRYPTED PRIVATE KEY-----\n")
	if _, err := PreparePublicKey(SchemeSecureBootV2RSA, enc); err == nil || !strings.Contains(err.Error(), "encrypted") {
		t.Errorf("encrypted key: %v", err)
	}
	legacyEnc := []byte("-----BEGIN RSA PRIVATE KEY-----\nProc-Type: 4,ENCRYPTED\nDEK-Info: AES-128-CBC,00000000000000000000000000000000\n\nAAAA\n-----END RSA PRIVATE KEY-----\n")
	if _, err := PreparePublicKey(SchemeSecureBootV2RSA, legacyEnc); err == nil || !strings.Contains(err.Error(), "encrypted") {
		t.Errorf("legacy encrypted key: %v", err)
	}
}

func TestPreparePublicKeyEd25519TrimsNewline(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	got, err := PreparePublicKey(SchemeEd25519, []byte(PublicKeyString(pub)+"\n"))
	if err != nil || got != PublicKeyString(pub) {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestPreparePublicKeyCertificateChain(t *testing.T) {
	leaf, issuer := testCertPEM(t, "leaf"), testCertPEM(t, "issuer")
	got, err := PreparePublicKey(SchemeX509Certificate, append(append([]byte("subject=leaf\n"), leaf...), issuer...))
	if err != nil {
		t.Fatal(err)
	}
	if got != string(leaf)+string(issuer) {
		t.Errorf("chain not kept in order:\n%s", got)
	}

	withKey := append(append([]byte(nil), leaf...), EncodeRSAPrivateKeyPEM(testRSAKey(t))...)
	if _, err := PreparePublicKey(SchemeX509Certificate, withKey); err == nil || !strings.Contains(err.Error(), "RSA PRIVATE KEY") {
		t.Errorf("a private key beside the certificate must be refused, got %v", err)
	}
}

func TestDetectScheme(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	k := testRSAKey(t)
	spki, _ := EncodeRSAPublicKeyPEM(&k.PublicKey)
	cases := map[string]Scheme{
		PublicKeyString(pub) + "\n":       SchemeEd25519,
		spki:                              SchemeSecureBootV2RSA,
		string(EncodeRSAPrivateKeyPEM(k)): SchemeSecureBootV2RSA,
		string(testCertPEM(t, "detect")):  SchemeX509Certificate,
	}
	for in, want := range cases {
		if got, err := DetectScheme([]byte(in)); err != nil || got != want {
			t.Errorf("DetectScheme(%.30q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := DetectScheme([]byte("-----BEGIN EC PRIVATE KEY-----\nAAAA\n-----END EC PRIVATE KEY-----\n")); err == nil {
		t.Error("an EC key has no scheme")
	}
}

// knownSBv2PublicKey and knownSBv2Digest were cross-checked with an
// independent implementation of espsecure.py's digest_sbv2_public_key
// (SHA-256 over n, e, R^2 mod n and -n^-1 mod 2^32, little-endian).
const knownSBv2PublicKey = `-----BEGIN PUBLIC KEY-----
MIIBojANBgkqhkiG9w0BAQEFAAOCAY8AMIIBigKCAYEA9ycdCE+qOLh/Xp/kbN4E
7CKTu7YyLJcKME8j7YLEHUdknQPIoyFhAHisnTqfh99/Py0iH1XQHwoY8Qga/y2h
yraDsSusvXGOyYowcgmD2WCvQYZ0Ym6EJ6Gd9TEu36+1dFe9mBKy/FSwSOoNQcAQ
8UYVZ2lKWYEMNsq1r1OPJbLt+eJ0w0Bah9EG+6gJ4KEtnDOyZA8C20Ya/lXDoAYo
P1B2kAgq7f6lkEOcZ7QgmOjhQZkYSfXZt300C/ZfPZafg7+E6k4QrsOYt2Libg1h
mjYSxrpZix9nGYMehDY+aKWfOdkk8if4oMyYxPXQHrmMatzhqDyiYrC76IgQu2o4
qDAXEXv5A9Y0E0DXmEZIlwjSwUw5ID+CuPyiKzHPvmKCXKtcycuAYQKZ8fS6Me1B
GRYmzxXV40fqvRpn2HnsjFAsLBBWsJ2MEr8ppQZtcThT0G0v0BlHXSuRoGjCRbWo
AvAmYVnSGg2sJQ7iTqqgSZAFGDTncPlV0qrhg4UNrn9LAgMBAAE=
-----END PUBLIC KEY-----
`

const knownSBv2Digest = "32b6f7fd427fa71fa2139b33f0c68ef3eb57f5f17ca05ff7d9787a772e40b87d"

func TestSecureBootV2KeyDigest(t *testing.T) {
	if got := KeyPreview(SchemeSecureBootV2RSA, knownSBv2PublicKey); got != knownSBv2Digest {
		t.Errorf("digest: got %s, want %s", got, knownSBv2Digest)
	}
}

func TestKeyPreview(t *testing.T) {
	cert := testCertPEM(t, "fp")
	block, _ := pem.Decode(cert)
	sum := sha256.Sum256(block.Bytes)
	fp := KeyPreview(SchemeX509Certificate, string(cert))
	if strings.ReplaceAll(fp, ":", "") != strings.ToUpper(hex.EncodeToString(sum[:])) || len(fp) != 95 {
		t.Errorf("fingerprint: %s", fp)
	}
	if KeyPreview(SchemeEd25519, "abc=") != "abc=" {
		t.Error("ed25519 preview should be the key")
	}
	if got := KeyPreview(SchemeSecureBootV2RSA, "-----BEGIN X-----\nzz\n-----END X-----\n"); got != "-----BEGIN X----- … -----END X-----" {
		t.Errorf("fallback preview: %q", got)
	}
}

func TestRSAPrivateKeyRoundTrip(t *testing.T) {
	k := testRSAKey(t)
	pemBytes := EncodeRSAPrivateKeyPEM(k)
	if !strings.HasPrefix(string(pemBytes), "-----BEGIN RSA PRIVATE KEY-----\n") {
		t.Errorf("want a PKCS#1 PEM, got %.40q", pemBytes)
	}
	got, err := ParseRSAPrivateKeyPEM(pemBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(k) || k.N.BitLen() != SecureBootV2RSABits || k.E != 65537 {
		t.Error("round trip or key parameters wrong")
	}
}

func TestLocalKeyPaths(t *testing.T) {
	got := LocalKeyPaths("/d", "acme", "ci")
	dir := filepath.Join("/d", "keys", "acme")
	want := []string{filepath.Join(dir, "ci.priv"), filepath.Join(dir, "ci.pub"), filepath.Join(dir, "ci.rsa.pem"), filepath.Join(dir, "ci.rsa.pub.pem")}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v", got)
	}
}

// A certificate Go's parser refuses (a negative serial number) but Erlang's
// :public_key decodes must still be accepted, as the server accepts it.
func TestX509AcceptsWhatServerAccepts(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "neg"}, NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	// Rewrite the serial INTEGER 7 (02 01 07, right after the v3 version tag)
	// to -1 (02 01 ff). The signature no longer matches, which neither side
	// checks at registration.
	marker := []byte{0xa0, 0x03, 0x02, 0x01, 0x02, 0x02, 0x01, 0x07}
	i := strings.Index(string(der), string(marker))
	if i < 0 {
		t.Fatal("serial not found")
	}
	der[i+len(marker)-1] = 0xff
	if _, err := x509.ParseCertificate(der); err == nil {
		t.Skip("this Go accepts negative serials; nothing to test")
	}
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	if err := ValidatePublicKey(SchemeX509Certificate, certPEM); err != nil {
		t.Errorf("negative-serial certificate refused: %v", err)
	}
	if got, err := PreparePublicKey(SchemeX509Certificate, []byte(certPEM)); err != nil || got != certPEM {
		t.Errorf("prepare: %v", err)
	}
	if _, ok := KeyDigest(SchemeX509Certificate, certPEM); !ok {
		t.Error("fingerprint should not need a parseable certificate")
	}
	bad := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte{0x30, 0x03, 0x02, 0x01, 0x01}}))
	if err := ValidatePublicKey(SchemeX509Certificate, bad); err == nil {
		t.Error("a DER that is not a certificate must be refused")
	}
}

func TestKeyDigestFallback(t *testing.T) {
	if _, ok := KeyDigest(SchemeSecureBootV2RSA, "-----BEGIN X-----\nzz\n-----END X-----\n"); ok {
		t.Error("unparseable key has no digest")
	}
	if _, ok := KeyDigest(SchemeEd25519, "abc="); ok {
		t.Error("ed25519 has no digest")
	}
}
