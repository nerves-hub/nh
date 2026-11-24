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
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ESP-IDF application images: esp_image_header_t, the first segment header,
// then esp_app_desc_t at a fixed 0x20. A Secure Boot v2 signature is a 4 KB
// sector appended to the image with the signature block at its start. See
// lib/nerves_hub/firmwares/update_tool/esp_idf.ex.

const (
	espSigBlockMagic  = 0xE7
	espSigBlockSize   = 1216
	espSigBlockCRCLen = 1196
	espSigSectorSize  = 4096
)

// espChips maps esp_chip_id_t to {platform, architecture}, as the server does.
var espChips = map[uint16][2]string{
	0x0000: {"esp32", "xtensa"},
	0x0002: {"esp32s2", "xtensa"},
	0x0005: {"esp32c3", "riscv"},
	0x0009: {"esp32s3", "xtensa"},
	0x000C: {"esp32c2", "riscv"},
	0x000D: {"esp32c6", "riscv"},
	0x0010: {"esp32h2", "riscv"},
	0x0012: {"esp32p4", "riscv"},
	0x0017: {"esp32c5", "riscv"},
}

// ReadESPIDFMetadata reads the image header and app descriptor from the file
// at path, as EspIdf.get_firmware_metadata_from_file/1 does.
func ReadESPIDFMetadata(path string) (Metadata, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return Metadata{}, nil, err
	}
	defer f.Close()
	head := make([]byte, espHeaderSize)
	if _, err := io.ReadFull(f, head); err != nil {
		return Metadata{}, nil, errors.New("not an ESP-IDF image: too short")
	}
	return parseESPIDFHeader(head)
}

func parseESPIDFHeader(h []byte) (Metadata, []string, error) {
	if len(h) < espHeaderSize || h[0] != espImageMagic {
		return Metadata{}, nil, errors.New("not an ESP-IDF image")
	}
	if le32(h[espAppDescOffset:]) != espAppDescMagic {
		return Metadata{}, nil, errors.New("ESP-IDF image has no app descriptor")
	}
	chipID := binary.LittleEndian.Uint16(h[12:])
	chip, ok := espChips[chipID]
	if !ok {
		chip = [2]string{fmt.Sprintf("esp32-%d", chipID), "unknown"}
	}

	rawVersion := cstr(h[48:80])
	project := cstr(h[80:112])
	idfVer := cstr(h[144:176])
	elfSHA := h[176:208]

	var warnings []string
	version, ok := normaliseESPIDFVersion(rawVersion)
	if !ok {
		version = rawVersion
		warnings = append(warnings, fmt.Sprintf("version %q is not a semantic version; the server will reject it "+
			"(set PROJECT_VER to something like \"1.2.3\")", rawVersion))
	}

	return Metadata{
		Product:      project,
		Version:      version,
		Platform:     chip[0],
		Architecture: chip[1],
		UUID:         uuidFromDigest(elfSHA),
		Description:  "ESP-IDF " + idfVer,
	}, warnings, nil
}

// cstr reads a fixed-width, NUL padded esp_app_desc_t string.
func cstr(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return strings.TrimSpace(string(b))
}

// ESPIDFSigned reports whether the image at path already carries a Secure Boot
// v2 signature block: the block's magic at the start of the last 4 KB sector,
// with a CRC32 that matches. The CRC is checked as well as the magic so that an
// unsigned image whose byte happens to be 0xE7 there is not mistaken for a
// signed one.
func ESPIDFSigned(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	offset := info.Size() - espSigSectorSize
	if offset <= 0 {
		return false, nil
	}
	block := make([]byte, espSigBlockSize)
	if _, err := f.ReadAt(block, offset); err != nil {
		return false, nil
	}
	if block[0] != espSigBlockMagic {
		return false, nil
	}
	return crc32.ChecksumIEEE(block[:espSigBlockCRCLen]) == binary.LittleEndian.Uint32(block[espSigBlockCRCLen:]), nil
}

// EspsecureCommands are the names espsecure is tried under, in order: the
// esptool script and the entry point newer esptool releases install. A
// variable so tests can substitute a stub.
var EspsecureCommands = []string{"espsecure.py", "espsecure"}

// ErrEspsecureNotFound is returned when no espsecure executable is on PATH.
var ErrEspsecureNotFound = errors.New("espsecure not found")

// SignESPIDF signs the image at in with the RSA-3072 PEM key at keyFile using
// `espsecure sign_data --version 2`, writing the signed image to out.
func SignESPIDF(ctx context.Context, in, keyFile, out string) error {
	var bin string
	for _, name := range EspsecureCommands {
		if p, err := exec.LookPath(name); err == nil {
			bin = p
			break
		}
	}
	if bin == "" {
		return ErrEspsecureNotFound
	}

	cmd := exec.CommandContext(ctx, bin, "sign_data", "--version", "2", "--keyfile", keyFile, "--output", out, in)
	// espsecure.py may be a wrapper whose child holds the output pipe after
	// cancellation kills it; don't wait on that child.
	cmd.WaitDelay = 500 * time.Millisecond
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(output.String()); msg != "" {
			return fmt.Errorf("espsecure signing failed: %s", msg)
		}
		return fmt.Errorf("espsecure signing failed: %w", err)
	}
	return nil
}
