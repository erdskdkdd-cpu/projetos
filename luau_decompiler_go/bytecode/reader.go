package bytecode

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
)

// BytecodeReader provides sequential reading over a raw byte buffer.
type BytecodeReader struct {
	Data []byte
	Pos  int
}

// NewReader creates a BytecodeReader from raw bytes.
func NewReader(data []byte) *BytecodeReader {
	return &BytecodeReader{Data: data, Pos: 0}
}

// Remaining returns bytes left to read.
func (r *BytecodeReader) Remaining() int { return len(r.Data) - r.Pos }

// ReadByte reads a single byte.
func (r *BytecodeReader) ReadByte() (byte, error) {
	if r.Pos >= len(r.Data) {
		return 0, fmt.Errorf("read past end at pos %d", r.Pos)
	}
	b := r.Data[r.Pos]
	r.Pos++
	return b, nil
}

// ReadBytes reads exactly n bytes.
func (r *BytecodeReader) ReadBytes(n int) ([]byte, error) {
	if r.Pos+n > len(r.Data) {
		return nil, fmt.Errorf("read %d bytes past end at pos %d", n, r.Pos)
	}
	result := r.Data[r.Pos : r.Pos+n]
	r.Pos += n
	return result, nil
}

// ReadUint32 reads a little-endian unsigned 32-bit integer.
func (r *BytecodeReader) ReadUint32() (uint32, error) {
	raw, err := r.ReadBytes(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(raw), nil
}

// ReadInt32 reads a little-endian signed 32-bit integer.
func (r *BytecodeReader) ReadInt32() (int32, error) {
	raw, err := r.ReadBytes(4)
	if err != nil {
		return 0, err
	}
	return int32(binary.LittleEndian.Uint32(raw)), nil
}

// ReadFloat32 reads a little-endian 32-bit float.
func (r *BytecodeReader) ReadFloat32() (float32, error) {
	raw, err := r.ReadBytes(4)
	if err != nil {
		return 0, err
	}
	bits := binary.LittleEndian.Uint32(raw)
	return math.Float32frombits(bits), nil
}

// ReadFloat64 reads a little-endian 64-bit double.
func (r *BytecodeReader) ReadFloat64() (float64, error) {
	raw, err := r.ReadBytes(8)
	if err != nil {
		return 0, err
	}
	bits := binary.LittleEndian.Uint64(raw)
	return math.Float64frombits(bits), nil
}

// ReadVarint reads an unsigned LEB128-style variable-length integer.
func (r *BytecodeReader) ReadVarint() (int, error) {
	result := 0
	shift := 0
	for {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		result |= int(b&0x7F) << shift
		if b&0x80 == 0 {
			break
		}
		shift += 7
		if shift > 35 {
			return 0, errors.New("varint too long")
		}
	}
	return result, nil
}

// ReadString reads a fixed-length UTF-8 string.
func (r *BytecodeReader) ReadString(length int) (string, error) {
	raw, err := r.ReadBytes(length)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// HexToBytes converts a hex string (optionally space-separated) to bytes.
func HexToBytes(hexStr string) ([]byte, error) {
	cleaned := strings.Join(strings.Fields(strings.TrimSpace(hexStr)), "")
	return hex.DecodeString(cleaned)
}

// DecodeImportID unpacks a Luau GETIMPORT constant into string-table indices.
// Layout: bits 31-30 = count, 29-20 = name0, 19-10 = name1, 9-0 = name2.
func DecodeImportID(importVal uint32) []int {
	count := int((importVal >> 30) & 0x3)
	id0 := int((importVal >> 20) & 0x3FF)
	id1 := int((importVal >> 10) & 0x3FF)
	id2 := int(importVal & 0x3FF)

	ids := make([]int, 0, count)
	if count >= 1 {
		ids = append(ids, id0)
	}
	if count >= 2 {
		ids = append(ids, id1)
	}
	if count >= 3 {
		ids = append(ids, id2)
	}
	return ids
}
