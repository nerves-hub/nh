package cmd

import (
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nerves-hub/nh/internal/pki"
)

const lookupDeviceResponse = `{"data":{
	"identifier":"SN0001","connection_status":"connected","version":"1.2.0",
	"org_name":"acme","product_name":"sensor","update_mode":"automatic"
}}`

// lookupServer answers POST .../devices/auth, recording the PEM it was sent.
func lookupServer(t *testing.T, status int, body string, gotPEM *string, gotPath *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotPath = r.Method + " " + r.URL.Path
		var req map[string]string
		_ = json.NewDecoder(r.Body).Decode(&req)
		pemBytes, _ := base64.StdEncoding.DecodeString(req["certificate"])
		*gotPEM = string(pemBytes)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDeviceCertificatesLookup(t *testing.T) {
	keyPEM, certPEM, err := pki.GenerateSelfSignedDeviceCertificate("acme", "SN0001", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// A file holding the key as well: only the certificate may be sent.
	path := filepath.Join(t.TempDir(), "device.pem")
	if err := os.WriteFile(path, append(keyPEM, certPEM...), 0o600); err != nil {
		t.Fatal(err)
	}

	var gotPEM, gotPath string
	srv := lookupServer(t, http.StatusOK, lookupDeviceResponse, &gotPEM, &gotPath)

	out, err := execCmd(t, "",
		"device", "certificates", "lookup", path,
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, out)
	}
	if gotPath != "POST /api/orgs/acme/products/thermostat/devices/auth" {
		t.Errorf("request: %q", gotPath)
	}
	if gotPEM != string(certPEM) {
		t.Errorf("sent PEM should be the certificate alone, got:\n%s", gotPEM)
	}
	if strings.Contains(gotPEM, "PRIVATE KEY") {
		t.Error("the private key was sent")
	}
	cert, _ := pki.ValidateCertificatePEM(certPEM)
	for _, want := range []string{
		"belongs to:", certFingerprint(cert), formatSerial(cert.SerialNumber.String()),
		"Product:", "sensor", "Identifier:", "SN0001", "1.2.0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestDeviceCertificatesLookupDERAndJSON(t *testing.T) {
	_, certPEM, err := pki.GenerateSelfSignedDeviceCertificate("acme", "SN0001", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(certPEM)
	path := filepath.Join(t.TempDir(), "device.der")
	if err := os.WriteFile(path, block.Bytes, 0o600); err != nil {
		t.Fatal(err)
	}

	var gotPEM, gotPath string
	srv := lookupServer(t, http.StatusOK, lookupDeviceResponse, &gotPEM, &gotPath)

	out, err := execCmd(t, "",
		"device", "certs", "lookup", path, "-o", "json",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, out)
	}
	// DER is sent as the PEM the server reads.
	if gotPEM != string(certPEM) {
		t.Errorf("sent PEM:\n%s", gotPEM)
	}
	var device struct {
		Identifier  string `json:"identifier"`
		ProductName string `json:"product_name"`
	}
	if err := json.Unmarshal([]byte(out), &device); err != nil || device.Identifier != "SN0001" || device.ProductName != "sensor" {
		t.Errorf("json output (%v):\n%s", err, out)
	}
}

func TestDeviceCertificatesLookupNotRecognized(t *testing.T) {
	_, certPEM, err := pki.GenerateSelfSignedDeviceCertificate("acme", "SN0001", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "device.pem")
	if err := os.WriteFile(path, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	var gotPEM, gotPath string
	srv := lookupServer(t, http.StatusForbidden, `{"status":"Unauthorized"}`, &gotPEM, &gotPath)

	_, err = execCmd(t, "",
		"device", "certificates", "lookup", path,
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	cert, _ := pki.ValidateCertificatePEM(certPEM)
	want := "no device in acme you can see has this certificate (fingerprint " + certFingerprint(cert) + ")"
	if err == nil || err.Error() != want {
		t.Errorf("want %q, got %v", want, err)
	}
}

func TestDeviceCertificatesLookupBadFile(t *testing.T) {
	dir := t.TempDir()
	keyOnly := filepath.Join(dir, "key.pem")
	keyPEM, _, _ := pki.GenerateSelfSignedDeviceCertificate("acme", "SN0001", time.Hour)
	_ = os.WriteFile(keyOnly, keyPEM, 0o600)
	junk := filepath.Join(dir, "junk")
	_ = os.WriteFile(junk, []byte("hello"), 0o600)

	cases := []struct {
		args []string
		want string
	}{
		{[]string{"device", "certificates", "lookup"}, "Certificate path missing"},
		{[]string{"device", "certificates", "lookup", filepath.Join(dir, "missing.pem")}, "reading certificate"},
		{[]string{"device", "certificates", "lookup", keyOnly}, "no CERTIFICATE block"},
		{[]string{"device", "certificates", "lookup", junk}, "not a PEM or DER certificate"},
	}
	for _, tc := range cases {
		_, err := execCmd(t, "", append(tc.args, "--org", "acme", "--product", "p", "--token", "tok")...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: want error containing %q, got %v", tc.args, tc.want, err)
		}
	}
}
