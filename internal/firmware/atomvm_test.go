package firmware

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

// --- a packbeam builder, shaped like atomvm_packbeam's output -------------

type etfAtom string

// etf encodes the handful of term types an application spec uses.
func etf(v any) []byte {
	switch v := v.(type) {
	case etfAtom:
		return append([]byte{119, byte(len(v))}, v...)
	case string: // a charlist, as STRING_EXT
		b := []byte{107, 0, 0}
		binary.BigEndian.PutUint16(b[1:], uint16(len(v)))
		return append(b, v...)
	case etfTuple:
		b := []byte{104, byte(len(v))}
		for _, e := range v {
			b = append(b, etf(e)...)
		}
		return b
	case []any:
		if len(v) == 0 {
			return []byte{106}
		}
		b := []byte{108, 0, 0, 0, 0}
		binary.BigEndian.PutUint32(b[1:], uint32(len(v)))
		for _, e := range v {
			b = append(b, etf(e)...)
		}
		return append(b, 106)
	}
	panic("etf: unsupported")
}

func applicationBin(name, vsn, description string) []byte {
	term := append([]byte{131}, etf(etfTuple{
		etfAtom("application"), etfAtom(name), []any{
			etfTuple{etfAtom("description"), description},
			etfTuple{etfAtom("vsn"), vsn},
			etfTuple{etfAtom("modules"), []any{etfAtom(name)}},
		},
	})...)
	out := make([]byte, 4)
	binary.BigEndian.PutUint32(out, uint32(len(term)))
	return append(out, term...)
}

func packbeamEntryBytes(name string, flags uint32, data []byte) []byte {
	e := packbeamDataEntry(name, data)
	binary.BigEndian.PutUint32(e[4:], flags)
	return e
}

// buildPackbeam assembles magic, entries and the "end" terminator.
func buildPackbeam(entries ...[]byte) []byte {
	b := append([]byte{}, atomVMMagic...)
	for _, e := range entries {
		b = append(b, e...)
	}
	return append(b, avmTerminator...)
}

func samplePackbeam() []byte {
	return buildPackbeam(
		packbeamEntryBytes("kiosk.beam", 3, []byte("FOR1....BEAM code")),
		packbeamEntryBytes("kiosk/priv/application.bin", avmDataFlag, applicationBin("kiosk", "1.2", "Smart kiosk")),
		packbeamEntryBytes("dep/priv/application.bin", avmDataFlag, applicationBin("dep", "9.9.9", "A dependency")),
	)
}

// --- the server's verifier, ported from atom_vm.ex verify_signature/2 ------

// serverVerify is AtomVM.verify_signature/2: find the signature entry, take
// everything before its offset as the signed range, and check the payload.
func serverVerify(archive []byte, keys []ed25519.PublicKey) error {
	entries, _, err := packbeamEntries(archive)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name != "nerves_hub/signature" {
			continue
		}
		signed := archive[:e.Offset]
		p := e.Data
		if len(p) != 68 || string(p[:3]) != "NH1" {
			return errors.New("malformed_signature")
		}
		if p[3] != 1 {
			return errors.New("unsupported_signature_version")
		}
		for _, k := range keys {
			if ed25519.Verify(k, signed, p[4:]) {
				return nil
			}
		}
		return errors.New("invalid_signature")
	}
	return errors.New("firmware_not_signed")
}

// deviceByteLength is nh_packbeam:byte_length/1, which the device uses to find
// where an archive ends: the terminator's offset plus its 16 bytes.
func deviceByteLength(archive []byte) int {
	_, term, err := packbeamEntries(archive)
	if err != nil {
		return -1
	}
	return term + 16
}

func newKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func TestSignAtomVMVerifiesLikeTheServer(t *testing.T) {
	pub, priv := newKey(t)
	archive := samplePackbeam()

	signed, replaced, err := SignAtomVM(archive, priv)
	if err != nil {
		t.Fatal(err)
	}
	if replaced {
		t.Error("a fresh archive has no signature to replace")
	}
	if err := serverVerify(signed, []ed25519.PublicKey{pub}); err != nil {
		t.Fatalf("server verification: %v", err)
	}

	// Pure append: nothing before the terminator moved.
	unsignedBody := archive[:len(archive)-len(avmTerminator)]
	if !bytes.HasPrefix(signed, unsignedBody) {
		t.Error("signing must not change the bytes it signs")
	}
	// The entry is the 104 bytes nh_signature writes: 12 header, 24 name,
	// 68 payload; then the 16 byte terminator ends the file.
	if got := len(signed) - len(unsignedBody); got != 104+16 {
		t.Errorf("signing added %d bytes, want 120", got)
	}
	if !bytes.HasSuffix(signed, avmTerminator) {
		t.Error("signed archive must end with the terminator")
	}
	if deviceByteLength(signed) != len(signed) {
		t.Errorf("device would read %d bytes of a %d byte archive", deviceByteLength(signed), len(signed))
	}
	entries, _, _ := packbeamEntries(signed)
	last := entries[len(entries)-1]
	if last.Name != "nerves_hub/signature" || last.Flags != 4 || last.Size != 104 {
		t.Errorf("signature entry = %+v", last)
	}

	// The wrong key, or a flipped byte in the signed range, fails.
	other, _ := newKey(t)
	if err := serverVerify(signed, []ed25519.PublicKey{other}); err == nil {
		t.Error("another key must not verify")
	}
	tampered := append([]byte{}, signed...)
	tampered[40] ^= 1
	if err := serverVerify(tampered, []ed25519.PublicKey{pub}); err == nil {
		t.Error("a tampered archive must not verify")
	}
	if err := serverVerify(archive, []ed25519.PublicKey{pub}); err == nil || err.Error() != "firmware_not_signed" {
		t.Errorf("unsigned archive: %v", err)
	}
}

func TestSignAtomVMReplacesAnExistingSignature(t *testing.T) {
	_, first := newKey(t)
	pub, second := newKey(t)

	once, _, err := SignAtomVM(samplePackbeam(), first)
	if err != nil {
		t.Fatal(err)
	}
	twice, replaced, err := SignAtomVM(once, second)
	if err != nil {
		t.Fatal(err)
	}
	if !replaced {
		t.Error("re-signing should report the replacement")
	}
	if len(twice) != len(once) {
		t.Errorf("re-signing changed the size from %d to %d; signatures must not nest", len(once), len(twice))
	}
	if err := serverVerify(twice, []ed25519.PublicKey{pub}); err != nil {
		t.Errorf("re-signed archive: %v", err)
	}
	if signed, _ := AtomVMSigned(twice); !signed {
		t.Error("AtomVMSigned should see the signature")
	}
}

func TestSignAtomVMRefusesEntriesAfterTheSignature(t *testing.T) {
	_, priv := newKey(t)
	payload := append([]byte("NH1\x01"), make([]byte, 64)...)
	archive := buildPackbeam(
		packbeamEntryBytes("kiosk.beam", 3, []byte("code")),
		packbeamEntryBytes("nerves_hub/signature", avmDataFlag, payload),
		packbeamEntryBytes("late.beam", 3, []byte("unsigned code")),
	)
	if _, _, err := SignAtomVM(archive, priv); err == nil || !strings.Contains(err.Error(), "after its") {
		t.Errorf("want a refusal, got %v", err)
	}
}

// An fwup private key is the seed followed by its public half. Signing uses
// the seed, so a key whose halves disagree still produces a signature for the
// seed's public key rather than one nothing verifies.
func TestSignAtomVMUsesTheSeed(t *testing.T) {
	pub, priv := newKey(t)
	_, other := newKey(t)
	mismatched := append(append(ed25519.PrivateKey{}, priv.Seed()...), other.Public().(ed25519.PublicKey)...)

	signed, _, err := SignAtomVM(samplePackbeam(), mismatched)
	if err != nil {
		t.Fatal(err)
	}
	if err := serverVerify(signed, []ed25519.PublicKey{pub}); err != nil {
		t.Errorf("signature should verify against the seed's key: %v", err)
	}
}

func TestSignAtomVMDropsTrailingBytes(t *testing.T) {
	pub, priv := newKey(t)
	archive := append(samplePackbeam(), "flash padding\xff\xff"...)
	signed, _, err := SignAtomVM(archive, priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := serverVerify(signed, []ed25519.PublicKey{pub}); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(signed, avmTerminator) {
		t.Error("anything after the old terminator should be gone")
	}
}

func TestSignAtomVMRejectsMalformedArchives(t *testing.T) {
	_, priv := newKey(t)
	good := samplePackbeam()
	for name, archive := range map[string][]byte{
		"not a packbeam":  []byte("PK\x03\x04"),
		"no terminator":   good[:len(good)-len(avmTerminator)],
		"truncated entry": good[:len(atomVMMagic)+20],
		"tiny entry":      append(append([]byte{}, atomVMMagic...), 0, 0, 0, 8, 0, 0, 0, 1, 0, 0, 0, 0),
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := SignAtomVM(archive, priv); err == nil {
				t.Error("want an error")
			}
		})
	}
	if _, _, err := SignAtomVM(good, priv[:32]); err == nil {
		t.Error("a 32 byte key should be refused")
	}
}

func TestReadAtomVMMetadata(t *testing.T) {
	meta, warnings, err := ReadAtomVMMetadata(samplePackbeam())
	if err != nil {
		t.Fatal(err)
	}
	// The first application.bin is the project's own, not the dependency's.
	want := Metadata{Product: "kiosk", Version: "1.2.0", Platform: "atomvm", Architecture: "beam", Description: "Smart kiosk"}
	if meta != want {
		t.Errorf("metadata = %+v, want %+v", meta, want)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings: %v", warnings)
	}

	bad := buildPackbeam(packbeamEntryBytes("app/priv/application.bin", avmDataFlag, applicationBin("app", "main", "")))
	meta, warnings, err = ReadAtomVMMetadata(bad)
	if err != nil || meta.Version != "main" || len(warnings) != 1 {
		t.Errorf("non-semver vsn: %+v %v %v", meta, warnings, err)
	}

	none := buildPackbeam(packbeamEntryBytes("app.beam", 3, []byte("code")))
	if _, _, err := ReadAtomVMMetadata(none); err == nil {
		t.Error("an archive with no application.bin should say so")
	}

	// Only <app>/priv/application.bin counts, not any path ending that way.
	deep := buildPackbeam(packbeamEntryBytes("a/b/priv/application.bin", avmDataFlag, applicationBin("x", "1.0.0", "")))
	if _, _, err := ReadAtomVMMetadata(deep); err == nil {
		t.Error("a nested application.bin is not an application spec")
	}
}

func TestDecodeETFRejectsHostileInput(t *testing.T) {
	for name, b := range map[string][]byte{
		"no version byte": {104, 0},
		"huge list":       {131, 108, 0xFF, 0xFF, 0xFF, 0xFF},
		"short binary":    {131, 109, 0, 0, 0, 9, 'a'},
		"unknown tag":     {131, 70, 0, 0, 0, 0, 0, 0, 0, 0},
		"deep nesting":    append([]byte{131}, bytes.Repeat([]byte{104, 1}, 1000)...),
	} {
		if _, _, err := decodeETF(b); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
