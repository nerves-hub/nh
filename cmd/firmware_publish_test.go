package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// stubFwupMeta replaces fwup with a script that answers `-m -i <file>` with the
// given metadata, and otherwise signs (copying input to output) like the real
// thing. Both modes are needed: upload reads metadata and then signs.
func stubFwupMeta(t *testing.T, meta map[string]string) {
	t.Helper()
	dir := t.TempDir()

	var lines []string
	for k, v := range meta {
		lines = append(lines, fmt.Sprintf(`meta-%s="%s"`, k, v))
	}
	sort.Strings(lines)

	script := "#!/bin/sh\n" +
		"mode=sign\nin=\"\"\nout=\"\"\n" +
		"while [ $# -gt 0 ]; do\n" +
		"  case \"$1\" in\n" +
		"    -m) mode=meta; shift ;;\n" +
		"    -i) in=\"$2\"; shift 2 ;;\n" +
		"    -o) out=\"$2\"; shift 2 ;;\n" +
		"    *) shift ;;\n" +
		"  esac\n" +
		"done\n" +
		"if [ \"$mode\" = meta ]; then\n" +
		"  cat <<'META'\n" + strings.Join(lines, "\n") + "\nMETA\n" +
		"  exit 0\n" +
		"fi\n" +
		`cat "$in" > "$out"` + "\n" +
		`printf 'SIGNED' >> "$out"` + "\n"

	path := filepath.Join(dir, "fwup")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	old := fwupBin
	fwupBin = path
	t.Cleanup(func() { fwupBin = old })
}

// publishServer accepts a firmware upload and records any deployment updates.
func publishServer(t *testing.T) (srv *httptest.Server, deployed *[]string) {
	t.Helper()
	var mu sync.Mutex
	seen := []string{}
	deployed = &seen

	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/firmwares"):
			_, _ = w.Write([]byte(`{"data":{"uuid":"new-uuid","version":"1.4.0"}}`))
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/deployments/"):
			mu.Lock()
			seen = append(seen, filepath.Base(r.URL.Path))
			*deployed = seen
			mu.Unlock()
			_, _ = w.Write([]byte(`{"data":{"name":"x"}}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, deployed
}

// nervesProject builds a Nerves layout in a temp cwd holding one image.
func nervesProject(t *testing.T, target, env string) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "mix.exs"), []byte("# mix"), 0o644); err != nil {
		t.Fatal(err)
	}
	images := filepath.Join(dir, "_build", target+"_"+env, "nerves", "images")
	if err := os.MkdirAll(images, 0o755); err != nil {
		t.Fatal(err)
	}
	fw := filepath.Join(images, "app.fw")
	if err := os.WriteFile(fw, []byte("FWDATA"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MIX_TARGET", target)
	t.Setenv("MIX_ENV", env)
	return fw
}

func writeFW(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "image.fw")
	if err := os.WriteFile(p, []byte("FWDATA"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFirmwarePublishAliasWorks(t *testing.T) {
	stubFwupMeta(t, map[string]string{"product": "thermostat", "version": "1.4.0"})
	srv, _ := publishServer(t)

	out, err := execCmd(t, "",
		"firmware", "publish", writeFW(t),
		"--skip-signing",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("publish alias: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Uploaded firmware") {
		t.Errorf("expected an upload confirmation, got:\n%s", out)
	}
}

func TestFirmwareUploadDeploysToGroups(t *testing.T) {
	stubFwupMeta(t, map[string]string{"product": "thermostat", "version": "1.4.0"})
	srv, deployed := publishServer(t)

	out, err := execCmd(t, "",
		"firmware", "upload", writeFW(t),
		"--skip-signing",
		"--deploy", "canary", "--deploy", "production",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("upload --deploy: %v\n%s", err, out)
	}

	got := *deployed
	sort.Strings(got)
	if len(got) != 2 || got[0] != "canary" || got[1] != "production" {
		t.Errorf("deployment groups updated = %v, want [canary production]", got)
	}
	for _, want := range []string{"Updated deployment group canary", "Updated deployment group production"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got:\n%s", want, out)
		}
	}
}

func TestFirmwareUploadDeployFromEnv(t *testing.T) {
	stubFwupMeta(t, map[string]string{"product": "thermostat"})
	srv, deployed := publishServer(t)
	t.Setenv("NERVES_HUB_DEPLOYMENT_NAME", "from-env")

	if _, err := execCmd(t, "",
		"firmware", "upload", writeFW(t),
		"--skip-signing",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if got := *deployed; len(got) != 1 || got[0] != "from-env" {
		t.Errorf("deployed = %v, want [from-env]", got)
	}
}

func TestFirmwareUploadFlagBeatsDeployEnv(t *testing.T) {
	stubFwupMeta(t, map[string]string{"product": "thermostat"})
	srv, deployed := publishServer(t)
	t.Setenv("NERVES_HUB_DEPLOYMENT_NAME", "from-env")

	if _, err := execCmd(t, "",
		"firmware", "upload", writeFW(t),
		"--skip-signing", "--deploy", "explicit",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if got := *deployed; len(got) != 1 || got[0] != "explicit" {
		t.Errorf("deployed = %v, want [explicit] (--deploy must win over the env)", got)
	}
}

func TestFirmwareUploadRejectsProductMismatch(t *testing.T) {
	stubFwupMeta(t, map[string]string{"product": "doorbell", "version": "1.4.0"})
	uploaded := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		uploaded = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"uuid":"u"}}`))
	}))
	defer srv.Close()

	_, err := execCmd(t, "",
		"firmware", "upload", writeFW(t),
		"--skip-signing",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "built for product") {
		t.Fatalf("expected a product mismatch error, got %v", err)
	}
	if uploaded {
		t.Error("nothing should be uploaded when the product does not match")
	}
}

func TestFirmwareUploadDetectsFirmwareInProject(t *testing.T) {
	stubFwupMeta(t, map[string]string{"product": "thermostat", "version": "1.4.0"})
	nervesProject(t, "rpi4", "dev")
	srv, _ := publishServer(t)

	out, err := execCmd(t, "",
		"firmware", "upload", // no path
		"--skip-signing", "--yes",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err != nil {
		t.Fatalf("detected upload: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Detected firmware:") || !strings.Contains(out, "app.fw") {
		t.Errorf("expected the detected path to be reported, got:\n%s", out)
	}
}

// An auto-detected path is a guess, so it must not be uploaded unattended
// without --yes.
func TestFirmwareUploadDetectedRequiresConfirmation(t *testing.T) {
	stubFwupMeta(t, map[string]string{"product": "thermostat"})
	nervesProject(t, "rpi4", "dev")
	uploaded := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		uploaded = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	_, err := execCmd(t, "",
		"firmware", "upload",
		"--skip-signing", "--non-interactive",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err == nil || !strings.Contains(err.Error(), "without confirmation") {
		t.Fatalf("expected a confirmation refusal, got %v", err)
	}
	if uploaded {
		t.Error("nothing should be uploaded when confirmation was refused")
	}
}

// A deployment failure must not read as an upload failure.
func TestFirmwareUploadDeployFailureSaysUploadSucceeded(t *testing.T) {
	stubFwupMeta(t, map[string]string{"product": "thermostat"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"data":{"uuid":"new-uuid","version":"1.4.0"}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":{"detail":"Not Found"}}`))
	}))
	defer srv.Close()

	_, err := execCmd(t, "",
		"firmware", "upload", writeFW(t),
		"--skip-signing", "--deploy", "nope",
		"--org", "acme", "--product", "thermostat",
		"--uri", srv.URL, "--token", "tok",
	)
	if err == nil {
		t.Fatal("expected an error when the deployment update fails")
	}
	for _, want := range []string{"uploaded", "nope"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q so the upload is not retried, got %v", want, err)
		}
	}
}
