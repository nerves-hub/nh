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

package legacy

import (
	"encoding/binary"
	"fmt"
)

// The old nerves_hub_cli persists its config with :erlang.term_to_binary/1 — a
// plain map with atom keys and string values. This decodes just enough of the
// External Term Format (ETF) to read that map: a version-prefixed MAP_EXT whose
// keys are atoms and whose values are binaries or small integers. It is
// deliberately independent of internal/pki's ETF code, which targets the
// signing-key envelope rather than the config term.
const (
	etfVersion = 131 // leading version byte

	etfTagMap        = 116 // 't' MAP_EXT (u32 arity)
	etfTagAtom       = 100 // 'd' ATOM_EXT (u16 len, latin1)
	etfTagAtomUTF8   = 118 // 'v' ATOM_UTF8_EXT (u16 len)
	etfTagSmallAtomU = 119 // 'w' SMALL_ATOM_UTF8_EXT (u8 len)
	etfTagBinary     = 109 // 'm' BINARY_EXT (u32 len)
	etfTagString     = 107 // 'k' STRING_EXT (u16 len, list of bytes)
	etfTagInteger    = 98  // 'b' INTEGER_EXT (i32)
	etfTagSmallInt   = 97  // 'a' SMALL_INTEGER_EXT (u8)
)

// decodeConfigMap decodes a version-prefixed ETF map with atom keys, returning
// string keys mapped to string values. Binary, string, and integer values are
// all rendered as strings; keys or values of other shapes are skipped rather
// than failing the whole parse, so an unexpected extra field can't block a
// migration.
func decodeConfigMap(b []byte) (map[string]string, error) {
	r := &reader{buf: b}
	v, err := r.u8()
	if err != nil {
		return nil, err
	}
	if v != etfVersion {
		return nil, fmt.Errorf("legacy: not an Erlang term (version byte %d)", v)
	}
	tag, err := r.u8()
	if err != nil {
		return nil, err
	}
	if tag != etfTagMap {
		return nil, fmt.Errorf("legacy: expected a map (tag %d), got tag %d", etfTagMap, tag)
	}
	arity, err := r.u32()
	if err != nil {
		return nil, err
	}

	out := make(map[string]string, arity)
	for i := uint32(0); i < arity; i++ {
		key, err := r.term()
		if err != nil {
			return nil, err
		}
		val, err := r.term()
		if err != nil {
			return nil, err
		}
		ks, ok := key.(string)
		if !ok {
			continue // non-atom/binary key: not something we read
		}
		if vs, ok := val.(string); ok {
			out[ks] = vs
		}
	}
	return out, nil
}

// reader consumes an ETF byte stream.
type reader struct {
	buf []byte
	pos int
}

func (r *reader) u8() (byte, error) {
	if r.pos >= len(r.buf) {
		return 0, fmt.Errorf("legacy: unexpected end of term")
	}
	b := r.buf[r.pos]
	r.pos++
	return b, nil
}

func (r *reader) u16() (int, error) {
	if r.pos+2 > len(r.buf) {
		return 0, fmt.Errorf("legacy: unexpected end of term")
	}
	n := int(binary.BigEndian.Uint16(r.buf[r.pos:]))
	r.pos += 2
	return n, nil
}

func (r *reader) u32() (uint32, error) {
	if r.pos+4 > len(r.buf) {
		return 0, fmt.Errorf("legacy: unexpected end of term")
	}
	n := binary.BigEndian.Uint32(r.buf[r.pos:])
	r.pos += 4
	return n, nil
}

func (r *reader) bytes(n int) ([]byte, error) {
	if n < 0 || r.pos+n > len(r.buf) {
		return nil, fmt.Errorf("legacy: unexpected end of term")
	}
	b := r.buf[r.pos : r.pos+n]
	r.pos += n
	return b, nil
}

// term decodes a single value, returning a string for atoms/binaries/strings
// and an int for integers.
func (r *reader) term() (any, error) {
	tag, err := r.u8()
	if err != nil {
		return nil, err
	}
	switch tag {
	case etfTagSmallAtomU:
		n, err := r.u8()
		if err != nil {
			return nil, err
		}
		b, err := r.bytes(int(n))
		return string(b), err
	case etfTagAtom, etfTagAtomUTF8:
		n, err := r.u16()
		if err != nil {
			return nil, err
		}
		b, err := r.bytes(n)
		return string(b), err
	case etfTagBinary:
		n, err := r.u32()
		if err != nil {
			return nil, err
		}
		b, err := r.bytes(int(n))
		return string(b), err
	case etfTagString:
		n, err := r.u16()
		if err != nil {
			return nil, err
		}
		b, err := r.bytes(n)
		return string(b), err
	case etfTagSmallInt:
		n, err := r.u8()
		return int(n), err
	case etfTagInteger:
		n, err := r.u32()
		return int(int32(n)), err
	default:
		return nil, fmt.Errorf("legacy: unsupported term tag %d", tag)
	}
}
