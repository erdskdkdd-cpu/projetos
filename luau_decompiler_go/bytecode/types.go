package bytecode

import "fmt"

// Instruction represents a single decoded Luau VM instruction.
type Instruction struct {
	PC     int        // Program counter (index in instruction list)
	Opcode LuauOpcode // Decoded opcode
	OpName string     // Human-readable opcode name
	A      int        // Register A
	B      int        // Register B
	C      int        // Register C
	D      int        // Signed 16-bit immediate D
	E      int        // Signed 24-bit immediate E
	Aux    int        // AUX word (-1 if absent)
	Raw    uint32     // Raw 32-bit instruction word
}

// HasAux returns true when this instruction consumed an AUX word.
func (i *Instruction) HasAux() bool { return i.Aux >= 0 }

// Constant represents a single entry in a proto's constant table.
type Constant struct {
	Index int              // Position in the constant array
	Type  LuauConstantType // NIL, BOOLEAN, NUMBER, STRING, IMPORT, TABLE, CLOSURE, VECTOR
	Value interface{}      // Actual value (nil, bool, float64, string, uint32, []int, etc.)
}

// Proto represents a function prototype in the bytecode.
type Proto struct {
	ProtoID      int
	MaxStackSize int
	NumParams    int
	NumUpvalues  int
	IsVararg     bool
	Flags        int
	TypeInfo     []byte

	Instructions []*Instruction
	Constants    []*Constant
	ChildProtos  []int // indices into global proto list

	// Debug info (may be absent)
	LineDefined  int
	DebugName    string
	LineInfo     []int
	LocalVars    []LocalVarInfo
	UpvalueNames []string
}

// LocalVarInfo describes a debug-info local variable binding.
type LocalVarInfo struct {
	Name    string
	StartPC int
	EndPC   int
	Reg     int
}

// Bytecode is the top-level container for a deserialized Luau bytecode chunk.
type Bytecode struct {
	Version      int
	TypesVersion int
	Strings      []string
	Protos       []*Proto
	MainProtoID  int
}

// GetString returns a string by its 1-based index (0 → empty).
func (bc *Bytecode) GetString(index int) string {
	if index == 0 {
		return ""
	}
	if index >= 1 && index <= len(bc.Strings) {
		return bc.Strings[index-1]
	}
	return fmt.Sprintf("<string_%d>", index)
}
