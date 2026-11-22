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
	"crypto/ed25519"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"
)

// An AtomVM packbeam is the 24 byte magic followed by entries until a zeroed
// size or flags word. Each entry is a 12 byte header (size, flags, reserved,
// all big-endian; size covers the whole entry), a NUL terminated name padded to
// 4 bytes, then the data. See lib/nerves_hub/firmwares/update_tool/atom_vm.ex
// and nh_packbeam.erl in nerves_hub_link_atomvm_esp32.

const (
	packbeamHeaderSize = 12

	// avmSignatureEntry is the data entry a NervesHub signature travels in.
	avmSignatureEntry = "nerves_hub/signature"
	// avmSignatureVersion is the payload version: <<"NH1", 1, Sig:64>>.
	avmSignatureVersion = 1
	// avmDataFlag is the flag atomvm_packbeam gives a plain data file.
	avmDataFlag = 4

	// maxApplicationEntrySize caps the application spec the decoder is handed,
	// as the server does.
	maxApplicationEntrySize = 64 * 1024

	avmPlatform     = "atomvm"
	avmArchitecture = "beam"
)

// avmTerminator is what nh_signature writes after the last entry: a zeroed
// header and the "end\0" name.
var avmTerminator = append(make([]byte, packbeamHeaderSize), "end\x00"...)

// packbeamEntry is one entry of a packbeam archive.
type packbeamEntry struct {
	Name   string
	Flags  uint32
	Data   []byte
	Offset int // where the entry's header begins, from the start of the file
	Size   int // the whole entry, header included
}

// packbeamEntries walks a packbeam the way the server's AtomVM.entries/1 does,
// returning every entry in file order and the offset at which the terminator
// begins.
func packbeamEntries(archive []byte) ([]packbeamEntry, int, error) {
	if !bytes.HasPrefix(archive, atomVMMagic) {
		return nil, 0, errors.New("not an AtomVM packbeam")
	}
	offset := len(atomVMMagic)
	var entries []packbeamEntry
	for {
		rest := archive[offset:]
		// A zero size, or a zero flags word, terminates the archive.
		if len(rest) >= 4 && binary.BigEndian.Uint32(rest) == 0 {
			return entries, offset, nil
		}
		if len(rest) >= 8 && binary.BigEndian.Uint32(rest[4:]) == 0 {
			return entries, offset, nil
		}
		if len(rest) < packbeamHeaderSize {
			return nil, 0, errors.New("malformed packbeam: archive ends without a terminator")
		}
		size := int(binary.BigEndian.Uint32(rest))
		if size <= packbeamHeaderSize {
			return nil, 0, fmt.Errorf("malformed packbeam: entry at byte %d is smaller than its header", offset)
		}
		if size > len(rest) {
			return nil, 0, fmt.Errorf("truncated packbeam: entry at byte %d runs past the end of the file", offset)
		}
		body := rest[packbeamHeaderSize:size]
		nul := bytes.IndexByte(body, 0)
		if nul < 0 {
			return nil, 0, fmt.Errorf("malformed packbeam: entry at byte %d has an unterminated name", offset)
		}
		padded := (nul + 1 + 3) / 4 * 4
		if len(body) < padded {
			return nil, 0, fmt.Errorf("malformed packbeam: entry at byte %d is smaller than its name", offset)
		}
		entries = append(entries, packbeamEntry{
			Name:   string(body[:nul]),
			Flags:  binary.BigEndian.Uint32(rest[4:]),
			Data:   body[padded:],
			Offset: offset,
			Size:   size,
		})
		offset += size
	}
}

// AtomVMSigned reports whether a packbeam already carries a NervesHub
// signature entry.
func AtomVMSigned(archive []byte) (bool, error) {
	entries, _, err := packbeamEntries(archive)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.Name == avmSignatureEntry {
			return true, nil
		}
	}
	return false, nil
}

// SignAtomVM signs a packbeam with an Ed25519 key — the organization's fwup
// key — returning the signed archive.
//
// The signature is appended as a "nerves_hub/signature" data entry holding
// <<"NH1", 1, Signature:64>>, followed by a fresh terminator. The signed range
// is every byte before that entry begins, which is what the server and the
// device verify. This is the same output as nh_signature:sign/2.
//
// An archive that is already signed has its signature replaced, so re-signing
// after a key rotation is the same operation; replaced reports that. An
// archive with entries after its signature is refused, because replacing the
// signature would silently drop them.
func SignAtomVM(archive []byte, priv ed25519.PrivateKey) (signed []byte, replaced bool, err error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, false, fmt.Errorf("signing key is %d bytes, want an Ed25519 private key", len(priv))
	}
	entries, terminator, err := packbeamEntries(archive)
	if err != nil {
		return nil, false, err
	}

	end := terminator
	for i, e := range entries {
		if e.Name != avmSignatureEntry {
			continue
		}
		if i != len(entries)-1 {
			return nil, false, fmt.Errorf("packbeam has entries after its %s entry; "+
				"rebuild it rather than re-signing", avmSignatureEntry)
		}
		end = e.Offset
		replaced = true
		break
	}

	// Sign with the key derived from the seed alone. An fwup private key is the
	// seed followed by its public half, and a mismatched half would otherwise
	// produce a signature that verifies against nothing.
	prefix := archive[:end]
	sig := ed25519.Sign(ed25519.NewKeyFromSeed(priv.Seed()), prefix)
	payload := append([]byte{'N', 'H', '1', avmSignatureVersion}, sig...)

	out := make([]byte, 0, len(prefix)+128)
	out = append(out, prefix...)
	out = append(out, packbeamDataEntry(avmSignatureEntry, payload)...)
	out = append(out, avmTerminator...)
	return out, replaced, nil
}

// SignAtomVMFile signs the packbeam at in with SignAtomVM, writing the signed
// archive to out.
func SignAtomVMFile(in, out string, priv ed25519.PrivateKey) (replaced bool, err error) {
	archive, err := os.ReadFile(in)
	if err != nil {
		return false, err
	}
	signed, replaced, err := SignAtomVM(archive, priv)
	if err != nil {
		return false, err
	}
	return replaced, os.WriteFile(out, signed, 0o600)
}

// packbeamDataEntry builds one entry the way packbeam_api:pack_data/1 does.
func packbeamDataEntry(name string, data []byte) []byte {
	nameField := pad4(append([]byte(name), 0))
	padded := pad4(data)
	size := packbeamHeaderSize + len(nameField) + len(padded)

	entry := make([]byte, packbeamHeaderSize, size)
	binary.BigEndian.PutUint32(entry[0:], uint32(size))
	binary.BigEndian.PutUint32(entry[4:], avmDataFlag)
	entry = append(entry, nameField...)
	return append(entry, padded...)
}

func pad4(b []byte) []byte {
	if r := len(b) % 4; r != 0 {
		return append(b, make([]byte, 4-r)...)
	}
	return b
}

// ReadAtomVMMetadata reads the OTP application spec a packbeam carries in
// <app>/priv/application.bin, as the server's
// AtomVM.get_firmware_metadata_from_file/1 does.
//
// UUID is left empty: the server derives it from the SHA-256 of the archive as
// uploaded, which signing changes.
func ReadAtomVMMetadata(archive []byte) (Metadata, []string, error) {
	entries, _, err := packbeamEntries(archive)
	if err != nil {
		return Metadata{}, nil, err
	}

	var app *packbeamEntry
	for i := range entries {
		parts := strings.Split(entries[i].Name, "/")
		if len(parts) == 3 && parts[1] == "priv" && parts[2] == "application.bin" {
			app = &entries[i]
			break
		}
	}
	if app == nil {
		return Metadata{}, nil, errors.New("packbeam has no <app>/priv/application.bin entry")
	}
	if len(app.Data) > maxApplicationEntrySize {
		return Metadata{}, nil, errors.New("application metadata is too large")
	}
	if len(app.Data) < 4 || int(binary.BigEndian.Uint32(app.Data)) > len(app.Data)-4 {
		return Metadata{}, nil, errors.New("malformed application metadata")
	}
	termBytes := app.Data[4 : 4+binary.BigEndian.Uint32(app.Data)]

	term, _, err := decodeETF(termBytes)
	if err != nil {
		return Metadata{}, nil, fmt.Errorf("malformed application metadata: %w", err)
	}
	tuple, ok := term.(etfTuple)
	if !ok || len(tuple) != 3 || tuple[0] != "application" {
		return Metadata{}, nil, errors.New("malformed application metadata")
	}
	name, ok := tuple[1].(string)
	props, ok2 := tuple[2].([]any)
	if !ok || !ok2 {
		return Metadata{}, nil, errors.New("malformed application metadata")
	}

	raw := etfProperty(props, "vsn")
	if raw == "" {
		return Metadata{}, nil, errors.New("application metadata has no vsn")
	}
	var warnings []string
	version, ok := normaliseAtomVMVersion(raw)
	if !ok {
		version = raw
		warnings = append(warnings, fmt.Sprintf("version %q is not a semantic version; the server will reject it", raw))
	}

	return Metadata{
		Product:      name,
		Version:      version,
		Platform:     avmPlatform,
		Architecture: avmArchitecture,
		Description:  etfProperty(props, "description"),
	}, warnings, nil
}

func etfProperty(props []any, key string) string {
	for _, p := range props {
		if t, ok := p.(etfTuple); ok && len(t) == 2 && t[0] == key {
			if s, ok := t[1].(string); ok {
				return s
			}
		}
	}
	return ""
}

// etfTuple is a decoded Erlang tuple.
type etfTuple []any

// decodeETF decodes the subset of Erlang's external term format an
// application spec uses, returning atoms, charlists and binaries as strings.
// It ports AtomVM.decode_term/1.
func decodeETF(b []byte) (any, []byte, error) {
	if len(b) == 0 || b[0] != 131 {
		return nil, nil, errors.New("not an external term")
	}
	return etfTerm(b[1:], 0)
}

var errETF = errors.New("unsupported or truncated term")

func etfTerm(b []byte, depth int) (any, []byte, error) {
	if len(b) == 0 || depth > 64 {
		return nil, nil, errETF
	}
	tag, b := b[0], b[1:]
	switch tag {
	case 97: // SMALL_INTEGER_EXT
		if len(b) < 1 {
			return nil, nil, errETF
		}
		return int(b[0]), b[1:], nil
	case 98: // INTEGER_EXT
		if len(b) < 4 {
			return nil, nil, errETF
		}
		return int(int32(binary.BigEndian.Uint32(b))), b[4:], nil
	case 100, 118, 107: // ATOM_EXT, ATOM_UTF8_EXT, STRING_EXT
		if len(b) < 2 {
			return nil, nil, errETF
		}
		n := int(binary.BigEndian.Uint16(b))
		if len(b) < 2+n {
			return nil, nil, errETF
		}
		return string(b[2 : 2+n]), b[2+n:], nil
	case 119: // SMALL_ATOM_UTF8_EXT
		if len(b) < 1 || len(b) < 1+int(b[0]) {
			return nil, nil, errETF
		}
		n := int(b[0])
		return string(b[1 : 1+n]), b[1+n:], nil
	case 106: // NIL_EXT
		return []any{}, b, nil
	case 109: // BINARY_EXT
		if len(b) < 4 {
			return nil, nil, errETF
		}
		n := binary.BigEndian.Uint32(b)
		if uint64(len(b)-4) < uint64(n) {
			return nil, nil, errETF
		}
		return string(b[4 : 4+n]), b[4+n:], nil
	case 104: // SMALL_TUPLE_EXT
		if len(b) < 1 {
			return nil, nil, errETF
		}
		items, rest, err := etfElements(int(b[0]), b[1:], depth)
		return etfTuple(items), rest, err
	case 105: // LARGE_TUPLE_EXT
		if len(b) < 4 {
			return nil, nil, errETF
		}
		items, rest, err := etfElements(int(binary.BigEndian.Uint32(b)), b[4:], depth)
		return etfTuple(items), rest, err
	case 108: // LIST_EXT
		if len(b) < 4 {
			return nil, nil, errETF
		}
		items, rest, err := etfElements(int(binary.BigEndian.Uint32(b)), b[4:], depth)
		if err != nil {
			return nil, nil, err
		}
		// A proper list ends in NIL_EXT, which is a term like any other.
		_, rest, err = etfTerm(rest, depth+1)
		return items, rest, err
	}
	return nil, nil, errETF
}

func etfElements(count int, b []byte, depth int) ([]any, []byte, error) {
	// Every element is at least one byte, which bounds a crafted count.
	if count < 0 || count > len(b) {
		return nil, nil, errETF
	}
	items := make([]any, 0, count)
	for i := 0; i < count; i++ {
		item, rest, err := etfTerm(b, depth+1)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, item)
		b = rest
	}
	return items, b, nil
}
