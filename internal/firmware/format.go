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

// Package firmware recognises the firmware formats NervesHub accepts, reads
// their metadata and signs them, mirroring the server's update tools in
// lib/nerves_hub/firmwares/update_tool/ of nerves_hub_web.
package firmware

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Format is a firmware format, named as the server records it in a firmware's
// "tool" field.
type Format string

const (
	// Fwup is an fwup archive (.fw), a zip file.
	Fwup Format = "fwup"
	// ESPIDF is an ESP-IDF application image (.bin).
	ESPIDF Format = "esp-idf"
	// AtomVM is an AtomVM packbeam archive (.avm).
	AtomVM Format = "atomvm"
	// RAUC is a RAUC bundle (.raucb).
	RAUC Format = "rauc"
)

// Formats lists every format nh recognises, in the order they are described.
var Formats = []Format{Fwup, ESPIDF, AtomVM, RAUC}

// Extension is the file extension the server stores this format under,
// leading dot included.
func (f Format) Extension() string {
	switch f {
	case Fwup:
		return ".fw"
	case ESPIDF:
		return ".bin"
	case AtomVM:
		return ".avm"
	case RAUC:
		return ".raucb"
	}
	return ""
}

// Supported describes every recognised format the way the server's
// UpdateTool.accepted_formats/0 does: "atomvm (.avm), esp-idf (.bin), ...".
func Supported() string {
	parts := make([]string, 0, len(Formats))
	for _, f := range Formats {
		parts = append(parts, fmt.Sprintf("%s (%s)", f, f.Extension()))
	}
	return strings.Join(parts, ", ")
}

// ErrUnknownFormat is returned by Detect for a file none of the formats
// recognise.
var ErrUnknownFormat = errors.New("unrecognised firmware format")

const (
	// espImageMagic is esp_image_header_t.magic, the first byte of every
	// ESP-IDF image.
	espImageMagic = 0xE9
	// espAppDescMagic is esp_app_desc_t.magic_word, little-endian at 0x20.
	espAppDescMagic = 0xABCD5432
	// espAppDescOffset is sizeof(esp_image_header_t) +
	// sizeof(esp_image_segment_header_t).
	espAppDescOffset = 0x20
	espAppDescSize   = 256
	espHeaderSize    = espAppDescOffset + espAppDescSize
)

// atomVMMagic is the 24 bytes every packbeam opens with: a shebang, NUL padded
// to a 4 byte boundary.
var atomVMMagic = []byte("#!/usr/bin/env AtomVM\n\x00\x00")

// squashfsMagic opens every RAUC bundle, whose payload is a SquashFS image.
var squashfsMagic = []byte("hsqs")

// zipMagic is a zip local file header, which opens every fwup archive.
var zipMagic = []byte("PK\x03\x04")

// Detect reports the format of the file at path from its leading bytes, the
// same magic-number checks as each server tool's recognises?/1.
func Detect(path string) (Format, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	head := make([]byte, espHeaderSize)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", err
	}
	return DetectBytes(head[:n])
}

// DetectBytes is Detect over the leading bytes of a file. It needs at most the
// first 288 bytes.
func DetectBytes(head []byte) (Format, error) {
	switch {
	case bytes.HasPrefix(head, zipMagic):
		return Fwup, nil
	case bytes.HasPrefix(head, atomVMMagic):
		return AtomVM, nil
	case bytes.HasPrefix(head, squashfsMagic):
		return RAUC, nil
	case len(head) >= espHeaderSize && head[0] == espImageMagic &&
		le32(head[espAppDescOffset:]) == espAppDescMagic:
		return ESPIDF, nil
	}
	return "", fmt.Errorf("%w: NervesHub accepts %s", ErrUnknownFormat, Supported())
}

func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

// Metadata is what an image says about itself, in NervesHub's terms. Fields
// the format cannot supply are left empty.
type Metadata struct {
	Product      string
	Version      string
	Platform     string
	Architecture string
	UUID         string
	Description  string
}

// uuidFromDigest formats the first 16 bytes of a digest as a UUID, the
// convention every non-fwup tool on the server uses.
func uuidFromDigest(d []byte) string {
	if len(d) < 16 {
		return ""
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", d[0:4], d[4:6], d[6:8], d[8:10], d[10:16])
}

// ReadMetadata reads the metadata of an ESP-IDF, AtomVM or RAUC image the
// way the server will. fwup archives are read with `fwup -m` instead, which
// this package does not wrap. Warnings describe problems the server will
// reject the upload for that do not stop the metadata being read.
func ReadMetadata(format Format, path string) (Metadata, []string, error) {
	switch format {
	case ESPIDF:
		return ReadESPIDFMetadata(path)
	case AtomVM:
		archive, err := os.ReadFile(path)
		if err != nil {
			return Metadata{}, nil, err
		}
		return ReadAtomVMMetadata(archive)
	case RAUC:
		return ReadRAUCMetadata(path)
	}
	return Metadata{}, nil, fmt.Errorf("no built-in metadata reader for %s", format)
}
