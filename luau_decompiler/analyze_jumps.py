"""Analyze JUMPXEQK* semantics to determine correct branch inversion."""
import sys, os
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
from luau_decompiler.deserializer import deserialize

hex_path = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "test_bytecode.hex")
data = bytes.fromhex(open(hex_path).read().strip().replace(" ", ""))
bc = deserialize(data)

p3 = bc.protos[3]

print("=== Proto 3: JUMPXEQKS at idx 6 ===")
inst = p3.instructions[6]
print(f"  op=0x{inst.opcode:02x} A=R{inst.a} D={inst.d}")
print(f"  AUX={inst.aux} => const_idx={inst.aux & 0xFFFFFF}, NOT={(inst.aux >> 31) & 1}")
print(f"  K4 = '{p3.constants[4].value}' (STRING)")
print(f"  Jump target PC = {inst.pc + 1 + inst.d}")
print()

# What's at the target?
target_pc = inst.pc + 1 + inst.d
for i2, inst2 in enumerate(p3.instructions):
    if inst2.pc == target_pc:
        print(f"  Target is idx {i2}: op=0x{inst2.opcode:02x} (JUMPIF at PC {target_pc})")
        break

print()
print("Block from idx 7 to target:")
for i2 in range(7, len(p3.instructions)):
    inst2 = p3.instructions[i2]
    if inst2.pc >= target_pc:
        print(f"  --- target reached at idx {i2} ---")
        break
    print(f"  [{i2:2d}] PC={inst2.pc} op=0x{inst2.opcode:02x} A={inst2.a} D={inst2.d}")

print()
print("=== Proto 3: JUMPXEQKN at idx 24 ===")
inst24 = p3.instructions[24]
print(f"  op=0x{inst24.opcode:02x} A=R{inst24.a} D={inst24.d}")
aux24 = inst24.aux
print(f"  AUX={aux24} => const_idx={aux24 & 0xFFFFFF}, NOT={(aux24 >> 31) & 1}")
cidx = aux24 & 0xFFFFFF
print(f"  K{cidx} = {p3.constants[cidx].value} (NUMBER)")
target_pc24 = inst24.pc + 1 + inst24.d
print(f"  Jump target PC = {target_pc24}")
print()

print("Block from idx 25 to target:")
for i2 in range(25, len(p3.instructions)):
    inst2 = p3.instructions[i2]
    if inst2.pc >= target_pc24:
        print(f"  --- target reached at idx {i2} ---")
        break
    print(f"  [{i2:2d}] PC={inst2.pc} op=0x{inst2.opcode:02x} A={inst2.a} D={inst2.d}")

print()
print("=== JUMPXEQK* BRANCH INVERSION PROOF ===")
print()
print("JUMPXEQKN at idx 24: NOT=1, jumps +3")
print("  Block is only 2 instructions (LOADN R1=1, JUMP +5)")
print("  This is the 'then' path when PlaceId == 120148879522453")
print("  NOT=1 means: jump when R2 ~= K13 (mismatch)")
print("  So block runs when R2 == K13 (match)")
print("  Current code: if (R2 ~= K13) => WRONG")
print("  Correct: if R2 == K13 then ...")
print()
print("JUMPXEQKS at idx 6: NOT=0, jumps +13")
print("  NOT=0 means: jump when R0 == K4 (match)")
print("  So block RUNS when R0 ~= K4 (mismatch)")  
print("  But original code was: if R0 == '[C]' then ...")
print("  Hmm, that means D=13 jumps PAST the anti-cheat block")
print("  The then-body is NOT between idx 7 and target")
print("  Instead, the jump SKIPS to the target when the condition MATCHES")
print("  And the fallthrough (no jump) is the body!")
print()
print("WAIT: This changes everything!")
print("For JUMPXEQKS NOT=0, D=+13:")
print("  If R0 == '[C]': JUMP to target (skip the block)")
print("  If R0 ~= '[C]': fall through (execute the block)")
print("  So the fallthrough body runs when condition FAILS")
print()
print("But for JUMPXEQKN NOT=1, D=+3:")  
print("  If R2 ~= K13: JUMP to target (skip)")
print("  If R2 == K13: fall through (execute)")
print("  So the fallthrough body runs when R2 == K13")
print()
print("FINAL SEMANTICS:")
print("  JUMPXEQK* with NOT=0: jump when EQUAL -> then body when NOT EQUAL")
print("  JUMPXEQK* with NOT=1: jump when NOT EQUAL -> then body when EQUAL")
print("  So: emit '==' when NOT=1, emit '~=' when NOT=0")
print("  This is the OPPOSITE of what our code currently does!")
