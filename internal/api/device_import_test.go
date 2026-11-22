package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testCA signs device certificates for manifests built in tests.
type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func newTestCA(t *testing.T) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test Signer"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
		SubjectKeyId:          []byte{1, 2, 3, 4},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &testCA{cert: cert, key: key}
}

// deviceCert returns the DER of a device certificate for key, signed by the
// CA, so it carries the CA's key identifier as its AKI.
func (ca *testCA) deviceCert(t *testing.T, serial int64, cn string, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	if key == nil {
		var err error
		if key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader); err != nil {
			t.Fatal(err)
		}
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

// tngEntry builds a Trust&Go manifest entry shaped like Microchip's (and the
// NervesHub test fixture): a base64, unpadded payload whose publicKeySet has
// the device certificate in the first key's x5c, followed by a nested list.
func tngEntry(t *testing.T, id string, der []byte) map[string]any {
	t.Helper()
	payload := map[string]any{
		"uniqueId": id,
		"publicKeySet": map[string]any{
			"keys": []any{
				map[string]any{"kid": "0", "kty": "EC", "x5c": []any{base64.RawStdEncoding.EncodeToString(der), "**unused**"}},
				[]any{map[string]any{"kid": "1"}},
			},
		},
	}
	return tngEntryWithPayload(t, id, payload)
}

func tngEntryWithPayload(t *testing.T, id string, payload any) map[string]any {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{
		"header":    map[string]any{"uniqueId": id},
		"payload":   base64.RawStdEncoding.EncodeToString(b),
		"protected": "eyJhbGciOiJFUzI1NiJ9",
		"signature": "sig",
	}
}

func manifestJSON(t *testing.T, entries ...any) []byte {
	t.Helper()
	b, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseTrustAndGoManifest(t *testing.T) {
	ca := newTestCA(t)
	der1 := ca.deviceCert(t, 255, "SN0001", nil)
	der2 := ca.deviceCert(t, 256, "SN0002", nil)
	data := manifestJSON(t, tngEntry(t, "SN0001", der1), tngEntry(t, "SN0002", der2))

	m, err := ParseTrustAndGoManifest(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if bad := m.Invalid(); len(bad) != 0 {
		t.Fatalf("unexpected invalid rows: %+v", bad)
	}
	if m.Format != "microchip_trust_and_go" || string(m.Raw) != string(data) {
		t.Errorf("format/raw: %q", m.Format)
	}
	if len(m.Rows) != 2 {
		t.Fatalf("rows: %d", len(m.Rows))
	}
	r := m.Rows[0]
	if r.Row != 1 || r.Identifier != "SN0001" || r.Certificate == nil {
		t.Fatalf("row 1: %+v", r)
	}
	c := r.Certificate
	if c.Serial != "255" || c.Subject != "CN=SN0001" || !c.NotAfter.Equal(time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("certificate: %+v", c)
	}
	if len(c.Fingerprint) != 40 || strings.ToUpper(c.Fingerprint) != c.Fingerprint {
		t.Errorf("fingerprint should be uppercase hex SHA-1, got %q", c.Fingerprint)
	}
	if c.Fingerprint != CertificateFingerprint(der1) {
		t.Errorf("fingerprint: got %q", c.Fingerprint)
	}
	if m.Rows[1].Row != 2 || m.Rows[1].Identifier != "SN0002" {
		t.Errorf("row 2: %+v", m.Rows[1])
	}
}

// Padded base64 is accepted too: Base.decode64(padding: false) takes both.
func TestParseTrustAndGoManifestPaddedBase64(t *testing.T) {
	ca := newTestCA(t)
	der := ca.deviceCert(t, 1, "SN1", nil)
	entry := tngEntry(t, "SN1", der)
	p, _ := base64.RawStdEncoding.DecodeString(entry["payload"].(string))
	entry["payload"] = base64.StdEncoding.EncodeToString(p)

	m, err := ParseTrustAndGoManifest(manifestJSON(t, entry))
	if err != nil {
		t.Fatal(err)
	}
	if bad := m.Invalid(); len(bad) != 0 {
		t.Fatalf("padded payload should parse: %+v", bad)
	}
}

func TestParseTrustAndGoManifestTooLarge(t *testing.T) {
	data := []byte("[" + strings.Repeat(" ", MaxDeviceImportBytes) + "]")
	if _, err := ParseTrustAndGoManifest(data); err == nil || !strings.Contains(err.Error(), "split it into smaller files") {
		t.Errorf("want a size error, got %v", err)
	}
}

func TestParseTrustAndGoManifestFileErrors(t *testing.T) {
	cases := map[string]string{
		`{"header":{}}`: "must be a JSON list",
		`"text"`:        "must be a JSON list",
		`null`:          "must be a JSON list",
		`[{"a":`:        "invalid JSON",
		`[]`:            "no entries",
	}
	for in, want := range cases {
		_, err := ParseTrustAndGoManifest([]byte(in))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want error containing %q, got %v", in, want, err)
		}
	}
}

func TestParseTrustAndGoManifestRowErrors(t *testing.T) {
	ca := newTestCA(t)
	der := ca.deviceCert(t, 7, "SN7", nil)
	good := func() map[string]any { return tngEntry(t, "SN7", der) }
	withPayload := func(p any) map[string]any { return tngEntryWithPayload(t, "SN7", p) }
	keySet := func(keys any) map[string]any {
		return withPayload(map[string]any{"publicKeySet": map[string]any{"keys": keys}})
	}
	x5c := func(v any) map[string]any { return keySet([]any{map[string]any{"x5c": v}}) }

	// No AKI: a self-signed certificate made without a key identifier.
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(9), NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour)}
	noAKI, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		entry any
		want  string
	}{
		{"not an object", "SN7", "not a JSON object"},
		{"header not an object", func() any { e := good(); e["header"] = "SN7"; return e }(), "header is not a JSON object"},
		{"no uniqueId", func() any { e := good(); e["header"] = map[string]any{}; return e }(), "uniqueId (the device identifier) is missing"},
		{"null header", func() any { e := good(); e["header"] = nil; return e }(), "uniqueId (the device identifier) is missing"},
		{"blank uniqueId", func() any { e := good(); e["header"] = map[string]any{"uniqueId": " "}; return e }(), "is missing"},
		{"numeric uniqueId", func() any { e := good(); e["header"] = map[string]any{"uniqueId": 7}; return e }(), "uniqueId is not a string"},
		{"no payload", func() any { e := good(); delete(e, "payload"); return e }(), "payload is missing"},
		{"payload not a string", func() any { e := good(); e["payload"] = 1; return e }(), "payload is missing or not a string"},
		{"null payload", func() any { e := good(); e["payload"] = nil; return e }(), "payload is missing or not a string"},
		{"url-safe base64", func() any { e := good(); e["payload"] = "ab-_"; return e }(), "payload is not valid base64"},
		{"line break in base64", func() any {
			e := good()
			p := e["payload"].(string)
			e["payload"] = p[:8] + "\n" + p[8:]
			return e
		}(), "payload is not valid base64"},
		{"payload not an object", withPayload([]any{1}), "payload does not decode to a JSON object"},
		{"no publicKeySet", withPayload(map[string]any{}), "no publicKeySet object"},
		{"keys not a list", keySet(map[string]any{}), "publicKeySet.keys is missing or not a list"},
		{"non-object before x5c", keySet([]any{[]any{}, map[string]any{"x5c": []any{"AA"}}}), "not an object before the one with the certificate"},
		{"no x5c", keySet([]any{map[string]any{"kid": "0"}}), "no key in publicKeySet.keys has an x5c"},
		{"x5c not a list", x5c("AA"), "x5c is not a list"},
		{"x5c empty", x5c([]any{}), "x5c has no certificate"},
		{"x5c null certificate", x5c([]any{nil}), "x5c has no certificate"},
		{"x5c not base64", x5c([]any{"!!!!"}), "x5c certificate is not valid base64"},
		{"x5c not a certificate", x5c([]any{base64.RawStdEncoding.EncodeToString([]byte("nope"))}), "not a valid X.509 certificate"},
		{"no AKI", tngEntry(t, "SN7", noAKI), "no authority key identifier"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := ParseTrustAndGoManifest(manifestJSON(t, tc.entry))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := m.Rows[0].Error; !strings.Contains(got, tc.want) {
				t.Errorf("want error containing %q, got %q", tc.want, got)
			}
			if len(m.Invalid()) != 1 {
				t.Errorf("Invalid: %+v", m.Invalid())
			}
		})
	}
}

func TestParseTrustAndGoManifestDuplicates(t *testing.T) {
	ca := newTestCA(t)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	derA := ca.deviceCert(t, 1, "A", key)
	derB := ca.deviceCert(t, 2, "B", nil)
	derSameKey := ca.deviceCert(t, 3, "C", key)

	m, err := ParseTrustAndGoManifest(manifestJSON(t,
		tngEntry(t, "A", derA),
		tngEntry(t, "A", derB),       // identifier repeats row 1
		tngEntry(t, "B", derA),       // certificate repeats row 1
		tngEntry(t, "C", derSameKey), // public key repeats row 1
		tngEntry(t, "D", derB),       // fine: row 2 failed, so its certificate wasn't taken
	))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"",
		"identifier A is already used by row 1",
		"the certificate is already used by row 1",
		"the certificate's public key is already used by row 1",
		"",
	}
	for i, w := range want {
		if got := m.Rows[i].Error; got != w {
			t.Errorf("row %d: want %q, got %q", i+1, w, got)
		}
	}
}

func TestValidateImportTags(t *testing.T) {
	if err := ValidateImportTags([]string{"a", "b-c"}); err != nil {
		t.Errorf("good tags: %v", err)
	}
	for _, bad := range [][]string{{"a b"}, {"a,b"}, {""}} {
		if err := ValidateImportTags(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestImportDevices(t *testing.T) {
	ca := newTestCA(t)
	data := manifestJSON(t, tngEntry(t, "SN1", ca.deviceCert(t, 1, "SN1", nil)))
	m, err := ParseTrustAndGoManifest(data)
	if err != nil {
		t.Fatal(err)
	}

	var gotMethod, gotPath, gotCT, gotFormat, gotAuth string
	var gotTags []string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotCT = r.Header.Get("Content-Type")
		gotFormat = r.Header.Get("Format")
		gotTags = r.Header.Values("Tags")
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		// DevicesController.bulk_import: send_resp(conn, 201, "").
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	c := newTestClient(t, srv)

	if err := c.ImportDevices(context.Background(), "acme", "thermo stat", m, []string{"batch-7", "eu"}); err != nil {
		t.Fatalf("ImportDevices: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/orgs/acme/products/thermo stat/devices/import" {
		t.Errorf("request: %s %s", gotMethod, gotPath)
	}
	if gotCT != "application/json" || gotFormat != "microchip_trust_and_go" || gotAuth != "Bearer tok-123" {
		t.Errorf("headers: content-type %q, format %q, auth %q", gotCT, gotFormat, gotAuth)
	}
	if len(gotTags) != 1 || gotTags[0] != "batch-7,eu" {
		t.Errorf("tags header: %q", gotTags)
	}
	// The manifest goes as it is, for the server's own parser.
	if string(gotBody) != string(data) {
		t.Errorf("body changed:\n%s", gotBody)
	}

	// Without tags there is no tags header, which the server reads as none.
	if err := c.ImportDevices(context.Background(), "acme", "p", m, nil); err != nil {
		t.Fatal(err)
	}
	if gotTags != nil {
		t.Errorf("tags header without tags: %q", gotTags)
	}
}

func TestImportDevicesErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":{"detail":"Unauthorized"}}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	m := &ImportManifest{Format: DeviceImportFormatTrustAndGo, Raw: []byte(`[]`)}

	var apiErr *APIError
	if err := c.ImportDevices(context.Background(), "acme", "p", m, nil); !errors.As(err, &apiErr) || apiErr.StatusCode != 401 {
		t.Errorf("want 401 APIError, got %v", err)
	}
	if err := c.ImportDevices(context.Background(), "", "p", m, nil); err == nil {
		t.Error("missing org should error")
	}
	if err := c.ImportDevices(context.Background(), "acme", "", m, nil); err == nil {
		t.Error("missing product should error")
	}
	if err := c.ImportDevices(context.Background(), "acme", "p", nil, nil); err == nil {
		t.Error("missing manifest should error")
	}
	if err := c.ImportDevices(context.Background(), "acme", "p", m, []string{"a b"}); err == nil {
		t.Error("a tag with a space should be refused before sending")
	}
	bad := &ImportManifest{Format: DeviceImportFormatTrustAndGo, Raw: []byte(`[1]`), Rows: []ImportRow{{Row: 1, Error: "entry is not a JSON object"}}}
	if err := c.ImportDevices(context.Background(), "acme", "p", bad, nil); err == nil || !strings.Contains(err.Error(), "row 1: entry is not a JSON object") {
		t.Errorf("a manifest with invalid rows should be refused, got %v", err)
	}
}
