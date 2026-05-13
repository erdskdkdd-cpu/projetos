"""
Luau Bytecode Opcode Definitions (Bytecode Version 3-7)
Based on Luau open-source VM: https://github.com/luau-lang/luau
"""

from enum import IntEnum

class LuauOpcode(IntEnum):
    NOP = 0x00
    BREAK = 0x01
    LOADNIL = 0x02
    LOADB = 0x03
    LOADN = 0x04
    LOADK = 0x05
    MOVE = 0x06
    GETGLOBAL = 0x07
    SETGLOBAL = 0x08
    GETUPVAL = 0x09
    SETUPVAL = 0x0A
    CLOSEUPVALS = 0x0B
    GETIMPORT = 0x0C
    GETTABLE = 0x0D
    SETTABLE = 0x0E
    GETTABLEKS = 0x0F
    SETTABLEKS = 0x10
    GETTABLEN = 0x11
    SETTABLEN = 0x12
    NEWCLOSURE = 0x13
    NAMECALL = 0x14
    CALL = 0x15
    RETURN = 0x16
    JUMP = 0x17
    JUMPBACK = 0x18
    JUMPIF = 0x19
    JUMPIFNOT = 0x1A
    JUMPIFEQ = 0x1B
    JUMPIFLE = 0x1C
    JUMPIFLT = 0x1D
    JUMPIFNOTEQ = 0x1E
    JUMPIFNOTLE = 0x1F
    JUMPIFNOTLT = 0x20
    ADD = 0x21
    SUB = 0x22
    MUL = 0x23
    DIV = 0x24
    MOD = 0x25
    POW = 0x26
    ADDK = 0x27
    SUBK = 0x28
    MULK = 0x29
    DIVK = 0x2A
    MODK = 0x2B
    POWK = 0x2C
    AND = 0x2D
    OR = 0x2E
    ANDK = 0x2F
    ORK = 0x30
    CONCAT = 0x31
    NOT = 0x32
    MINUS = 0x33
    LENGTH = 0x34
    NEWTABLE = 0x35
    DUPTABLE = 0x36
    SETLIST = 0x37
    FORNPREP = 0x38
    FORNLOOP = 0x39
    FORGLOOP = 0x3A        # moved from 0x3B in older versions
    FORGPREP_INEXT = 0x3B
    FASTCALL3 = 0x3C       # new in v6+
    FORGPREP_NEXT = 0x3D
    NATIVECALL = 0x3E      # new pseudo-instruction
    GETVARARGS = 0x3F
    DUPCLOSURE = 0x40
    PREPVARARGS = 0x41
    LOADKX = 0x42
    JUMPX = 0x43
    FASTCALL = 0x44
    COVERAGE = 0x45
    CAPTURE = 0x46
    SUBRK = 0x47
    DIVRK = 0x48
    FASTCALL1 = 0x49
    FASTCALL2 = 0x4A
    FASTCALL2K = 0x4B
    FORGPREP = 0x4C        # moved from 0x3A in older versions
    JUMPXEQKNIL = 0x4D
    JUMPXEQKB = 0x4E
    JUMPXEQKN = 0x4F
    JUMPXEQKS = 0x50
    IDIV = 0x51
    IDIVK = 0x52
    GETUDATAKS = 0x53      # new: atom-based userdata field access
    SETUDATAKS = 0x54
    NAMECALLUDATA = 0x55


# Instruction format types
# ABC: [op:8][A:8][B:8][C:8]
# AD:  [op:8][A:8][D:16] (D is signed)
# AE:  [op:8][E:24] (E is signed)

OPCODE_FORMAT = {
    LuauOpcode.NOP: "NONE",
    LuauOpcode.BREAK: "NONE",
    LuauOpcode.LOADNIL: "A",
    LuauOpcode.LOADB: "ABC",
    LuauOpcode.LOADN: "AD",
    LuauOpcode.LOADK: "AD",
    LuauOpcode.MOVE: "AB",
    LuauOpcode.GETGLOBAL: "AC",    # +AUX (string index)
    LuauOpcode.SETGLOBAL: "AC",    # +AUX
    LuauOpcode.GETUPVAL: "AB",
    LuauOpcode.SETUPVAL: "AB",
    LuauOpcode.CLOSEUPVALS: "A",
    LuauOpcode.GETIMPORT: "AD",    # +AUX (import id)
    LuauOpcode.GETTABLE: "ABC",
    LuauOpcode.SETTABLE: "ABC",
    LuauOpcode.GETTABLEKS: "ABC",  # +AUX (string index)
    LuauOpcode.SETTABLEKS: "ABC",  # +AUX
    LuauOpcode.GETTABLEN: "ABC",
    LuauOpcode.SETTABLEN: "ABC",
    LuauOpcode.NEWCLOSURE: "AD",
    LuauOpcode.NAMECALL: "ABC",    # +AUX (string index)
    LuauOpcode.CALL: "ABC",
    LuauOpcode.RETURN: "AB",
    LuauOpcode.JUMP: "D",
    LuauOpcode.JUMPBACK: "D",
    LuauOpcode.JUMPIF: "AD",
    LuauOpcode.JUMPIFNOT: "AD",
    LuauOpcode.JUMPIFEQ: "AD",     # +AUX
    LuauOpcode.JUMPIFLE: "AD",     # +AUX
    LuauOpcode.JUMPIFLT: "AD",     # +AUX
    LuauOpcode.JUMPIFNOTEQ: "AD",  # +AUX
    LuauOpcode.JUMPIFNOTLE: "AD",  # +AUX
    LuauOpcode.JUMPIFNOTLT: "AD",  # +AUX
    LuauOpcode.ADD: "ABC",
    LuauOpcode.SUB: "ABC",
    LuauOpcode.MUL: "ABC",
    LuauOpcode.DIV: "ABC",
    LuauOpcode.MOD: "ABC",
    LuauOpcode.POW: "ABC",
    LuauOpcode.ADDK: "ABC",
    LuauOpcode.SUBK: "ABC",
    LuauOpcode.MULK: "ABC",
    LuauOpcode.DIVK: "ABC",
    LuauOpcode.MODK: "ABC",
    LuauOpcode.POWK: "ABC",
    LuauOpcode.AND: "ABC",
    LuauOpcode.OR: "ABC",
    LuauOpcode.ANDK: "ABC",
    LuauOpcode.ORK: "ABC",
    LuauOpcode.CONCAT: "ABC",
    LuauOpcode.NOT: "AB",
    LuauOpcode.MINUS: "AB",
    LuauOpcode.LENGTH: "AB",
    LuauOpcode.NEWTABLE: "AB",     # +AUX (table size hint)
    LuauOpcode.DUPTABLE: "AD",
    LuauOpcode.SETLIST: "ABC",     # +AUX
    LuauOpcode.FORNPREP: "AD",
    LuauOpcode.FORNLOOP: "AD",
    LuauOpcode.FORGLOOP: "AD",     # +AUX (var count in low 8 bits)
    LuauOpcode.FORGPREP_INEXT: "AD",
    LuauOpcode.FASTCALL3: "ABC",   # +AUX
    LuauOpcode.FORGPREP_NEXT: "AD",
    LuauOpcode.NATIVECALL: "AD",
    LuauOpcode.GETVARARGS: "AB",
    LuauOpcode.DUPCLOSURE: "AD",
    LuauOpcode.PREPVARARGS: "A",
    LuauOpcode.LOADKX: "A",        # +AUX (constant index)
    LuauOpcode.JUMPX: "E",
    LuauOpcode.FASTCALL: "AC",     # +AUX (skip)
    LuauOpcode.COVERAGE: "E",
    LuauOpcode.CAPTURE: "AB",
    LuauOpcode.SUBRK: "ABC",       # +AUX
    LuauOpcode.DIVRK: "ABC",       # +AUX
    LuauOpcode.FASTCALL1: "AB",    # +AUX
    LuauOpcode.FASTCALL2: "AB",    # +AUX (extra arg)
    LuauOpcode.FASTCALL2K: "AB",   # +AUX
    LuauOpcode.FORGPREP: "AD",
    LuauOpcode.JUMPXEQKNIL: "AD",  # +AUX
    LuauOpcode.JUMPXEQKB: "AD",    # +AUX
    LuauOpcode.JUMPXEQKN: "AD",    # +AUX
    LuauOpcode.JUMPXEQKS: "AD",    # +AUX
    LuauOpcode.IDIV: "ABC",
    LuauOpcode.IDIVK: "ABC",
    LuauOpcode.GETUDATAKS: "ABC",   # +AUX
    LuauOpcode.SETUDATAKS: "ABC",   # +AUX
    LuauOpcode.NAMECALLUDATA: "ABC", # +AUX
}

# Opcodes that consume an AUX word (next 32-bit instruction)
OPCODES_WITH_AUX = {
    LuauOpcode.GETGLOBAL,
    LuauOpcode.SETGLOBAL,
    LuauOpcode.GETIMPORT,
    LuauOpcode.GETTABLEKS,
    LuauOpcode.SETTABLEKS,
    LuauOpcode.NAMECALL,
    LuauOpcode.JUMPIFEQ,
    LuauOpcode.JUMPIFLE,
    LuauOpcode.JUMPIFLT,
    LuauOpcode.JUMPIFNOTEQ,
    LuauOpcode.JUMPIFNOTLE,
    LuauOpcode.JUMPIFNOTLT,
    LuauOpcode.NEWTABLE,
    LuauOpcode.SETLIST,
    LuauOpcode.FORGLOOP,
    LuauOpcode.FASTCALL3,
    LuauOpcode.LOADKX,
    LuauOpcode.FASTCALL,
    LuauOpcode.SUBRK,
    LuauOpcode.DIVRK,
    LuauOpcode.FASTCALL1,
    LuauOpcode.FASTCALL2,
    LuauOpcode.FASTCALL2K,
    LuauOpcode.JUMPXEQKNIL,
    LuauOpcode.JUMPXEQKB,
    LuauOpcode.JUMPXEQKN,
    LuauOpcode.JUMPXEQKS,
    LuauOpcode.GETUDATAKS,
    LuauOpcode.SETUDATAKS,
    LuauOpcode.NAMECALLUDATA,
}

# Constant types in the constant table
class LuauConstantType(IntEnum):
    NIL = 0
    BOOLEAN = 1
    NUMBER = 2
    STRING = 3
    IMPORT = 4
    TABLE = 5
    CLOSURE = 6
    VECTOR = 7

# Capture types
class LuauCaptureType(IntEnum):
    VAL = 0    # by value
    REF = 1    # by reference
    UPVAL = 2  # re-capture parent upvalue
