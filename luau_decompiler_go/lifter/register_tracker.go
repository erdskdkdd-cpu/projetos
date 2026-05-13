package lifter

import (
	"fmt"

	"Geckocompiler/ast"
	bc "Geckocompiler/bytecode"
)

// writeRegister emits a local declaration or assignment when the register
// needs a statement, otherwise silently updates the register value.
func (l *Lifter) writeRegister(
	proto *bc.Proto, stmts []ast.Stmt, state *blockState,
	targetReg int, expr ast.Expr, pc int,
) []ast.Stmt {
	suggestion, high := suggestName(expr)
	if suggestion != "" {
		setRegName(state.regNames, targetReg, suggestion, high)
	}

	shouldEmit := shouldEmitRegister(proto, targetReg, state.regNames, state.trackedRegs, state.declaredRegs, pc)

	// Suppress trivial aliasing for untracked first-time writes
	if !shouldEmit && !state.declaredRegs[targetReg] {
		if isTrivialExpr(expr) {
			state.regs[targetReg] = expr
			return stmts
		}
	}

	if !shouldEmit {
		state.regs[targetReg] = expr
		if tc, ok := expr.(*ast.TableConstructor); ok {
			if _, exists := state.tableBuilders[targetReg]; !exists {
				state.tableBuilders[targetReg] = tc
			}
		}
		return stmts
	}

	name := regName(proto, state.regNames, targetReg, pc)

	// If we're assigning a function/closure into this register and a prior
	// local with the same name exists, split the live range so we emit a
	// fresh `local` rather than overwriting the previous variable.
	switch expr.(type) {
	case *ast.FunctionExpr, ast.FunctionExpr:
		if state.declaredRegs[targetReg] {
			// Only split the live range for a new function local if it's
			// safe to do so — don't split when this register is captured
			// later or when it is live later.
			if !l.isRegCapturedLater(proto, targetReg, pc) && !l.isRegLiveLater(proto, targetReg, pc) {
				ver := l.bumpNameVersion(targetReg)
				newName := fmt.Sprintf("%s_%d", name, ver)
				state.regNames[targetReg] = newName
				name = newName
				state.declaredRegs[targetReg] = false
			}
		}
	}

	// SSA cleanup: eliminate dead self-assignments
	if isDeadSelfAssign(name, expr) {
		state.regs[targetReg] = expr
		return stmts
	}

	alreadyDeclared := state.declaredRegs[targetReg]
	existing := state.regs[targetReg]
	sameVariable := false
	if alreadyDeclared {
		if lv, ok := existing.(ast.LocalVar); ok && lv.Name == name {
			// If the previous local is captured by a future closure, or is
			// live later, keep the same variable. Only split when it's safe
			// (not captured and not live later).
			if l.hasAnyCapture(proto, targetReg) || l.isRegLiveLater(proto, targetReg, pc) {
				sameVariable = true
			} else {
				sameVariable = false
				// generate a fresh name for the new live range
				ver := l.bumpNameVersion(targetReg)
				newName := fmt.Sprintf("%s_%d", name, ver)
				state.regNames[targetReg] = newName
				name = newName
				// mark as not declared so we emit a fresh local for this
				// new live range instead of an assignment to the old local.
				state.declaredRegs[targetReg] = false
			}
		}
	}

	if sameVariable {
		stmts = append(stmts, ast.Assign{
			Targets: []ast.Expr{ast.LocalVar{Name: name, Reg: targetReg}},
			Values:  []ast.Expr{expr},
		})
	} else {
		stmts = append(stmts, ast.LocalAssign{Names: []string{name}, Values: []ast.Expr{expr}})
		state.declaredRegs[targetReg] = true
	}

	state.regs[targetReg] = ast.LocalVar{Name: name, Reg: targetReg}
	if tc, ok := expr.(*ast.TableConstructor); ok {
		state.sealedTableBuilders[id(tc)] = true
		state.tableBuilders[targetReg] = tc
	} else {
		delete(state.tableBuilders, targetReg)
	}
	return stmts
}

func shouldEmitRegister(
	proto *bc.Proto, reg int, regNames map[int]string,
	trackedRegs map[int]int, declaredRegs map[int]bool, pc int,
) bool {
	if declaredRegs[reg] {
		return true
	}
	if _, ok := regNames[reg]; ok {
		return true
	}
	if startPC, ok := trackedRegs[reg]; ok && pc >= startPC {
		return true
	}
	for _, lv := range proto.LocalVars {
		if lv.Reg == reg && lv.StartPC <= pc && pc < lv.EndPC && lv.Name != "" {
			return true
		}
	}
	return false
}

func (l *Lifter) scanTrackedRegisters(proto *bc.Proto) map[int]int {
	tracked := make(map[int]int)
	insts := proto.Instructions

	for index, inst := range insts {
		op := inst.Opcode
		if op == bc.OpSETTABLEKS || op == bc.OpSETTABLE {
			trackRegisterFromUsage(proto, tracked, inst.B, index)
		}
		if op == bc.OpRETURN && inst.B > 1 {
			trackRegisterFromUsage(proto, tracked, inst.A, index)
		}
		if op == bc.OpCALL && callResultIsReused(insts, index) {
			trackRegisterFromUsage(proto, tracked, inst.A, index)
		}
	}

	// Closure captures
	idx := 0
	for idx < len(insts) {
		inst := insts[idx]
		op := inst.Opcode
		if op == bc.OpNEWCLOSURE || op == bc.OpDUPCLOSURE {
			child := l.resolveChildProto(proto, inst, op)
			upCount := 0
			if child != nil {
				upCount = child.NumUpvalues
			}
			trackRegisterFromUsage(proto, tracked, inst.A, idx)
			for off := 0; off < upCount; off++ {
				capIdx := idx + 1 + off
				if capIdx >= len(insts) {
					break
				}
				cap := insts[capIdx]
				if cap.Opcode != bc.OpCAPTURE {
					break
				}
				ct := bc.LuauCaptureType(cap.A)
				if ct == bc.CaptureVal || ct == bc.CaptureRef {
					trackRegisterFromUsage(proto, tracked, cap.B, capIdx)
				}
			}
			idx += 1 + upCount
			continue
		}
		idx++
	}
	return tracked
}

func trackRegisterFromUsage(proto *bc.Proto, tracked map[int]int, reg, usageIndex int) {
	startPC := findRegisterTrackingStart(proto, reg, usageIndex)
	if current, ok := tracked[reg]; !ok || startPC < current {
		tracked[reg] = startPC
	}
}

func findRegisterTrackingStart(proto *bc.Proto, reg, usageIndex int) int {
	fallbackPC := proto.Instructions[usageIndex].PC
	lastWritePC := fallbackPC
	for idx := usageIndex - 1; idx >= 0; idx-- {
		inst := proto.Instructions[idx]
		if inst.A != reg {
			continue
		}
		lastWritePC = inst.PC
		switch inst.Opcode {
		case bc.OpLOADNIL, bc.OpCALL, bc.OpMOVE,
			bc.OpNEWTABLE, bc.OpDUPTABLE,
			bc.OpNEWCLOSURE, bc.OpDUPCLOSURE:
			return inst.PC
		}
	}
	return lastWritePC
}

func callResultIsReused(insts []*bc.Instruction, index int) bool {
	inst := insts[index]
	nresults := inst.C - 1
	if inst.C == 0 {
		nresults = -1
	}
	if nresults != 1 {
		return false
	}
	reg := inst.A
	useCount := 0
	for _, probe := range insts[index+1:] {
		if instructionWritesReg(probe, reg) {
			break
		}
		if instructionReadsReg(probe, reg) {
			useCount++
			if useCount > 1 {
				return true
			}
		}
	}
	return false
}

func instructionReadsReg(inst *bc.Instruction, reg int) bool {
	op := inst.Opcode
	switch op {
	case bc.OpMOVE, bc.OpNOT, bc.OpMINUS, bc.OpLENGTH:
		return inst.B == reg
	case bc.OpSETGLOBAL, bc.OpSETUPVAL:
		return inst.A == reg
	case bc.OpJUMPIF, bc.OpJUMPIFNOT,
		bc.OpJUMPXEQKN, bc.OpJUMPXEQKS, bc.OpJUMPXEQKB, bc.OpJUMPXEQKNIL:
		return inst.A == reg
	case bc.OpGETTABLEKS, bc.OpGETTABLEN, bc.OpNAMECALL:
		return inst.B == reg
	case bc.OpGETTABLE, bc.OpSETTABLE,
		bc.OpADD, bc.OpSUB, bc.OpMUL, bc.OpDIV, bc.OpIDIV, bc.OpMOD, bc.OpPOW,
		bc.OpAND, bc.OpOR:
		return inst.B == reg || inst.C == reg || (inst.A == reg && op == bc.OpSETTABLE)
	case bc.OpADDK, bc.OpSUBK, bc.OpMULK, bc.OpDIVK, bc.OpANDK, bc.OpORK:
		return inst.B == reg
	case bc.OpSETTABLEKS, bc.OpSETTABLEN:
		return inst.A == reg || inst.B == reg
	case bc.OpSETLIST:
		if inst.A == reg {
			return true
		}
		if inst.C == 0 {
			return reg >= inst.B
		}
		count := inst.C - 1
		if count < 0 {
			count = 0
		}
		return reg >= inst.B && reg < inst.B+count
	case bc.OpCONCAT:
		return reg >= inst.B && reg <= inst.C
	case bc.OpCALL:
		nargs := inst.B - 1
		if nargs < 0 {
			return reg >= inst.A
		}
		return reg >= inst.A && reg <= inst.A+nargs
	case bc.OpRETURN:
		nvals := inst.B - 1
		if nvals < 0 {
			nvals = 0
		}
		return reg >= inst.A && reg < inst.A+nvals
	case bc.OpJUMPIFEQ, bc.OpJUMPIFNOTEQ, bc.OpJUMPIFLE, bc.OpJUMPIFLT,
		bc.OpJUMPIFNOTLE, bc.OpJUMPIFNOTLT:
		rightReg := -1
		if inst.HasAux() {
			rightReg = inst.Aux & 0xFF
		}
		return inst.A == reg || rightReg == reg
	}
	return false
}

func instructionWritesReg(inst *bc.Instruction, reg int) bool {
	op := inst.Opcode
	if op == bc.OpCALL {
		nresults := inst.C - 1
		if inst.C == 0 {
			nresults = -1
		}
		if nresults < 0 {
			return reg >= inst.A
		}
		if nresults == 0 {
			return false
		}
		return reg >= inst.A && reg < inst.A+nresults
	}

	writeOps := map[bc.LuauOpcode]bool{
		bc.OpLOADNIL: true, bc.OpLOADB: true, bc.OpLOADN: true,
		bc.OpLOADK: true, bc.OpLOADKX: true, bc.OpMOVE: true,
		bc.OpGETGLOBAL: true, bc.OpGETUPVAL: true, bc.OpGETIMPORT: true,
		bc.OpGETTABLEKS: true, bc.OpGETTABLE: true, bc.OpGETTABLEN: true,
		bc.OpNAMECALL: true,
		bc.OpADD: true, bc.OpSUB: true, bc.OpMUL: true, bc.OpDIV: true,
		bc.OpIDIV: true, bc.OpMOD: true, bc.OpPOW: true,
		bc.OpADDK: true, bc.OpSUBK: true, bc.OpMULK: true, bc.OpDIVK: true,
		bc.OpAND: true, bc.OpOR: true, bc.OpANDK: true, bc.OpORK: true,
		bc.OpCONCAT: true, bc.OpNOT: true, bc.OpMINUS: true, bc.OpLENGTH: true,
		bc.OpNEWTABLE: true, bc.OpDUPTABLE: true,
		bc.OpNEWCLOSURE: true, bc.OpDUPCLOSURE: true, bc.OpGETVARARGS: true,
	}
	if writeOps[op] {
		return inst.A == reg
	}
	return false
}

func isTrivialExpr(expr ast.Expr) bool {
	switch expr.(type) {
	case ast.LocalVar, ast.UpvalueRef, ast.GlobalRef,
		ast.VarArgExpr, ast.StringLiteral,
		ast.NumberLiteral, ast.BoolLiteral, ast.NilLiteral:
		return true
	}
	return false
}

func isDeadSelfAssign(name string, expr ast.Expr) bool {
	if lv, ok := expr.(ast.LocalVar); ok && lv.Name == name {
		return true
	}
	if uv, ok := expr.(ast.UpvalueRef); ok && uv.Name == name {
		return true
	}
	return false
}

func foldDeadVariableReturn(stmts []ast.Stmt, values []ast.Expr) []ast.Expr {
	if len(stmts) == 0 || len(values) != 1 {
		return values
	}
	val, ok := values[0].(ast.LocalVar)
	if !ok {
		return values
	}
	prev, ok := stmts[len(stmts)-1].(ast.LocalAssign)
	if !ok || len(prev.Names) != 1 || prev.Names[0] != val.Name || len(prev.Values) == 0 {
		return values
	}
	// Remove the dead local and inline
	// Note: caller is responsible for actually popping stmts
	return prev.Values
}

// tableIDCounter assigns unique IDs to table constructors for sealing.
var tableIDCounter int
var tableIDMap = make(map[*ast.TableConstructor]int)

// id returns a stable unique integer for a TableConstructor pointer.
func id(ptr *ast.TableConstructor) int {
	if v, ok := tableIDMap[ptr]; ok {
		return v
	}
	tableIDCounter++
	tableIDMap[ptr] = tableIDCounter
	return tableIDCounter
}
