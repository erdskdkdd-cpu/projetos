"""
Binary reading utilities for Luau bytecode deserialization.
Handles varint (LEB128-style), little-endian integers, floats, doubles.
"""

import struct


class BytecodeReader:
    """Sequential binary reader over a bytes buffer."""

    def __init__(self, data: bytes):
        self.data = data
        self.pos = 0

    def remaining(self) -> int:
        return len(self.data) - self.pos

    def read_byte(self) -> int:
        if self.pos >= len(self.data):
            raise EOFError(f"Attempted to read past end of data at pos {self.pos}")
        b = self.data[self.pos]
        self.pos += 1
        return b

    def read_bytes(self, n: int) -> bytes:
        if self.pos + n > len(self.data):
            raise EOFError(f"Attempted to read {n} bytes past end at pos {self.pos}")
        result = self.data[self.pos:self.pos + n]
        self.pos += n
        return result

    def read_uint32(self) -> int:
        """Read a little-endian unsigned 32-bit integer."""
        raw = self.read_bytes(4)
        return struct.unpack('<I', raw)[0]

    def read_int32(self) -> int:
        """Read a little-endian signed 32-bit integer."""
        raw = self.read_bytes(4)
        return struct.unpack('<i', raw)[0]

    def read_float(self) -> float:
        raw = self.read_bytes(4)
        return struct.unpack('<f', raw)[0]

    def read_double(self) -> float:
        raw = self.read_bytes(8)
        return struct.unpack('<d', raw)[0]

    def read_varint(self) -> int:
        """
        Read a variable-length integer (unsigned LEB128-style encoding used by Luau).
        Each byte uses 7 data bits + 1 continuation bit (MSB).
        """
        result = 0
        shift = 0
        while True:
            b = self.read_byte()
            result |= (b & 0x7F) << shift
            if (b & 0x80) == 0:
                break
            shift += 7
            if shift > 35:
                raise ValueError("Varint too long")
        return result

    def read_string(self, length: int) -> str:
        """Read a fixed-length UTF-8 string."""
        raw = self.read_bytes(length)
        return raw.decode('utf-8', errors='replace')

    def peek_byte(self) -> int:
        if self.pos >= len(self.data):
            raise EOFError(f"Attempted to peek past end of data at pos {self.pos}")
        return self.data[self.pos]


def hex_to_bytes(hex_string: str) -> bytes:
    """Convert a space-separated hex string to bytes."""
    hex_string = hex_string.strip()
    # Remove all whitespace
    hex_string = ''.join(hex_string.split())
    return bytes.fromhex(hex_string)


def decode_import_id(import_val: int):
    """
    Decode a Luau GETIMPORT constant.
    Import IDs encode a chain of string lookups:
      bits 30-20: count (1-3)
      bits 19-10: id0 (first key)
      bits  9-0:  id1 (second key, if count >= 2)
    The third key comes from bits 29-20 in some encodings.
    
    Actually, the Luau import encoding is:
      bits 31-30: count (number of keys, 0-3 but usually 1-3)
      bits 29-20: name0
      bits 19-10: name1
      bits  9-0:  name2
    Each name is a 10-bit index into the string table (1-based).
    """
    count = (import_val >> 30) & 0x3
    id0 = ((import_val >> 20) & 0x3FF)
    id1 = ((import_val >> 10) & 0x3FF)
    id2 = (import_val & 0x3FF)
    
    ids = []
    if count >= 1:
        ids.append(id0)
    if count >= 2:
        ids.append(id1)
    if count >= 3:
        ids.append(id2)
    return ids
