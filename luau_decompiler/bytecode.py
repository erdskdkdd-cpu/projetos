"""
Data classes representing parsed Luau bytecode structures.
"""

from dataclasses import dataclass, field
from typing import List, Optional, Any, Dict


@dataclass
class Instruction:
    """A single decoded Luau VM instruction."""
    pc: int                     # Program counter (index in instruction list)
    opcode: int                 # Raw opcode byte
    opname: str = ""            # Human-readable opcode name
    a: int = 0                  # Register A
    b: int = 0                  # Register B
    c: int = 0                  # Register C
    d: int = 0                  # Signed 16-bit immediate D
    e: int = 0                  # Signed 24-bit immediate E
    aux: Optional[int] = None   # AUX word (next instruction, if applicable)
    raw: int = 0                # Raw 32-bit instruction word


@dataclass
class Constant:
    """A constant in a proto's constant table."""
    index: int
    type: int                   # LuauConstantType
    value: Any = None           # The actual value (None, bool, float, str, import IDs, etc.)


@dataclass
class Proto:
    """A function prototype (proto) in the bytecode."""
    proto_id: int               # Index in the global proto list
    max_stack_size: int = 0
    num_params: int = 0
    num_upvalues: int = 0
    is_vararg: bool = False
    flags: int = 0              # (version >= 4)
    typeinfo: bytes = b""       # Type info bytes (version >= 4)

    instructions: List[Instruction] = field(default_factory=list)
    constants: List[Constant] = field(default_factory=list)
    child_protos: List[int] = field(default_factory=list)  # Indices into global proto list

    # Debug info (may be absent)
    line_defined: int = 0
    debug_name: str = ""
    line_info: List[int] = field(default_factory=list)
    local_vars: List[dict] = field(default_factory=list)
    upvalue_names: List[str] = field(default_factory=list)


@dataclass
class Bytecode:
    """Top-level container for a deserialized Luau bytecode chunk."""
    version: int = 0
    types_version: int = 0
    strings: List[str] = field(default_factory=list)       # 1-indexed in bytecode
    protos: List[Proto] = field(default_factory=list)
    main_proto_id: int = 0

    def get_string(self, index: int) -> str:
        """Get a string by its 1-based index (0 = nil/empty)."""
        if index == 0:
            return ""
        if 1 <= index <= len(self.strings):
            return self.strings[index - 1]
        return f"<string_{index}>"
