package lifter

import (
	"fmt"

	"Geckocompiler/ast"
	bc "Geckocompiler/bytecode"
)

// liftBlock decompiles a range of instructions [start, end) into statements.
func (l *Lifter) liftBlock(
	proto *bc.Proto, start, end int, state *blockState,
) ([]ast.Stmt, *blockState) {
	stmts := []ast.Stmt{}
	insts := proto.Instructions
	pcToIdx := buildPCIndex(insts)

	var pendingNamecallObj ast.Expr
	var pendingNamecallMethod string

	i := start
	for i < end && i < len(insts) {
		inst := insts[i]
		op := inst.Opcode

		// Skip NOPs
		if op == bc.OpNOP || op == bc.OpBREAK || op == bc.OpCLOSEUPVALS {
			i++
			continue
		}

		switch op {
		case bc.OpLOADNIL:
			stmts = l.writeRegister(proto, stmts, state, inst.A, ast.NilLiteral{}, inst.PC)
			i++
		case bc.OpLOADB:
			state.regs[inst.A] = ast.BoolLiteral{Value: inst.B != 0}
			i++
		case bc.OpLOADN:
			state.regs[inst.A] = ast.NumberLiteral{Value: float64(inst.D)}
			i++
		case bc.OpLOADK:
			state.regs[inst.A] = l.getConstExpr(proto, inst.D)
			i++
		case bc.OpLOADKX:
			kidx := inst.Aux
			if kidx < 0 {
				kidx = 0
			}
			state.regs[inst.A] = l.getConstExpr(proto, kidx)
			i++
		case bc.OpMOVE:
			src := l.exprForReg(proto, state, inst.B, inst.PC)
			stmts = l.writeRegister(proto, stmts, state, inst.A, src, inst.PC)
			if tb, ok := state.tableBuilders[inst.B]; ok {
				state.tableBuilders[inst.A] = tb
			}
			i++
		case bc.OpGETGLOBAL:
			name := l.auxString(proto, inst.Aux)
			if name == "" {
				name = fmt.Sprintf("global_%d", inst.A)
			}
			state.regs[inst.A] = ast.GlobalRef{Name: name}
			i++
		case bc.OpSETGLOBAL:
			name := l.auxString(proto, inst.Aux)
			if name == "" {
				name = fmt.Sprintf("global_%d", inst.A)
			}
			val := l.exprForReg(proto, state, inst.A, inst.PC)
			stmts = append(stmts, ast.Assign{Targets: []ast.Expr{ast.GlobalRef{Name: name}}, Values: []ast.Expr{val}})
			i++
		case bc.OpGETUPVAL:
			if inst.B < len(state.upvalues) {
				state.regs[inst.A] = state.upvalues[inst.B]
			} else {
				state.regs[inst.A] = l.fallbackUpvalue(proto, inst.B)
			}
			i++
		case bc.OpSETUPVAL:
			var target ast.Expr
			if inst.B < len(state.upvalues) {
				target = state.upvalues[inst.B]
			} else {
				target = l.fallbackUpvalue(proto, inst.B)
			}
			val := l.exprForReg(proto, state, inst.A, inst.PC)
			stmts = append(stmts, ast.Assign{Targets: []ast.Expr{target}, Values: []ast.Expr{val}})
			i++
		case bc.OpGETIMPORT:
			state.regs[inst.A] = l.getConstExpr(proto, inst.D)
			i++
		case bc.OpGETTABLEKS:
			obj := l.exprForReg(proto, state, inst.B, inst.PC)
			keyName := l.auxString(proto, inst.Aux)
			if keyName == "" {
				keyName = fmt.Sprintf("field_%d", inst.C)
			}
			state.regs[inst.A] = ast.IndexExpr{Obj: obj, Key: ast.StringLiteral{Value: keyName}, IsDot: true}
			i++
		case bc.OpSETTABLEKS:
			keyName := l.auxString(proto, inst.Aux)
			if keyName == "" {
				keyName = fmt.Sprintf("field_%d", inst.C)
			}
			val := l.exprForReg(proto, state, inst.A, inst.PC)
			if l.appendNamedTableField(state, inst.B, keyName, val) {
				i++
				continue
			}
			obj := l.exprForReg(proto, state, inst.B, inst.PC)
			target := ast.IndexExpr{Obj: obj, Key: ast.StringLiteral{Value: keyName}, IsDot: true}
			stmts = append(stmts, ast.Assign{Targets: []ast.Expr{target}, Values: []ast.Expr{val}})
			i++
		case bc.OpGETTABLE:
			obj := l.exprForReg(proto, state, inst.B, inst.PC)
			key := l.exprForReg(proto, state, inst.C, inst.PC)
			state.regs[inst.A] = ast.IndexExpr{Obj: obj, Key: key, IsDot: false}
			i++
		case bc.OpSETTABLE:
			val := l.exprForReg(proto, state, inst.A, inst.PC)
			key := l.exprForReg(proto, state, inst.C, inst.PC)
			if l.appendTableField(state, inst.B, key, val) {
				i++
				continue
			}
			obj := l.exprForReg(proto, state, inst.B, inst.PC)
			target := ast.IndexExpr{Obj: obj, Key: key, IsDot: false}
			stmts = append(stmts, ast.Assign{Targets: []ast.Expr{target}, Values: []ast.Expr{val}})
			i++
		case bc.OpGETTABLEN:
			obj := l.exprForReg(proto, state, inst.B, inst.PC)
			state.regs[inst.A] = ast.IndexExpr{Obj: obj, Key: ast.NumberLiteral{Value: float64(inst.C + 1)}, IsDot: false}
			i++
		case bc.OpSETTABLEN:
			val := l.exprForReg(proto, state, inst.A, inst.PC)
			key := ast.NumberLiteral{Value: float64(inst.C + 1)}
			if l.appendTableField(state, inst.B, key, val) {
				i++
				continue
			}
			obj := l.exprForReg(proto, state, inst.B, inst.PC)
			target := ast.IndexExpr{Obj: obj, Key: key, IsDot: false}
			stmts = append(stmts, ast.Assign{Targets: []ast.Expr{target}, Values: []ast.Expr{val}})
			i++
		case bc.OpNAMECALL:
			pendingNamecallObj = l.exprForReg(proto, state, inst.B, inst.PC)
			pendingNamecallMethod = l.auxString(proto, inst.Aux)
			if pendingNamecallMethod == "" {
				pendingNamecallMethod = fmt.Sprintf("method_%d", inst.C)
			}
			state.regs[inst.A] = ast.GlobalRef{Name: pendingNamecallMethod}
			state.regs[inst.A+1] = pendingNamecallObj
			i++
		case bc.OpCALL:
			stmts, i = l.handleCall(proto, insts, stmts, state, i, &pendingNamecallObj, &pendingNamecallMethod)
		case bc.OpRETURN:
			stmts, i = l.handleReturn(proto, stmts, state, inst, i)
		case bc.OpJUMP, bc.OpJUMPBACK:
			stmts = append(stmts, ast.Comment{Text: fmt.Sprintf("jump %+d", inst.D)})
			i++
		case bc.OpJUMPIF:
			cond := l.exprForReg(proto, state, inst.A, inst.PC)
			newI := l.handleConditionalJump(proto, insts, pcToIdx, &stmts, state, i, inst, ast.UnaryOp{Op: "not", Operand: cond})
			i = newI
		case bc.OpJUMPIFNOT:
			cond := l.exprForReg(proto, state, inst.A, inst.PC)
			newI := l.handleConditionalJump(proto, insts, pcToIdx, &stmts, state, i, inst, cond)
			i = newI
		case bc.OpJUMPIFEQ, bc.OpJUMPIFNOTEQ, bc.OpJUMPIFLE, bc.OpJUMPIFLT, bc.OpJUMPIFNOTLE, bc.OpJUMPIFNOTLT:
			left := l.exprForReg(proto, state, inst.A, inst.PC)
			rightReg := 0
			if inst.HasAux() {
				rightReg = inst.Aux & 0xFF
			}
			right := l.exprForReg(proto, state, rightReg, inst.PC)
			symbolMap := map[bc.LuauOpcode]string{
				bc.OpJUMPIFEQ: "~=", bc.OpJUMPIFNOTEQ: "==",
				bc.OpJUMPIFLE: ">", bc.OpJUMPIFNOTLE: "<=",
				bc.OpJUMPIFLT: ">=", bc.OpJUMPIFNOTLT: "<",
			}
			cond := ast.BinaryOp{Op: symbolMap[op], Left: left, Right: right}
			newI := l.handleConditionalJump(proto, insts, pcToIdx, &stmts, state, i, inst, cond)
			i = newI
		case bc.OpJUMPXEQKN, bc.OpJUMPXEQKS:
			left := l.exprForReg(proto, state, inst.A, inst.PC)
			auxVal := 0
			if inst.HasAux() {
				auxVal = inst.Aux
			}
			constIdx := auxVal & 0xFFFFFF
			invert := (auxVal >> 31) & 1
			right := l.getConstExpr(proto, constIdx)
			symbol := "~="
			if invert == 1 {
				symbol = "=="
			}
			cond := ast.BinaryOp{Op: symbol, Left: left, Right: right}
			newI := l.handleConditionalJump(proto, insts, pcToIdx, &stmts, state, i, inst, cond)
			i = newI
		case bc.OpJUMPXEQKB:
			left := l.exprForReg(proto, state, inst.A, inst.PC)
			auxVal := 0
			if inst.HasAux() {
				auxVal = inst.Aux
			}
			boolVal := (auxVal & 1) != 0
			invert := (auxVal >> 31) & 1
			symbol := "~="
			if invert == 1 {
				symbol = "=="
			}
			cond := ast.BinaryOp{Op: symbol, Left: left, Right: ast.BoolLiteral{Value: boolVal}}
			newI := l.handleConditionalJump(proto, insts, pcToIdx, &stmts, state, i, inst, cond)
			i = newI
		case bc.OpJUMPXEQKNIL:
			left := l.exprForReg(proto, state, inst.A, inst.PC)
			auxVal := 0
			if inst.HasAux() {
				auxVal = inst.Aux
			}
			invert := (auxVal >> 31) & 1
			symbol := "~="
			if invert == 1 {
				symbol = "=="
			}
			cond := ast.BinaryOp{Op: symbol, Left: left, Right: ast.NilLiteral{}}
			newI := l.handleConditionalJump(proto, insts, pcToIdx, &stmts, state, i, inst, cond)
			i = newI
		case bc.OpADD, bc.OpSUB, bc.OpMUL, bc.OpDIV, bc.OpIDIV, bc.OpMOD, bc.OpPOW:
			symMap := map[bc.LuauOpcode]string{
				bc.OpADD: "+", bc.OpSUB: "-", bc.OpMUL: "*",
				bc.OpDIV: "/", bc.OpIDIV: "//", bc.OpMOD: "%", bc.OpPOW: "^",
			}
			left := l.exprForReg(proto, state, inst.B, inst.PC)
			right := l.exprForReg(proto, state, inst.C, inst.PC)
			state.regs[inst.A] = ast.BinaryOp{Op: symMap[op], Left: left, Right: right}
			i++
		case bc.OpADDK, bc.OpSUBK, bc.OpMULK, bc.OpDIVK:
			symMap := map[bc.LuauOpcode]string{
				bc.OpADDK: "+", bc.OpSUBK: "-", bc.OpMULK: "*", bc.OpDIVK: "/",
			}
			left := l.exprForReg(proto, state, inst.B, inst.PC)
			right := l.getConstExpr(proto, inst.C)
			state.regs[inst.A] = ast.BinaryOp{Op: symMap[op], Left: left, Right: right}
			i++
		case bc.OpAND, bc.OpOR:
			sym := "and"
			if op == bc.OpOR {
				sym = "or"
			}
			left := l.exprForReg(proto, state, inst.B, inst.PC)
			right := l.exprForReg(proto, state, inst.C, inst.PC)
			state.regs[inst.A] = ast.BinaryOp{Op: sym, Left: left, Right: right}
			i++
		case bc.OpANDK, bc.OpORK:
			sym := "and"
			if op == bc.OpORK {
				sym = "or"
			}
			left := l.exprForReg(proto, state, inst.B, inst.PC)
			right := l.getConstExpr(proto, inst.C)
			state.regs[inst.A] = ast.BinaryOp{Op: sym, Left: left, Right: right}
			i++
		case bc.OpCONCAT:
			parts := make([]ast.Expr, 0, inst.C-inst.B+1)
			for r := inst.B; r <= inst.C; r++ {
				parts = append(parts, l.exprForReg(proto, state, r, inst.PC))
			}
			state.regs[inst.A] = ast.ConcatExpr{Parts: parts}
			i++
		case bc.OpNOT:
			operand := l.exprForReg(proto, state, inst.B, inst.PC)
			state.regs[inst.A] = ast.UnaryOp{Op: "not", Operand: operand}
			i++
		case bc.OpMINUS:
			operand := l.exprForReg(proto, state, inst.B, inst.PC)
			state.regs[inst.A] = ast.UnaryOp{Op: "-", Operand: operand}
			i++
		case bc.OpLENGTH:
			operand := l.exprForReg(proto, state, inst.B, inst.PC)
			state.regs[inst.A] = ast.UnaryOp{Op: "#", Operand: operand}
			i++
		case bc.OpNEWTABLE, bc.OpDUPTABLE:
			builder := &ast.TableConstructor{}
			stmts = l.writeRegister(proto, stmts, state, inst.A, builder, inst.PC)
			state.tableBuilders[inst.A] = builder
			i++
		case bc.OpSETLIST:
			builder := state.tableBuilders[inst.A]
			if builder != nil {
				valueCount := 0
				if inst.C == 0 {
					probe := inst.B
					for {
						if _, ok := state.regs[probe]; !ok {
							break
						}
						valueCount++
						probe++
					}
				} else {
					valueCount = inst.C - 1
					if valueCount < 0 {
						valueCount = 0
					}
				}
				for off := 0; off < valueCount; off++ {
					builder.Fields = append(builder.Fields, ast.TableField{
						Value: l.exprForReg(proto, state, inst.B+off, inst.PC),
					})
				}
			}
			i++
		case bc.OpNEWCLOSURE, bc.OpDUPCLOSURE:
			closure, nextI := l.liftClosure(proto, insts, i, state, op)
			if l.shouldInlineAnonymousClosure(insts, nextI, inst.A, closure) {
				state.regs[inst.A] = closure
				i = nextI
				continue
			}
			if closure.Name != "" {
				setRegName(state.regNames, inst.A, closure.Name, false)
			}
			stmts = l.writeRegister(proto, stmts, state, inst.A, closure, inst.PC)
			i = nextI
		case bc.OpGETVARARGS:
			state.regs[inst.A] = ast.VarArgExpr{}
			i++
		case bc.OpPREPVARARGS:
			i++
		case bc.OpFORNPREP:
			stmts, i = l.handleNumericFor(proto, insts, stmts, state, i, end)
		case bc.OpFORNLOOP:
			stmts = append(stmts, ast.Comment{Text: fmt.Sprintf("fornloop at R%d, jump %+d", inst.A, inst.D)})
			i++
		case bc.OpFORGPREP, bc.OpFORGPREP_INEXT, bc.OpFORGPREP_NEXT, bc.OpFASTCALL3:
			stmts, i = l.handleGenericFor(proto, insts, stmts, state, i, end)
		case bc.OpFORGLOOP:
			stmts = append(stmts, ast.Comment{Text: fmt.Sprintf("forgloop at R%d, jump %+d", inst.A, inst.D)})
			i++
		case bc.OpFASTCALL, bc.OpFASTCALL1, bc.OpFASTCALL2, bc.OpFASTCALL2K, bc.OpCOVERAGE, bc.OpCAPTURE:
			i++
		default:
			stmts = append(stmts, ast.Comment{Text: fmt.Sprintf("unhandled: %s (0x%02X)", inst.OpName, inst.Opcode)})
			i++
		}
	}

	stmts = foldBooleanAST(stmts)
	return stmts, state
}

func (l *Lifter) handleCall(
	proto *bc.Proto, insts []*bc.Instruction, stmts []ast.Stmt,
	state *blockState, i int,
	pendingObj *ast.Expr, pendingMethod *string,
) ([]ast.Stmt, int) {
	inst := insts[i]
	funcReg := inst.A
	nargs := inst.B - 1
	if inst.B == 0 {
		nargs = -1
	}
	nresults := inst.C - 1
	if inst.C == 0 {
		nresults = -1
	}

	var callExpr ast.Expr
	if *pendingObj != nil && *pendingMethod != "" {
		args := []ast.Expr{}
		explicitArgCount := 0
		if nargs >= 0 {
			explicitArgCount = nargs - 1
			if explicitArgCount < 0 {
				explicitArgCount = 0
			}
		}
		for off := 0; off < explicitArgCount; off++ {
			args = append(args, l.exprForReg(proto, state, funcReg+2+off, inst.PC))
		}
		callExpr = ast.MethodCall{Obj: *pendingObj, Method: *pendingMethod, Args: args}
		*pendingObj = nil
		*pendingMethod = ""
	} else {
		funcExpr := l.exprForReg(proto, state, funcReg, inst.PC)
		args := []ast.Expr{}
		if nargs >= 0 {
			for off := 0; off < nargs; off++ {
				args = append(args, l.exprForReg(proto, state, funcReg+1+off, inst.PC))
			}
		}
		callExpr = ast.FunctionCall{Func: funcExpr, Args: args, NumReturns: nresults}
	}

	if nresults == 0 {
		stmts = append(stmts, ast.ExprStat{Expr: callExpr})
	} else if nresults == 1 {
		suggestion, high := suggestName(callExpr)
		if suggestion != "" {
			setRegName(state.regNames, funcReg, suggestion, high)
		}
		stmts = l.writeRegister(proto, stmts, state, funcReg, callExpr, inst.PC)
	} else if nresults > 1 {
		names := make([]string, 0, nresults)
		for off := 0; off < nresults; off++ {
			reg := funcReg + off
			name := regName(proto, state.regNames, reg, inst.PC)
			names = append(names, name)
			state.regs[reg] = ast.LocalVar{Name: name, Reg: reg}
			state.declaredRegs[reg] = true
		}
		stmts = append(stmts, ast.LocalAssign{Names: names, Values: []ast.Expr{callExpr}})
	} else {
		state.regs[funcReg] = callExpr
		stmts = append(stmts, ast.ExprStat{Expr: callExpr})
	}
	return stmts, i + 1
}

func (l *Lifter) handleReturn(
	proto *bc.Proto, stmts []ast.Stmt, state *blockState,
	inst *bc.Instruction, i int,
) ([]ast.Stmt, int) {
	if inst.B == 0 {
		val := l.exprForReg(proto, state, inst.A, inst.PC)
		if len(stmts) > 0 {
			if es, ok := stmts[len(stmts)-1].(ast.ExprStat); ok && sameExpr(es.Expr, val) {
				stmts = stmts[:len(stmts)-1]
			}
		}
		values := foldDeadVariableReturn(stmts, []ast.Expr{val})
		stmts = append(stmts, ast.ReturnStat{Values: values})
		return stmts, i + 1
	}
	nvals := inst.B - 1
	if nvals < 0 {
		nvals = 0
	}
	values := make([]ast.Expr, 0, nvals)
	for off := 0; off < nvals; off++ {
		values = append(values, l.exprForReg(proto, state, inst.A+off, inst.PC))
	}
	values = foldDeadVariableReturn(stmts, values)
	stmts = append(stmts, ast.ReturnStat{Values: values})
	return stmts, i + 1
}

func (l *Lifter) handleConditionalJump(
	proto *bc.Proto, insts []*bc.Instruction, pcToIdx map[int]int,
	stmts *[]ast.Stmt, state *blockState, i int,
	inst *bc.Instruction, cond ast.Expr,
) int {
	result := l.liftStructuredIf(proto, insts, pcToIdx, i, inst, state, cond)
	if result != nil {
		if result.stmt != nil {
			*stmts = append(*stmts, result.stmt)
		}
		state.regs = result.regs
		state.declaredRegs = result.declared
		state.tableBuilders = result.builders
		return result.nextI
	}
	*stmts = append(*stmts, ast.IfStat{Condition: cond, ThenBody: []ast.Stmt{ast.Comment{Text: fmt.Sprintf("jump %+d", inst.D)}}})
	return i + 1
}

func (l *Lifter) handleNumericFor(
	proto *bc.Proto, insts []*bc.Instruction, stmts []ast.Stmt,
	state *blockState, i, end int,
) ([]ast.Stmt, int) {
	inst := insts[i]
	loopIdx := findNumericLoopEnd(insts, i, end, inst.A)
	if loopIdx < 0 {
		stmts = append(stmts, ast.Comment{Text: fmt.Sprintf("for prep at R%d, jump %+d", inst.A, inst.D)})
		return stmts, i + 1
	}

	loopVarReg := inst.A + 2
	loopVarName := regName(proto, state.regNames, loopVarReg, inst.PC)
	loopStart := l.exprForReg(proto, state, loopVarReg, inst.PC)
	loopStep := l.exprForReg(proto, state, inst.A+1, inst.PC)

	bodyState := state.clone()
	bodyState.regs[loopVarReg] = ast.LocalVar{Name: loopVarName, Reg: loopVarReg}
	bodyState.declaredRegs[loopVarReg] = true
	body, _ := l.liftBlock(proto, i+1, loopIdx, bodyState)

	_ = loopStart // used for possible expression replacement

	stmts = append(stmts, ast.NumericForStat{
		VarName: loopVarName,
		Start:   loopStart,
		Limit:   l.exprForReg(proto, state, inst.A, inst.PC),
		Step:    loopStep,
		Body:    body,
	})
	return stmts, loopIdx + 1
}

func (l *Lifter) handleGenericFor(
	proto *bc.Proto, insts []*bc.Instruction, stmts []ast.Stmt,
	state *blockState, i, end int,
) ([]ast.Stmt, int) {
	inst := insts[i]
	loopEnd := findGenericLoopEnd(insts, i, end, inst.A)
	if loopEnd < 0 {
		stmts = append(stmts, ast.Comment{Text: fmt.Sprintf("forgprep at R%d, jump %+d", inst.A, inst.D)})
		return stmts, i + 1
	}
	loopEndInst := insts[loopEnd]
	nresults := 2
	if loopEndInst.HasAux() && loopEndInst.Aux > 0 {
		nresults = loopEndInst.Aux
	}
	varNames := make([]string, 0, nresults)
	for off := 0; off < nresults; off++ {
		varNames = append(varNames, regName(proto, state.regNames, inst.A+3+off, inst.PC))
	}
	iterators := []ast.Expr{l.exprForReg(proto, state, inst.A, inst.PC)}
	body, _ := l.liftBlock(proto, i+1, loopEnd, state.clone())

	stmts = append(stmts, ast.GenericForStat{
		VarNames: varNames, Iterators: iterators, Body: body,
	})
	return stmts, loopEnd + 1
}

func buildPCIndex(insts []*bc.Instruction) map[int]int {
	m := make(map[int]int, len(insts))
	for idx, inst := range insts {
		m[inst.PC] = idx
	}
	return m
}

func findNumericLoopEnd(insts []*bc.Instruction, start, end, register int) int {
	for idx := start + 1; idx < end && idx < len(insts); idx++ {
		if insts[idx].Opcode == bc.OpFORNLOOP && insts[idx].A == register {
			return idx
		}
	}
	return -1
}

func findGenericLoopEnd(insts []*bc.Instruction, start, end, register int) int {
	for idx := start + 1; idx < end && idx < len(insts); idx++ {
		if insts[idx].Opcode == bc.OpFORGLOOP && insts[idx].A == register {
			return idx
		}
	}
	return -1
}
