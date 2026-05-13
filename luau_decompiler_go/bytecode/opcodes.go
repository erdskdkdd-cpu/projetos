// Package bytecode defines Luau VM opcode constants, format tables, and
// helper types for bytecode versions 3-7.
package bytecode

// LuauOpcode represents a decoded Luau VM opcode.
type LuauOpcode uint8

const (
	OpNOP             LuauOpcode = 0x00
	OpBREAK           LuauOpcode = 0x01
	OpLOADNIL         LuauOpcode = 0x02
	OpLOADB           LuauOpcode = 0x03
	OpLOADN           LuauOpcode = 0x04
	OpLOADK           LuauOpcode = 0x05
	OpMOVE            LuauOpcode = 0x06
	OpGETGLOBAL       LuauOpcode = 0x07
	OpSETGLOBAL       LuauOpcode = 0x08
	OpGETUPVAL        LuauOpcode = 0x09
	OpSETUPVAL        LuauOpcode = 0x0A
	OpCLOSEUPVALS     LuauOpcode = 0x0B
	OpGETIMPORT       LuauOpcode = 0x0C
	OpGETTABLE        LuauOpcode = 0x0D
	OpSETTABLE        LuauOpcode = 0x0E
	OpGETTABLEKS      LuauOpcode = 0x0F
	OpSETTABLEKS      LuauOpcode = 0x10
	OpGETTABLEN       LuauOpcode = 0x11
	OpSETTABLEN       LuauOpcode = 0x12
	OpNEWCLOSURE      LuauOpcode = 0x13
	OpNAMECALL        LuauOpcode = 0x14
	OpCALL            LuauOpcode = 0x15
	OpRETURN          LuauOpcode = 0x16
	OpJUMP            LuauOpcode = 0x17
	OpJUMPBACK        LuauOpcode = 0x18
	OpJUMPIF          LuauOpcode = 0x19
	OpJUMPIFNOT       LuauOpcode = 0x1A
	OpJUMPIFEQ        LuauOpcode = 0x1B
	OpJUMPIFLE        LuauOpcode = 0x1C
	OpJUMPIFLT        LuauOpcode = 0x1D
	OpJUMPIFNOTEQ     LuauOpcode = 0x1E
	OpJUMPIFNOTLE     LuauOpcode = 0x1F
	OpJUMPIFNOTLT     LuauOpcode = 0x20
	OpADD             LuauOpcode = 0x21
	OpSUB             LuauOpcode = 0x22
	OpMUL             LuauOpcode = 0x23
	OpDIV             LuauOpcode = 0x24
	OpMOD             LuauOpcode = 0x25
	OpPOW             LuauOpcode = 0x26
	OpADDK            LuauOpcode = 0x27
	OpSUBK            LuauOpcode = 0x28
	OpMULK            LuauOpcode = 0x29
	OpDIVK            LuauOpcode = 0x2A
	OpMODK            LuauOpcode = 0x2B
	OpPOWK            LuauOpcode = 0x2C
	OpAND             LuauOpcode = 0x2D
	OpOR              LuauOpcode = 0x2E
	OpANDK            LuauOpcode = 0x2F
	OpORK             LuauOpcode = 0x30
	OpCONCAT          LuauOpcode = 0x31
	OpNOT             LuauOpcode = 0x32
	OpMINUS           LuauOpcode = 0x33
	OpLENGTH          LuauOpcode = 0x34
	OpNEWTABLE        LuauOpcode = 0x35
	OpDUPTABLE        LuauOpcode = 0x36
	OpSETLIST         LuauOpcode = 0x37
	OpFORNPREP        LuauOpcode = 0x38
	OpFORNLOOP        LuauOpcode = 0x39
	OpFORGLOOP        LuauOpcode = 0x3A
	OpFORGPREP_INEXT  LuauOpcode = 0x3B
	OpFASTCALL3       LuauOpcode = 0x3C
	OpFORGPREP_NEXT   LuauOpcode = 0x3D
	OpNATIVECALL      LuauOpcode = 0x3E
	OpGETVARARGS      LuauOpcode = 0x3F
	OpDUPCLOSURE      LuauOpcode = 0x40
	OpPREPVARARGS     LuauOpcode = 0x41
	OpLOADKX          LuauOpcode = 0x42
	OpJUMPX           LuauOpcode = 0x43
	OpFASTCALL        LuauOpcode = 0x44
	OpCOVERAGE        LuauOpcode = 0x45
	OpCAPTURE         LuauOpcode = 0x46
	OpSUBRK           LuauOpcode = 0x47
	OpDIVRK           LuauOpcode = 0x48
	OpFASTCALL1       LuauOpcode = 0x49
	OpFASTCALL2       LuauOpcode = 0x4A
	OpFASTCALL2K      LuauOpcode = 0x4B
	OpFORGPREP        LuauOpcode = 0x4C
	OpJUMPXEQKNIL     LuauOpcode = 0x4D
	OpJUMPXEQKB       LuauOpcode = 0x4E
	OpJUMPXEQKN       LuauOpcode = 0x4F
	OpJUMPXEQKS       LuauOpcode = 0x50
	OpIDIV            LuauOpcode = 0x51
	OpIDIVK           LuauOpcode = 0x52
	OpGETUDATAKS      LuauOpcode = 0x53
	OpSETUDATAKS      LuauOpcode = 0x54
	OpNAMECALLUDATA   LuauOpcode = 0x55
)

// LuauConstantType identifies the type of a constant in the constant table.
type LuauConstantType uint8

const (
	ConstNil     LuauConstantType = 0
	ConstBoolean LuauConstantType = 1
	ConstNumber  LuauConstantType = 2
	ConstString  LuauConstantType = 3
	ConstImport  LuauConstantType = 4
	ConstTable   LuauConstantType = 5
	ConstClosure LuauConstantType = 6
	ConstVector  LuauConstantType = 7
)

// LuauCaptureType identifies how an upvalue is captured.
type LuauCaptureType uint8

const (
	CaptureVal   LuauCaptureType = 0 // by value
	CaptureRef   LuauCaptureType = 1 // by reference
	CaptureUpval LuauCaptureType = 2 // re-capture parent upvalue
)

// opcodeNames maps each opcode to its human-readable name.
var opcodeNames = map[LuauOpcode]string{
	OpNOP: "NOP", OpBREAK: "BREAK", OpLOADNIL: "LOADNIL", OpLOADB: "LOADB",
	OpLOADN: "LOADN", OpLOADK: "LOADK", OpMOVE: "MOVE", OpGETGLOBAL: "GETGLOBAL",
	OpSETGLOBAL: "SETGLOBAL", OpGETUPVAL: "GETUPVAL", OpSETUPVAL: "SETUPVAL",
	OpCLOSEUPVALS: "CLOSEUPVALS", OpGETIMPORT: "GETIMPORT", OpGETTABLE: "GETTABLE",
	OpSETTABLE: "SETTABLE", OpGETTABLEKS: "GETTABLEKS", OpSETTABLEKS: "SETTABLEKS",
	OpGETTABLEN: "GETTABLEN", OpSETTABLEN: "SETTABLEN", OpNEWCLOSURE: "NEWCLOSURE",
	OpNAMECALL: "NAMECALL", OpCALL: "CALL", OpRETURN: "RETURN", OpJUMP: "JUMP",
	OpJUMPBACK: "JUMPBACK", OpJUMPIF: "JUMPIF", OpJUMPIFNOT: "JUMPIFNOT",
	OpJUMPIFEQ: "JUMPIFEQ", OpJUMPIFLE: "JUMPIFLE", OpJUMPIFLT: "JUMPIFLT",
	OpJUMPIFNOTEQ: "JUMPIFNOTEQ", OpJUMPIFNOTLE: "JUMPIFNOTLE",
	OpJUMPIFNOTLT: "JUMPIFNOTLT", OpADD: "ADD", OpSUB: "SUB", OpMUL: "MUL",
	OpDIV: "DIV", OpMOD: "MOD", OpPOW: "POW", OpADDK: "ADDK", OpSUBK: "SUBK",
	OpMULK: "MULK", OpDIVK: "DIVK", OpMODK: "MODK", OpPOWK: "POWK", OpAND: "AND",
	OpOR: "OR", OpANDK: "ANDK", OpORK: "ORK", OpCONCAT: "CONCAT", OpNOT: "NOT",
	OpMINUS: "MINUS", OpLENGTH: "LENGTH", OpNEWTABLE: "NEWTABLE",
	OpDUPTABLE: "DUPTABLE", OpSETLIST: "SETLIST", OpFORNPREP: "FORNPREP",
	OpFORNLOOP: "FORNLOOP", OpFORGLOOP: "FORGLOOP", OpFORGPREP_INEXT: "FORGPREP_INEXT",
	OpFASTCALL3: "FASTCALL3", OpFORGPREP_NEXT: "FORGPREP_NEXT",
	OpNATIVECALL: "NATIVECALL", OpGETVARARGS: "GETVARARGS",
	OpDUPCLOSURE: "DUPCLOSURE", OpPREPVARARGS: "PREPVARARGS", OpLOADKX: "LOADKX",
	OpJUMPX: "JUMPX", OpFASTCALL: "FASTCALL", OpCOVERAGE: "COVERAGE",
	OpCAPTURE: "CAPTURE", OpSUBRK: "SUBRK", OpDIVRK: "DIVRK",
	OpFASTCALL1: "FASTCALL1", OpFASTCALL2: "FASTCALL2", OpFASTCALL2K: "FASTCALL2K",
	OpFORGPREP: "FORGPREP", OpJUMPXEQKNIL: "JUMPXEQKNIL", OpJUMPXEQKB: "JUMPXEQKB",
	OpJUMPXEQKN: "JUMPXEQKN", OpJUMPXEQKS: "JUMPXEQKS", OpIDIV: "IDIV",
	OpIDIVK: "IDIVK", OpGETUDATAKS: "GETUDATAKS", OpSETUDATAKS: "SETUDATAKS",
	OpNAMECALLUDATA: "NAMECALLUDATA",
}

// OpcodeName returns the human-readable name for an opcode.
func OpcodeName(op LuauOpcode) string {
	if name, ok := opcodeNames[op]; ok {
		return name
	}
	return "UNKNOWN"
}

// OpcodesWithAux lists opcodes that consume a following AUX word.
var OpcodesWithAux = map[LuauOpcode]bool{
	OpGETGLOBAL: true, OpSETGLOBAL: true, OpGETIMPORT: true,
	OpGETTABLEKS: true, OpSETTABLEKS: true, OpNAMECALL: true,
	OpJUMPIFEQ: true, OpJUMPIFLE: true, OpJUMPIFLT: true,
	OpJUMPIFNOTEQ: true, OpJUMPIFNOTLE: true, OpJUMPIFNOTLT: true,
	OpNEWTABLE: true, OpSETLIST: true, OpFORGLOOP: true,
	OpFASTCALL3: true, OpLOADKX: true, OpFASTCALL: true,
	OpSUBRK: true, OpDIVRK: true, OpFASTCALL1: true,
	OpFASTCALL2: true, OpFASTCALL2K: true,
	OpJUMPXEQKNIL: true, OpJUMPXEQKB: true, OpJUMPXEQKN: true,
	OpJUMPXEQKS: true, OpGETUDATAKS: true, OpSETUDATAKS: true,
	OpNAMECALLUDATA: true,
}
