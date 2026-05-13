// Package deserializer converts raw Luau bytecode bytes into structured
// Bytecode/Proto/Instruction objects. Supports versions 3-7.
package deserializer

import (
	"fmt"

	bc "Geckocompiler/bytecode"
)

// decodeInstructionFields extracts A/B/C/D/E from a raw 32-bit instruction word.
func decodeInstructionFields(raw uint32) (op, a, b, c uint8, d int16, e int32) {
	op = uint8(raw & 0xFF)
	a = uint8((raw >> 8) & 0xFF)
	b = uint8((raw >> 16) & 0xFF)
	c = uint8((raw >> 24) & 0xFF)

	// D: signed 16-bit from bits 16-31
	dRaw := uint16((raw >> 16) & 0xFFFF)
	d = int16(dRaw)

	// E: signed 24-bit from bits 8-31
	eRaw := (raw >> 8) & 0xFFFFFF
	if eRaw >= 0x800000 {
		e = int32(eRaw) - 0x1000000
	} else {
		e = int32(eRaw)
	}
	return
}

// Deserialize parses a Luau bytecode buffer into a Bytecode structure.
func Deserialize(data []byte) (*bc.Bytecode, error) {
	reader := bc.NewReader(data)
	result := &bc.Bytecode{}

	// --- Header ---
	version, err := reader.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("reading version: %w", err)
	}
	result.Version = int(version)
	if result.Version == 0 {
		return nil, fmt.Errorf("version 0 indicates a compilation error marker")
	}

	if result.Version >= 4 {
		tv, err := reader.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("reading types version: %w", err)
		}
		result.TypesVersion = int(tv)
	}

	// --- String Table ---
	stringCount, err := reader.ReadVarint()
	if err != nil {
		return nil, fmt.Errorf("reading string count: %w", err)
	}
	result.Strings = make([]string, 0, stringCount)
	for i := 0; i < stringCount; i++ {
		strLen, err := reader.ReadVarint()
		if err != nil {
			return nil, fmt.Errorf("reading string %d length: %w", i, err)
		}
		s, err := reader.ReadString(strLen)
		if err != nil {
			return nil, fmt.Errorf("reading string %d: %w", i, err)
		}
		result.Strings = append(result.Strings, s)
	}

	// --- Proto Table ---
	if result.Version >= 7 {
		// Version 7 has an extra byte before proto count
		if _, err := reader.ReadByte(); err != nil {
			return nil, fmt.Errorf("reading v7 extra byte: %w", err)
		}
	}

	protoCount, err := reader.ReadVarint()
	if err != nil {
		return nil, fmt.Errorf("reading proto count: %w", err)
	}

	result.Protos = make([]*bc.Proto, 0, protoCount)
	for pIdx := 0; pIdx < protoCount; pIdx++ {
		proto, err := deserializeProto(reader, result, pIdx)
		if err != nil {
			return nil, fmt.Errorf("proto %d: %w", pIdx, err)
		}
		result.Protos = append(result.Protos, proto)
	}

	// --- Main proto index ---
	mainID, err := reader.ReadVarint()
	if err != nil {
		return nil, fmt.Errorf("reading main proto id: %w", err)
	}
	result.MainProtoID = mainID

	return result, nil
}

func deserializeProto(reader *bc.BytecodeReader, result *bc.Bytecode, pIdx int) (*bc.Proto, error) {
	proto := &bc.Proto{ProtoID: pIdx}

	maxStack, err := reader.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("reading max_stack: %w", err)
	}
	proto.MaxStackSize = int(maxStack)

	numParams, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	proto.NumParams = int(numParams)

	numUpvalues, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	proto.NumUpvalues = int(numUpvalues)

	isVararg, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	proto.IsVararg = isVararg != 0

	if result.Version >= 4 {
		flags, err := reader.ReadByte()
		if err != nil {
			return nil, err
		}
		proto.Flags = int(flags)

		typeInfoSize, err := reader.ReadVarint()
		if err != nil {
			return nil, err
		}
		if typeInfoSize > 0 {
			ti, err := reader.ReadBytes(typeInfoSize)
			if err != nil {
				return nil, err
			}
			proto.TypeInfo = ti
		}
	}

	// --- Instructions ---
	if err := deserializeInstructions(reader, proto); err != nil {
		return nil, fmt.Errorf("instructions: %w", err)
	}

	// --- Constants ---
	if err := deserializeConstants(reader, result, proto, pIdx); err != nil {
		return nil, fmt.Errorf("constants: %w", err)
	}

	// --- Child protos ---
	childCount, err := reader.ReadVarint()
	if err != nil {
		return nil, fmt.Errorf("reading child count: %w", err)
	}
	proto.ChildProtos = make([]int, 0, childCount)
	for i := 0; i < childCount; i++ {
		cid, err := reader.ReadVarint()
		if err != nil {
			return nil, err
		}
		proto.ChildProtos = append(proto.ChildProtos, cid)
	}

	// --- Debug info ---
	deserializeDebugInfo(reader, result, proto)

	return proto, nil
}

func deserializeInstructions(reader *bc.BytecodeReader, proto *bc.Proto) error {
	codeSize, err := reader.ReadVarint()
	if err != nil {
		return err
	}

	rawInsts := make([]uint32, 0, codeSize)
	for i := 0; i < codeSize; i++ {
		raw, err := reader.ReadUint32()
		if err != nil {
			return err
		}
		rawInsts = append(rawInsts, raw)
	}

	pc := 0
	for pc < len(rawInsts) {
		raw := rawInsts[pc]
		opRaw, a, b, c, d, e := decodeInstructionFields(raw)

		// Opcode decryption: (encryptedOp * 203) & 0xFF
		op := bc.LuauOpcode((uint16(opRaw) * 203) & 0xFF)

		inst := &bc.Instruction{
			PC:     pc,
			Opcode: op,
			OpName: bc.OpcodeName(op),
			A:      int(a),
			B:      int(b),
			C:      int(c),
			D:      int(d),
			E:      int(e),
			Aux:    -1,
			Raw:    raw,
		}

		// Check for AUX word
		if bc.OpcodesWithAux[op] && pc+1 < len(rawInsts) {
			inst.Aux = int(rawInsts[pc+1])
			pc += 2
		} else {
			pc++
		}

		proto.Instructions = append(proto.Instructions, inst)
	}
	return nil
}

func deserializeConstants(reader *bc.BytecodeReader, result *bc.Bytecode, proto *bc.Proto, pIdx int) error {
	constCount, err := reader.ReadVarint()
	if err != nil {
		return err
	}

	proto.Constants = make([]*bc.Constant, 0, constCount)
	for kIdx := 0; kIdx < constCount; kIdx++ {
		constType, err := reader.ReadByte()
		if err != nil {
			return err
		}
		ct := bc.LuauConstantType(constType)
		c := &bc.Constant{Index: kIdx, Type: ct}

		switch ct {
		case bc.ConstNil:
			c.Value = nil
		case bc.ConstBoolean:
			bv, err := reader.ReadByte()
			if err != nil {
				return err
			}
			c.Value = bv != 0
		case bc.ConstNumber:
			v, err := reader.ReadFloat64()
			if err != nil {
				return err
			}
			c.Value = v
		case bc.ConstString:
			strIdx, err := reader.ReadVarint()
			if err != nil {
				return err
			}
			c.Value = result.GetString(strIdx)
		case bc.ConstImport:
			v, err := reader.ReadUint32()
			if err != nil {
				return err
			}
			c.Value = v
		case bc.ConstTable:
			tableSize, err := reader.ReadVarint()
			if err != nil {
				return err
			}
			keys := make([]int, 0, tableSize)
			for j := 0; j < tableSize; j++ {
				k, err := reader.ReadVarint()
				if err != nil {
					return err
				}
				keys = append(keys, k)
			}
			c.Value = keys
		case bc.ConstClosure:
			v, err := reader.ReadVarint()
			if err != nil {
				return err
			}
			c.Value = v
		case bc.ConstVector:
			x, err := reader.ReadFloat32()
			if err != nil {
				return err
			}
			y, err := reader.ReadFloat32()
			if err != nil {
				return err
			}
			z, err := reader.ReadFloat32()
			if err != nil {
				return err
			}
			w, err := reader.ReadFloat32()
			if err != nil {
				return err
			}
			c.Value = [4]float32{x, y, z, w}
		default:
			fmt.Printf("Warning: Unknown constant type %d at proto %d, constant %d\n", constType, pIdx, kIdx)
			proto.Constants = append(proto.Constants, c)
			return nil // stop reading constants for this proto
		}

		proto.Constants = append(proto.Constants, c)
	}
	return nil
}

func deserializeDebugInfo(reader *bc.BytecodeReader, result *bc.Bytecode, proto *bc.Proto) {
	// Wrapped in recovery since debug info may be truncated
	defer func() { recover() }()

	if result.Version >= 7 {
		lineDefined, err := reader.ReadVarint()
		if err != nil {
			return
		}
		proto.LineDefined = lineDefined

		debugNameIdx, err := reader.ReadVarint()
		if err != nil {
			return
		}
		proto.DebugName = result.GetString(debugNameIdx)
	}

	hasLineInfo, err := reader.ReadByte()
	if err != nil {
		return
	}
	if hasLineInfo != 0 {
		parseLineInfo(reader, proto)
	}

	hasDebugInfo, err := reader.ReadByte()
	if err != nil {
		return
	}
	if hasDebugInfo != 0 {
		parseLocalVarInfo(reader, result, proto)
	}
}

func parseLineInfo(reader *bc.BytecodeReader, proto *bc.Proto) {
	lineGapLog2, err := reader.ReadByte()
	if err != nil {
		return
	}
	lineGap := 1 << lineGapLog2
	numInstructions := len(proto.Instructions)

	// Line deltas — one per raw instruction (including AUX words)
	codeSize := numInstructions
	// Recalculate code size: each instruction with AUX counts as 2 raw slots
	rawCodeSize := 0
	for _, inst := range proto.Instructions {
		rawCodeSize++
		if inst.HasAux() {
			rawCodeSize++
		}
	}
	codeSize = rawCodeSize

	lineInfo := make([]int, 0, codeSize)
	for i := 0; i < codeSize; i++ {
		b, err := reader.ReadByte()
		if err != nil {
			return
		}
		delta := int(int8(b))
		lineInfo = append(lineInfo, delta)
	}

	numIntervals := 0
	if codeSize > 0 {
		numIntervals = ((codeSize - 1) / lineGap) + 1
	}

	absLines := make([]int32, 0, numIntervals)
	for i := 0; i < numIntervals; i++ {
		v, err := reader.ReadInt32()
		if err != nil {
			return
		}
		absLines = append(absLines, v)
	}

	if len(absLines) > 0 {
		currentLine := int(absLines[0])
		resolved := make([]int, 0, codeSize)
		for i := 0; i < codeSize; i++ {
			intervalIdx := i / lineGap
			if i%lineGap == 0 && intervalIdx < len(absLines) {
				currentLine = int(absLines[intervalIdx])
			} else {
				currentLine += lineInfo[i]
			}
			resolved = append(resolved, currentLine)
		}
		proto.LineInfo = resolved
	}
}

func parseLocalVarInfo(reader *bc.BytecodeReader, result *bc.Bytecode, proto *bc.Proto) {
	localCount, err := reader.ReadVarint()
	if err != nil {
		return
	}
	for i := 0; i < localCount; i++ {
		nameIdx, err := reader.ReadVarint()
		if err != nil {
			return
		}
		startPC, err := reader.ReadVarint()
		if err != nil {
			return
		}
		endPC, err := reader.ReadVarint()
		if err != nil {
			return
		}
		reg, err := reader.ReadByte()
		if err != nil {
			return
		}
		proto.LocalVars = append(proto.LocalVars, bc.LocalVarInfo{
			Name:    result.GetString(nameIdx),
			StartPC: startPC,
			EndPC:   endPC,
			Reg:     int(reg),
		})
	}

	upvalCount, err := reader.ReadVarint()
	if err != nil {
		return
	}
	for i := 0; i < upvalCount; i++ {
		nameIdx, err := reader.ReadVarint()
		if err != nil {
			return
		}
		proto.UpvalueNames = append(proto.UpvalueNames, result.GetString(nameIdx))
	}
}
