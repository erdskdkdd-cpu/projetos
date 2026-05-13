"""
Luau Bytecode Deserializer
Parses raw bytecode bytes into structured Proto/Constant/Instruction objects.
Supports Luau bytecode versions 3-7.
"""

from .utils import BytecodeReader, decode_import_id
from .bytecode import Bytecode, Proto, Constant, Instruction
from .enums import LuauOpcode, LuauConstantType, OPCODES_WITH_AUX, OPCODE_FORMAT
import struct


def _decode_instruction(raw: int) -> dict:
    """Decode a raw 32-bit instruction word into its fields."""
    op = raw & 0xFF
    a = (raw >> 8) & 0xFF
    b = (raw >> 16) & 0xFF
    c = (raw >> 24) & 0xFF

    # D is a signed 16-bit value from bits 16-31
    d = (raw >> 16) & 0xFFFF
    if d >= 0x8000:
        d -= 0x10000

    # E is a signed 24-bit value from bits 8-31
    e = (raw >> 8) & 0xFFFFFF
    if e >= 0x800000:
        e -= 0x1000000

    return {"op": op, "a": a, "b": b, "c": c, "d": d, "e": e}


def _get_opname(opcode: int) -> str:
    """Get the human-readable name of an opcode."""
    try:
        return LuauOpcode(opcode).name
    except ValueError:
        return f"UNKNOWN_{opcode:02X}"


def deserialize(data: bytes) -> Bytecode:
    """
    Deserialize a Luau bytecode buffer into a Bytecode object.
    """
    reader = BytecodeReader(data)
    bc = Bytecode()

    # --- Header ---
    bc.version = reader.read_byte()
    if bc.version == 0:
        raise ValueError("Bytecode error: version 0 indicates a compilation error marker")

    if bc.version >= 4:
        bc.types_version = reader.read_byte()

    # --- String Table ---
    string_count = reader.read_varint()
    for _ in range(string_count):
        str_len = reader.read_varint()
        s = reader.read_string(str_len)
        bc.strings.append(s)

    # --- Proto Table ---
    if bc.version >= 7:
        # Version 7 has an extra byte (usually 0) before proto count
        reader.read_byte()

    proto_count = reader.read_varint()
    for proto_idx in range(proto_count):
        proto = Proto(proto_id=proto_idx)

        proto.max_stack_size = reader.read_byte()
        proto.num_params = reader.read_byte()
        proto.num_upvalues = reader.read_byte()
        proto.is_vararg = bool(reader.read_byte())

        if bc.version >= 4:
            proto.flags = reader.read_byte()
            typeinfo_size = reader.read_varint()
            if typeinfo_size > 0:
                proto.typeinfo = reader.read_bytes(typeinfo_size)

        # --- Instructions ---
        code_size = reader.read_varint()
        raw_instructions = []
        for _ in range(code_size):
            raw_instructions.append(reader.read_uint32())

        # Decode instructions, handling AUX words and opcode mapping
        pc = 0
        while pc < len(raw_instructions):
            raw = raw_instructions[pc]
            fields = _decode_instruction(raw)
            
            # Opcode mapping for this version/obfuscation
            # EncryptedOp = (Op * 227) % 256
            # DecryptedOp = (EncryptedOp * 203) % 256
            op = (fields["op"] * 203) & 0xFF

            inst = Instruction(
                pc=pc,
                opcode=op,
                opname=_get_opname(op),
                a=fields["a"],
                b=fields["b"],
                c=fields["c"],
                d=fields["d"],
                e=fields["e"],
                raw=raw,
            )

            # Check if this opcode has an AUX word
            try:
                has_aux = LuauOpcode(op) in OPCODES_WITH_AUX
            except ValueError:
                has_aux = False

            if has_aux and (pc + 1) < len(raw_instructions):
                inst.aux = raw_instructions[pc + 1]
                pc += 2
            else:
                pc += 1

            proto.instructions.append(inst)

        # --- Constants ---
        const_count = reader.read_varint()
        for k_idx in range(const_count):
            const_type = reader.read_byte()
            const = Constant(index=k_idx, type=const_type)

            if const_type == LuauConstantType.NIL:
                const.value = None
            elif const_type == LuauConstantType.BOOLEAN:
                const.value = bool(reader.read_byte())
            elif const_type == LuauConstantType.NUMBER:
                const.value = reader.read_double()
            elif const_type == LuauConstantType.STRING:
                str_idx = reader.read_varint()
                const.value = bc.get_string(str_idx)
            elif const_type == LuauConstantType.IMPORT:
                const.value = reader.read_uint32()  # Encoded import ID
            elif const_type == LuauConstantType.TABLE:
                table_size = reader.read_varint()
                keys = []
                for _ in range(table_size):
                    keys.append(reader.read_varint())
                const.value = keys  # List of constant indices for keys
            elif const_type == LuauConstantType.CLOSURE:
                const.value = reader.read_varint()  # Proto index
            elif const_type == LuauConstantType.VECTOR:
                x = reader.read_float()
                y = reader.read_float()
                z = reader.read_float()
                w = reader.read_float()
                const.value = (x, y, z, w)
            else:
                print(f"Warning: Unknown constant type {const_type} at proto {proto_idx}, constant {k_idx}")
                # Try to guess - if we can't, we might stay out of sync. 
                # But let's try to just break this proto's constant reading.
                break

            proto.constants.append(const)

        # --- Child protos ---
        child_count = reader.read_varint()
        for _ in range(child_count):
            proto.child_protos.append(reader.read_varint())

        # --- Debug info ---
        # V7: lineDefined(varint) + debugname_idx(varint) come before lineinfo
        try:
            if bc.version >= 7:
                proto.line_defined = reader.read_varint()
                debugname_idx = reader.read_varint()
                proto.debug_name = bc.get_string(debugname_idx)

            has_lineinfo = reader.read_byte()
            if has_lineinfo:
                linegaplog2 = reader.read_byte()
                line_gap = 1 << linegaplog2
                num_instructions = code_size
                lineinfo = []
                for _ in range(num_instructions):
                    delta = reader.read_byte()
                    if delta >= 128:
                        delta -= 256
                    lineinfo.append(delta)
                num_intervals = ((num_instructions - 1) // line_gap) + 1 if num_instructions > 0 else 0
                abs_lines = []
                for _ in range(num_intervals):
                    abs_lines.append(reader.read_int32())
                if abs_lines:
                    current_line = abs_lines[0]
                    resolved_lines = []
                    for i in range(num_instructions):
                        interval_idx = i // line_gap
                        if i % line_gap == 0 and interval_idx < len(abs_lines):
                            current_line = abs_lines[interval_idx]
                        else:
                            current_line += lineinfo[i]
                        resolved_lines.append(current_line)
                    proto.line_info = resolved_lines

            has_debuginfo = reader.read_byte()
            if has_debuginfo:
                local_count = reader.read_varint()
                for _ in range(local_count):
                    name_idx = reader.read_varint()
                    start_pc = reader.read_varint()
                    end_pc = reader.read_varint()
                    reg = reader.read_byte()
                    proto.local_vars.append({
                        "name": bc.get_string(name_idx),
                        "start_pc": start_pc,
                        "end_pc": end_pc,
                        "reg": reg,
                    })
                upval_count = reader.read_varint()
                for _ in range(upval_count):
                    name_idx = reader.read_varint()
                    proto.upvalue_names.append(bc.get_string(name_idx))
        except Exception as e:
            print(f"Warning: Failed to read debug info for proto {proto_idx}: {e}")

        bc.protos.append(proto)

    # --- Main proto index ---
    bc.main_proto_id = reader.read_varint()

    return bc
