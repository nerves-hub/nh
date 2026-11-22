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

package firmware

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// A RAUC bundle is a SquashFS payload, a CMS signature, and an 8 byte
// big-endian footer giving the signature's length. In the verity format the
// manifest is the CMS's encapsulated content. See
// lib/nerves_hub/firmwares/update_tool/rauc.ex.
//
// The server extracts the manifest with `openssl cms -verify -noverify`; nh
// reads the encapsulated content straight out of the CMS structure instead, so
// previewing a bundle needs no openssl. The signature itself is not checked
// here — that is the server's job, against the organization's certificates.

const (
	raucFooterSize       = 8
	raucMaxSignatureSize = 64 * 1024 * 1024
)

// ReadRAUCMetadata reads the manifest out of a bundle's signature and maps it
// to NervesHub's fields as Rauc.get_firmware_metadata_from_file/1 does.
func ReadRAUCMetadata(path string) (Metadata, []string, error) {
	sig, err := readRAUCSignature(path)
	if err != nil {
		return Metadata{}, nil, err
	}
	manifest, err := cmsContent(sig)
	if err != nil {
		return Metadata{}, nil, err
	}
	if len(bytes.TrimSpace(manifest)) == 0 {
		return Metadata{}, nil, errors.New("plain RAUC bundles are not supported; build a verity bundle")
	}
	return raucMetadata(manifest)
}

func readRAUCSignature(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	if size <= raucFooterSize {
		return nil, errors.New("not a RAUC bundle: too short")
	}
	footer := make([]byte, raucFooterSize)
	if _, err := f.ReadAt(footer, size-raucFooterSize); err != nil {
		return nil, err
	}
	sigSize := binary.BigEndian.Uint64(footer)
	switch {
	case sigSize == 0:
		return nil, errors.New("RAUC bundle has no signature")
	case sigSize > uint64(size-raucFooterSize):
		return nil, errors.New("RAUC bundle footer gives a signature larger than the bundle")
	case sigSize > raucMaxSignatureSize:
		return nil, errors.New("RAUC bundle footer gives an implausible signature size")
	}
	sig := make([]byte, sigSize)
	if _, err := f.ReadAt(sig, size-raucFooterSize-int64(sigSize)); err != nil {
		return nil, err
	}
	return sig, nil
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func raucMetadata(manifest []byte) (Metadata, []string, error) {
	sections := parseManifest(string(manifest))
	update := sections["update"]
	ours, haveOurs := sections["meta.nerveshub"]

	compatible := update["compatible"]
	if compatible == "" {
		return Metadata{}, nil, errors.New("RAUC manifest has no [update] compatible")
	}
	version := update["version"]
	if version == "" {
		return Metadata{}, nil, errors.New("RAUC manifest has no [update] version")
	}
	if !haveOurs || len(ours) == 0 {
		return Metadata{}, nil, errors.New("RAUC manifest has no [meta.nerveshub] section " +
			"(RAUC before 1.9 drops it while signing; build with RAUC 1.9 or newer)")
	}
	architecture := ours["architecture"]
	if architecture == "" {
		return Metadata{}, nil, errors.New("RAUC manifest has no [meta.nerveshub] architecture")
	}

	uuid := ours["uuid"]
	switch {
	case uuid == "":
		sum := sha256.Sum256(manifest)
		uuid = uuidFromDigest(sum[:])
	case !uuidPattern.MatchString(uuid):
		return Metadata{}, nil, fmt.Errorf("RAUC manifest [meta.nerveshub] uuid %q is not a UUID", uuid)
	default:
		uuid = strings.ToLower(uuid)
	}

	return Metadata{
		Product:      orDefault(ours["product"], compatible),
		Version:      version,
		Platform:     orDefault(ours["platform"], compatible),
		Architecture: architecture,
		UUID:         uuid,
		Description:  update["description"],
	}, nil, nil
}

func orDefault(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

// parseManifest parses GLib key file syntax into section -> key -> value, last
// value winning, as Rauc.parse_manifest/1 does.
func parseManifest(manifest string) map[string]map[string]string {
	sections := map[string]map[string]string{}
	section := ""
	inSection := false
	for _, line := range strings.Split(strings.ReplaceAll(manifest, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") && len(line) >= 2:
			section = strings.TrimSpace(line[1 : len(line)-1])
			inSection = true
			if sections[section] == nil {
				sections[section] = map[string]string{}
			}
		case inSection:
			if k, v, ok := strings.Cut(line, "="); ok {
				sections[section][strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
		}
	}
	return sections
}

// cmsContent returns the encapsulated content of a CMS SignedData structure
// (ContentInfo -> SignedData -> EncapsulatedContentInfo -> eContent), without
// verifying anything. It reads BER, since a CMS may use indefinite lengths and
// a constructed OCTET STRING.
func cmsContent(der []byte) ([]byte, error) {
	bad := func(what string) error { return fmt.Errorf("RAUC signature is not a CMS SignedData: %s", what) }

	ci, _, err := berNext(der)
	if err != nil || ci.tag != 0x30 {
		return nil, bad("no ContentInfo")
	}
	oid, rest, err := berNext(ci.content)
	if err != nil || oid.tag != 0x06 || !bytes.Equal(oid.content, oidSignedData) {
		return nil, bad("wrong content type")
	}
	explicit, _, err := berNext(rest)
	if err != nil || explicit.tag != 0xA0 {
		return nil, bad("no content")
	}
	sd, _, err := berNext(explicit.content)
	if err != nil || sd.tag != 0x30 {
		return nil, bad("no SignedData")
	}
	// version INTEGER, digestAlgorithms SET, then encapContentInfo.
	_, rest, err = berNext(sd.content)
	if err != nil {
		return nil, bad("truncated")
	}
	_, rest, err = berNext(rest)
	if err != nil {
		return nil, bad("truncated")
	}
	eci, _, err := berNext(rest)
	if err != nil || eci.tag != 0x30 {
		return nil, bad("no EncapsulatedContentInfo")
	}
	_, rest, err = berNext(eci.content)
	if err != nil {
		return nil, bad("truncated")
	}
	if len(rest) == 0 {
		// A detached signature: the plain bundle format.
		return nil, errors.New("plain RAUC bundles are not supported; build a verity bundle")
	}
	econtent, _, err := berNext(rest)
	if err != nil || econtent.tag != 0xA0 {
		return nil, bad("no eContent")
	}
	octets, _, err := berNext(econtent.content)
	if err != nil {
		return nil, bad("no eContent")
	}
	return berOctets(octets, 0)
}

// oidSignedData is the DER body of 1.2.840.113549.1.7.2.
var oidSignedData = []byte{0x2A, 0x86, 0x48, 0x86, 0xF7, 0x0D, 0x01, 0x07, 0x02}

type berTLV struct {
	tag     byte
	content []byte
}

var errBER = errors.New("malformed BER")

// berNext reads one TLV off the front of b. Only low tag numbers are handled,
// which is all CMS uses. For an indefinite length the content excludes the
// end-of-contents marker.
func berNext(b []byte) (berTLV, []byte, error) { return berNextDepth(b, 0) }

func berNextDepth(b []byte, depth int) (berTLV, []byte, error) {
	if len(b) < 2 || b[0]&0x1F == 0x1F {
		return berTLV{}, nil, errBER
	}
	tag := b[0]
	l := int(b[1])
	b = b[2:]
	switch {
	case l < 0x80:
	case l == 0x80:
		if tag&0x20 == 0 {
			return berTLV{}, nil, errBER
		}
		end, err := berIndefiniteEnd(b, depth)
		if err != nil {
			return berTLV{}, nil, err
		}
		return berTLV{tag: tag, content: b[:end]}, b[end+2:], nil
	default:
		n := l & 0x7F
		if n > 4 || len(b) < n {
			return berTLV{}, nil, errBER
		}
		l = 0
		for _, c := range b[:n] {
			l = l<<8 | int(c)
		}
		b = b[n:]
	}
	if l < 0 || l > len(b) {
		return berTLV{}, nil, errBER
	}
	return berTLV{tag: tag, content: b[:l]}, b[l:], nil
}

// berIndefiniteEnd returns the offset of the end-of-contents marker that closes
// an indefinite-length value whose content starts at b.
func berIndefiniteEnd(b []byte, depth int) (int, error) {
	if depth > 32 {
		return 0, errBER
	}
	off := 0
	for {
		if len(b)-off >= 2 && b[off] == 0 && b[off+1] == 0 {
			return off, nil
		}
		_, rest, err := berNextDepth(b[off:], depth+1)
		if err != nil {
			return 0, err
		}
		off = len(b) - len(rest)
	}
}

// berOctets flattens an OCTET STRING, primitive or constructed.
func berOctets(t berTLV, depth int) ([]byte, error) {
	switch t.tag {
	case 0x04:
		return t.content, nil
	case 0x24:
		if depth > 32 {
			return nil, errBER
		}
		var out []byte
		rest := t.content
		for len(rest) > 0 {
			var part berTLV
			var err error
			part, rest, err = berNext(rest)
			if err != nil {
				return nil, err
			}
			chunk, err := berOctets(part, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, chunk...)
		}
		return out, nil
	}
	return nil, errBER
}
