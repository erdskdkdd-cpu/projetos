package lifter

import (
	"Geckocompiler/ast"
	bc "Geckocompiler/bytecode"
)

// liftClosure processes NEWCLOSURE/DUPCLOSURE + following CAPTURE instructions.
func (l *Lifter) liftClosure(
	proto *bc.Proto, insts []*bc.Instruction, index int,
	state *blockState, opcode bc.LuauOpcode,
) (*ast.FunctionExpr, int) {
	inst := insts[index]
	childProto := l.resolveChildProto(proto, inst, opcode)
	if childProto == nil {
		return &ast.FunctionExpr{Body: []ast.Stmt{ast.Comment{Text: "missing child proto"}}}, index + 1
	}

	capturedUpvalues := make([]ast.Expr, 0, childProto.NumUpvalues)
	nextIndex := index + 1
	for off := 0; off < childProto.NumUpvalues; off++ {
		if nextIndex >= len(insts) {
			break
		}
		capture := insts[nextIndex]
		capturedUpvalues = append(capturedUpvalues, l.resolveCaptureValue(proto, state, capture))
		nextIndex++
	}

	// Fill missing upvalues with fallbacks
	for len(capturedUpvalues) < childProto.NumUpvalues {
		capturedUpvalues = append(capturedUpvalues, l.fallbackUpvalue(childProto, len(capturedUpvalues)))
	}

	return l.liftProto(childProto, capturedUpvalues), nextIndex
}

func (l *Lifter) resolveCaptureValue(
	proto *bc.Proto, state *blockState, capture *bc.Instruction,
) ast.Expr {
	captType := bc.LuauCaptureType(capture.A)

	switch captType {
	case bc.CaptureVal:
			if captured, ok := state.regs[capture.B]; ok {
				// If the register contains a local variable, capture it as an upvalue
				// (wrap as UpvalueRef) to avoid leaking a local into the child closure.
				switch v := captured.(type) {
				case ast.LocalVar:
					return ast.UpvalueRef{Name: v.Name, Index: capture.B}
				case ast.UpvalueRef:
					return v
				default:
					return captured
				}
			}
			name := regName(proto, state.regNames, capture.B, capture.PC)
			return ast.UpvalueRef{Name: name, Index: capture.B}

	case bc.CaptureRef:
			if captured, ok := state.regs[capture.B]; ok {
				// Prefer returning an UpvalueRef for captures to preserve
				// proper upvalue semantics in the lifted child proto.
				switch v := captured.(type) {
				case ast.LocalVar:
					return ast.UpvalueRef{Name: v.Name, Index: capture.B}
				case ast.UpvalueRef:
					return v
				}
			}
			name := regName(proto, state.regNames, capture.B, capture.PC)
			return ast.UpvalueRef{Name: name, Index: capture.B}

	case bc.CaptureUpval:
		if capture.B < len(state.upvalues) {
			return state.upvalues[capture.B]
		}
	}

	return l.fallbackUpvalue(proto, capture.B)
}

func (l *Lifter) resolveChildProto(
	proto *bc.Proto, inst *bc.Instruction, opcode bc.LuauOpcode,
) *bc.Proto {
	if opcode == bc.OpNEWCLOSURE {
		if inst.D >= 0 && inst.D < len(proto.ChildProtos) {
			childID := proto.ChildProtos[inst.D]
			if childID >= 0 && childID < len(l.BC.Protos) {
				return l.BC.Protos[childID]
			}
		}
		return nil
	}

	// DUPCLOSURE — constant index
	if inst.D >= 0 && inst.D < len(proto.Constants) {
		c := proto.Constants[inst.D]
		if c.Type == bc.ConstClosure {
			if pid, ok := c.Value.(int); ok && pid >= 0 && pid < len(l.BC.Protos) {
				return l.BC.Protos[pid]
			}
		}
	}
	return nil
}

func (l *Lifter) shouldInlineAnonymousClosure(
	insts []*bc.Instruction, nextIndex, reg int, closure *ast.FunctionExpr,
) bool {
	if closure.Name != "" {
		return false
	}
	if nextIndex >= len(insts) {
		return false
	}
	next := insts[nextIndex]
	switch next.Opcode {
	case bc.OpSETTABLEKS, bc.OpSETTABLE, bc.OpSETTABLEN, bc.OpSETGLOBAL:
		return next.A == reg
	}
	return false
}
