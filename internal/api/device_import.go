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

package api

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DeviceImportFormatTrustAndGo is the only manifest format
// POST .../devices/import understands: a Microchip Trust&Go manifest. The
// server reads the format from a "format" request header and defaults to this
// one; any other value makes its background job fail without importing
// anything.
const DeviceImportFormatTrustAndGo = "microchip_trust_and_go"

// MaxDeviceImportBytes is the largest manifest NervesHub accepts: its
// Plug.Parsers JSON parser keeps the default 8,000,000-byte body limit and
// answers anything bigger with 413.
const MaxDeviceImportBytes = 8_000_000

// ImportRow is what nh found in one entry of a device import manifest. It
// mirrors how NervesHub reads the entry (NervesHub.Devices.BulkImport), so a
// row without an Error is one the server would try to create.
type ImportRow struct {
	// Row is the entry's 1-based position in the manifest.
	Row int `json:"row"`
	// Identifier is the device identifier, from the entry's header.uniqueId.
	Identifier string `json:"identifier,omitempty"`
	// Certificate is the device certificate the entry carries, when it
	// could be read.
	Certificate *ImportCertificate `json:"certificate,omitempty"`
	// Error says why the server would not create this device, or would
	// abandon the whole import because of it. Empty for a good row.
	Error string `json:"error,omitempty"`
}

// ImportCertificate describes the device certificate found in a manifest
// entry.
type ImportCertificate struct {
	// Serial is the certificate serial as a decimal string, as the API
	// reports serials elsewhere.
	Serial   string    `json:"serial"`
	Subject  string    `json:"subject"`
	NotAfter time.Time `json:"not_after"`
	// Fingerprint is the SHA-1 of the DER in uppercase hex, as NervesHub
	// fingerprints certificates. It refuses a certificate it already has.
	Fingerprint string `json:"fingerprint"`
	// PublicKeyFingerprint is the SHA-1 of the public key's DER in uppercase
	// hex. NervesHub refuses a public key another device already has.
	PublicKeyFingerprint string `json:"public_key_fingerprint"`
}

// ImportManifest is a parsed device import manifest: the bytes to send and
// what each entry holds.
type ImportManifest struct {
	Format string
	Raw    []byte
	Rows   []ImportRow
}

// Invalid returns the rows that have an error.
func (m *ImportManifest) Invalid() []ImportRow {
	var bad []ImportRow
	for _, r := range m.Rows {
		if r.Error != "" {
			bad = append(bad, r)
		}
	}
	return bad
}

// ParseTrustAndGoManifest reads a Microchip Trust&Go manifest and checks every
// entry the way NervesHub will. It returns an error only when the file as a
// whole can't be a manifest; problems with single entries are reported on
// their rows.
//
// The server answers the upload with 201 before it has read a single entry
// and does the work in a background job, so it never reports per-device
// results. Worse, an entry it can't parse (a missing payload, bad base64, no
// certificate) crashes that job, and then no device in the file is created and
// no notification is posted. Checking here first is the only way to learn
// about either.
func ParseTrustAndGoManifest(data []byte) (*ImportManifest, error) {
	if len(data) > MaxDeviceImportBytes {
		return nil, fmt.Errorf("the manifest is %d bytes, and NervesHub accepts at most %d in one import: split it into smaller files",
			len(data), MaxDeviceImportBytes)
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(data, &entries); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			return nil, errors.New("not a Trust&Go manifest: it must be a JSON list of entries")
		}
		return nil, fmt.Errorf("not a Trust&Go manifest: invalid JSON: %w", err)
	}
	if entries == nil {
		return nil, errors.New("not a Trust&Go manifest: it must be a JSON list of entries")
	}
	if len(entries) == 0 {
		return nil, errors.New("the manifest has no entries")
	}

	m := &ImportManifest{Format: DeviceImportFormatTrustAndGo, Raw: data, Rows: make([]ImportRow, len(entries))}
	for i, raw := range entries {
		m.Rows[i] = parseTrustAndGoEntry(i+1, raw)
	}
	markDuplicates(m.Rows)
	return m, nil
}

// parseTrustAndGoEntry reads one manifest entry, following
// NervesHub.Devices.BulkImport.parse_file/2 step by step.
func parseTrustAndGoEntry(n int, raw json.RawMessage) (row ImportRow) {
	row.Row = n
	fail := func(format string, args ...any) ImportRow {
		row.Error = fmt.Sprintf(format, args...)
		return row
	}

	var entry map[string]json.RawMessage
	if !decodeObject(raw, &entry) {
		return fail("entry is not a JSON object")
	}

	// header.uniqueId is read last by the server, but a header that isn't an
	// object (or null) crashes it all the same. Read it first so the row
	// carries its identifier in later messages.
	identifierErr := ""
	if h, ok := entry["header"]; ok && !isNull(h) {
		var header map[string]json.RawMessage
		if !decodeObject(h, &header) {
			return fail("header is not a JSON object")
		}
		if u, ok := header["uniqueId"]; ok && !isNull(u) {
			if err := json.Unmarshal(u, &row.Identifier); err != nil {
				identifierErr = "header.uniqueId is not a string"
			}
		}
	}
	if identifierErr == "" && strings.TrimSpace(row.Identifier) == "" {
		identifierErr = "header.uniqueId (the device identifier) is missing"
	}

	payloadB64, ok := decodeString(entry["payload"])
	if !ok {
		return fail("payload is missing or not a string")
	}
	payload, err := decodeElixirBase64(payloadB64)
	if err != nil {
		return fail("payload is not valid base64")
	}

	var tng map[string]json.RawMessage
	if !decodeObject(payload, &tng) {
		return fail("payload does not decode to a JSON object")
	}
	var keySet map[string]json.RawMessage
	if !decodeObject(tng["publicKeySet"], &keySet) {
		return fail("payload has no publicKeySet object")
	}
	var keys []json.RawMessage
	if k, ok := keySet["keys"]; !ok || json.Unmarshal(k, &keys) != nil || keys == nil {
		return fail("publicKeySet.keys is missing or not a list")
	}

	// The server takes the first key that has an x5c, and gives up on the
	// file if it meets something that isn't an object before finding one.
	var x5c json.RawMessage
	for _, k := range keys {
		var key map[string]json.RawMessage
		if !decodeObject(k, &key) {
			return fail("publicKeySet.keys has an entry that is not an object before the one with the certificate")
		}
		if v, ok := key["x5c"]; ok {
			x5c = v
			break
		}
	}
	if x5c == nil {
		return fail("no key in publicKeySet.keys has an x5c certificate")
	}
	var chain []json.RawMessage
	if json.Unmarshal(x5c, &chain) != nil || chain == nil {
		return fail("x5c is not a list")
	}
	if len(chain) == 0 {
		return fail("x5c has no certificate")
	}
	certB64, ok := decodeString(chain[0])
	if !ok {
		return fail("x5c has no certificate")
	}
	der, err := decodeElixirBase64(certB64)
	if err != nil {
		return fail("the x5c certificate is not valid base64")
	}

	// From here on a problem only skips this device: the rest still import.
	if identifierErr != "" {
		return fail("%s", identifierErr)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return fail("the x5c certificate is not a valid X.509 certificate: %v", err)
	}
	row.Certificate = &ImportCertificate{
		Serial:               cert.SerialNumber.String(),
		Subject:              cert.Subject.String(),
		NotAfter:             cert.NotAfter.UTC(),
		Fingerprint:          CertificateFingerprint(cert.Raw),
		PublicKeyFingerprint: CertificateFingerprint(cert.RawSubjectPublicKeyInfo),
	}
	// NervesHub requires a certificate's authority key identifier, to find
	// the signer CA by.
	if len(cert.AuthorityKeyId) == 0 {
		return fail("the certificate has no authority key identifier, which NervesHub requires")
	}
	return row
}

// markDuplicates flags rows that repeat an earlier row's identifier,
// certificate or public key. The server creates the first and refuses the
// rest.
func markDuplicates(rows []ImportRow) {
	ids := map[string]int{}
	fps := map[string]int{}
	pks := map[string]int{}
	for i := range rows {
		r := &rows[i]
		if r.Error != "" {
			continue
		}
		c := r.Certificate
		if first, ok := ids[r.Identifier]; ok {
			r.Error = fmt.Sprintf("identifier %s is already used by row %d", r.Identifier, first)
			continue
		}
		if first, ok := fps[c.Fingerprint]; ok {
			r.Error = fmt.Sprintf("the certificate is already used by row %d", first)
			continue
		}
		if first, ok := pks[c.PublicKeyFingerprint]; ok {
			r.Error = fmt.Sprintf("the certificate's public key is already used by row %d", first)
			continue
		}
		ids[r.Identifier] = r.Row
		fps[c.Fingerprint] = r.Row
		pks[c.PublicKeyFingerprint] = r.Row
	}
}

// CertificateFingerprint returns the SHA-1 of der in uppercase hex, as
// NervesHub fingerprints certificates and public keys
// (NervesHub.Certificate.fingerprint/1).
func CertificateFingerprint(der []byte) string {
	sum := sha1.Sum(der)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// decodeElixirBase64 decodes base64 as Elixir's Base.decode64(s, padding:
// false) does: the standard alphabet, padding optional, and nothing else, not
// even line breaks (which Go's decoder would skip).
func decodeElixirBase64(s string) ([]byte, error) {
	if strings.ContainsAny(s, "\r\n") {
		return nil, errors.New("base64 contains a line break")
	}
	if len(s)%4 == 0 {
		return base64.StdEncoding.DecodeString(s)
	}
	return base64.RawStdEncoding.DecodeString(s)
}

// decodeObject unmarshals raw into a JSON object, reporting false for null,
// anything that isn't an object, and missing input.
func decodeObject(raw []byte, out *map[string]json.RawMessage) bool {
	if len(raw) == 0 || isNull(raw) {
		return false
	}
	return json.Unmarshal(raw, out) == nil && *out != nil
}

// decodeString unmarshals raw as a JSON string, reporting false for null
// (which Go would decode as "") and anything else that isn't a string.
func decodeString(raw []byte) (string, bool) {
	var s string
	if len(raw) == 0 || isNull(raw) || json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

func isNull(raw []byte) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// ValidateImportTags checks tags for the import's "tags" header. The server
// splits the header on commas and refuses a tag with a space in it, and then
// it refuses every device in the file.
func ValidateImportTags(tags []string) error {
	for _, t := range tags {
		if strings.ContainsAny(t, ", \t\r\n") {
			return fmt.Errorf("tag %q: a tag cannot contain spaces or commas", t)
		}
		if t == "" {
			return errors.New("a tag cannot be empty")
		}
	}
	return nil
}

// ImportDevices sends a device import manifest via
// POST /orgs/{org}/products/{product}/devices/import. The body is the
// manifest as it is, with its format in a "format" header and any tags for
// the new devices, comma-separated, in a "tags" header.
//
// The server answers 201 with an empty body once it has queued the import,
// not 204 as its OpenAPI spec says, and it never reports per-device results:
// it creates the devices in the background and posts a notification on the
// product with how many it created and how many failed.
func (c *Client) ImportDevices(ctx context.Context, org, product string, m *ImportManifest, tags []string) error {
	if org == "" {
		return errors.New("api: org is required to import devices")
	}
	if product == "" {
		return errors.New("api: product is required to import devices")
	}
	if m == nil || len(m.Raw) == 0 {
		return errors.New("api: a manifest is required to import devices")
	}
	// The server drops the whole file over one bad entry, without saying so.
	if bad := m.Invalid(); len(bad) > 0 {
		return fmt.Errorf("api: manifest row %d: %s", bad[0].Row, bad[0].Error)
	}
	if err := ValidateImportTags(tags); err != nil {
		return fmt.Errorf("api: %w", err)
	}

	path := "/orgs/" + url.PathEscape(org) + "/products/" + url.PathEscape(product) + "/devices/import"
	endpoint, err := c.resolve(path, nil)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(m.Raw))
	if err != nil {
		return fmt.Errorf("api: building request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Format", m.Format)
	if len(tags) > 0 {
		req.Header.Set("Tags", strings.Join(tags, ","))
	}
	if c.token != "" {
		req.Header.Set("Authorization", authScheme+" "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("api: POST %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseError(resp)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
