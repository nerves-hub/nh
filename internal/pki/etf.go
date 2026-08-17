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

package pki

import (
	"encoding/binary"
	"fmt"
)

// This is a deliberately tiny subset of Erlang's External Term Format (ETF),
// sufficient to encode and decode the signing-key envelope NervesCloud uses: a
// map with atom keys whose values are binaries or a small integer.
const (
	etfVersion       = 131 // leading version byte
	etfTagMap        = 116 // 't' MAP_EXT (u32 arity)
	etfTagSmallAtomU = 119 // 'w' SMALL_ATOM_UTF8_EXT (u8 len)
	etfTagBinary     = 109 // 'm' BINARY_EXT (u32 len)
	etfTagInteger    = 98  // 'b' INTEGER_EXT (i32)
	etfTagSmallInt   = 97  // 'a' SMALL_INTEGER_EXT (u8)
)

// etfWriter builds an ETF-encoded term.
type etfWriter struct{ buf []byte }

func (w *etfWriter) version() { w.buf = append(w.buf, etfVersion) }
func (w *etfWriter) mapHeader(n int) {
	w.buf = append(w.buf, etfTagMap)
	w.buf = binary.BigEndian.AppendUint32(w.buf, uint32(n))
}
func (w *etfWriter) atom(s string) {
	w.buf = append(w.buf, etfTagSmallAtomU, byte(len(s)))
	w.buf = append(w.buf, s...)
}
func (w *etfWriter) binary(b []byte) {
	w.buf = append(w.buf, etfTagBinary)
	w.buf = binary.BigEndian.AppendUint32(w.buf, uint32(len(b)))
	w.buf = append(w.buf, b...)
}
func (w *etfWriter) integer(n int32) {
	w.buf = append(w.buf, etfTagInteger)
	w.buf = binary.BigEndian.AppendUint32(w.buf, uint32(n))
}

// decodeETFMap decodes a version-prefixed ETF map with atom keys. Each value is
// returned as either []byte (binary) or int (integer).
func decodeETFMap(b []byte) (map[string]any, error) {
	r := &etfReader{buf: b}
	v, err := r.u8()
	if err != nil {
		return nil, err
	}
	if v != etfVersion {
		return nil, fmt.Errorf("etf: bad version byte %d", v)
	}
	return r.readMap()
}

type etfReader struct {
	buf []byte
	pos int
}

func (r *etfReader) u8() (byte, error) {
	if r.pos+1 > len(r.buf) {
		return 0, errETFShort
	}
	v := r.buf[r.pos]
	r.pos++
	return v, nil
}

func (r *etfReader) u32() (int, error) {
	if r.pos+4 > len(r.buf) {
		return 0, errETFShort
	}
	v := binary.BigEndian.Uint32(r.buf[r.pos:])
	r.pos += 4
	return int(v), nil
}

func (r *etfReader) take(n int) ([]byte, error) {
	if n < 0 || r.pos+n > len(r.buf) {
		return nil, errETFShort
	}
	v := r.buf[r.pos : r.pos+n]
	r.pos += n
	return v, nil
}

func (r *etfReader) readMap() (map[string]any, error) {
	tag, err := r.u8()
	if err != nil {
		return nil, err
	}
	if tag != etfTagMap {
		return nil, fmt.Errorf("etf: expected map, got tag %d", tag)
	}
	n, err := r.u32()
	if err != nil {
		return nil, err
	}
	m := make(map[string]any, n)
	for i := 0; i < n; i++ {
		key, err := r.readAtom()
		if err != nil {
			return nil, err
		}
		val, err := r.readValue()
		if err != nil {
			return nil, err
		}
		m[key] = val
	}
	return m, nil
}

func (r *etfReader) readAtom() (string, error) {
	tag, err := r.u8()
	if err != nil {
		return "", err
	}
	if tag != etfTagSmallAtomU {
		return "", fmt.Errorf("etf: expected atom key, got tag %d", tag)
	}
	n, err := r.u8()
	if err != nil {
		return "", err
	}
	b, err := r.take(int(n))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (r *etfReader) readValue() (any, error) {
	tag, err := r.u8()
	if err != nil {
		return nil, err
	}
	switch tag {
	case etfTagBinary:
		n, err := r.u32()
		if err != nil {
			return nil, err
		}
		b, err := r.take(n)
		if err != nil {
			return nil, err
		}
		// Copy so the value does not alias the input buffer.
		return append([]byte(nil), b...), nil
	case etfTagInteger:
		n, err := r.u32()
		if err != nil {
			return nil, err
		}
		return int(int32(n)), nil
	case etfTagSmallInt:
		n, err := r.u8()
		if err != nil {
			return nil, err
		}
		return int(n), nil
	default:
		return nil, fmt.Errorf("etf: unsupported value tag %d", tag)
	}
}

var errETFShort = fmt.Errorf("etf: unexpected end of input")
