"""
Luau Bytecode Decompiler - CLI Entry Point
Usage: python main.py <hex_file> [-o output.luau] [--dump]
"""

import sys
import os
import argparse
import time

# Add parent to path so we can import the package
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from luau_decompiler.utils import hex_to_bytes
from luau_decompiler.deserializer import deserialize
from luau_decompiler.lifter import Lifter
from luau_decompiler.codegen import CodeGen
from luau_decompiler.enums import LuauConstantType


def dump_bytecode(bc):
    """Print a human-readable dump of the deserialized bytecode."""
    print(f"=== Luau Bytecode v{bc.version} (Types v{bc.types_version}) ===")
    print(f"Strings: {len(bc.strings)}")
    for i, s in enumerate(bc.strings):
        print(f"  [{i+1}] \"{s}\"")

    print(f"\nProtos: {len(bc.protos)}")
    for proto in bc.protos:
        print(f"\n--- Proto {proto.proto_id} ---")
        print(f"  max_stack={proto.max_stack_size} params={proto.num_params} "
              f"upvals={proto.num_upvalues} vararg={proto.is_vararg}")
        print(f"  Instructions: {len(proto.instructions)}")
        for inst in proto.instructions:
            aux_str = f" AUX={inst.aux:#010x}" if inst.aux is not None else ""
            print(f"    [{inst.pc:3d}] {inst.opname:<20s} A={inst.a} B={inst.b} C={inst.c} D={inst.d}{aux_str}")
        print(f"  Constants: {len(proto.constants)}")
        for k in proto.constants:
            tname = LuauConstantType(k.type).name if k.type <= 7 else f"UNK({k.type})"
            print(f"    [K{k.index}] {tname}: {k.value!r}")
        print(f"  Children: {proto.child_protos}")
        if proto.upvalue_names:
            print(f"  Upvalue names: {proto.upvalue_names}")
        if proto.local_vars:
            print(f"  Local vars:")
            for lv in proto.local_vars:
                print(f"    R{lv['reg']}: \"{lv['name']}\" (pc {lv['start_pc']}-{lv['end_pc']})")

    print(f"\nMain proto: {bc.main_proto_id}")


def main():
    parser = argparse.ArgumentParser(description="Luau Bytecode Decompiler")
    parser.add_argument("input", help="Path to file containing hex bytecode")
    parser.add_argument("-o", "--output", help="Output file for decompiled Luau code")
    parser.add_argument("--dump", action="store_true", help="Dump raw bytecode structure")
    args = parser.parse_args()

    # Read input
    with open(args.input, 'rb') as f:
        raw_data = f.read()

    # Auto-detect format: raw binary or hex string
    # Luau bytecode versions are typically < 10 (e.g., 5, 6, 7). 
    # Hex strings will start with ASCII characters like '0' (48).
    if len(raw_data) > 0 and raw_data[0] < 10:
        data = raw_data
    else:
        try:
            text = raw_data.decode('ascii').strip()
            data = hex_to_bytes(text)
        except Exception:
            # Fallback to raw data if it can't be decoded as hex
            data = raw_data

    print(f"Read {len(data)} bytes of bytecode")

    # Deserialize
    start = time.time()
    try:
        bc = deserialize(data)
    except Exception as e:
        print(f"Deserialization error: {e}", file=sys.stderr)
        import traceback
        traceback.print_exc()
        sys.exit(1)

    deser_time = time.time() - start
    print(f"Deserialized in {deser_time:.4f}s: {len(bc.strings)} strings, {len(bc.protos)} protos")
    print(f"Main proto ID: {bc.main_proto_id}")

    if args.dump:
        dump_bytecode(bc)
        return

    # Lift to AST
    start = time.time()
    lifter = Lifter(bc)
    try:
        main_ast = lifter.lift_all()
    except Exception as e:
        print(f"Lifter error: {e}", file=sys.stderr)
        import traceback
        traceback.print_exc()
        sys.exit(1)

    lift_time = time.time() - start

    # Generate code
    start = time.time()
    codegen = CodeGen()
    output = codegen.generate(main_ast)
    gen_time = time.time() - start

    header = (
        f"-- Decompiled with LuauDecompiler (Python)\n"
        f"-- Luau version {bc.version}, Types version {bc.types_version}\n"
        f"-- {len(bc.strings)} strings, {len(bc.protos)} protos\n"
        f"-- Deserialized in {deser_time:.4f}s, Lifted in {lift_time:.4f}s, "
        f"Generated in {gen_time:.4f}s\n\n"
    )

    result = header + output

    if args.output:
        with open(args.output, 'w', encoding='utf-8') as f:
            f.write(result)
        print(f"Output written to {args.output}")
    else:
        print("\n" + "=" * 60)
        print(result)


if __name__ == "__main__":
    main()
