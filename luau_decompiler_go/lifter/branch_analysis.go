package lifter

import (
	"Geckocompiler/ast"
	bc "Geckocompiler/bytecode"
)

type branchResult struct {
	stmt     ast.Stmt
	nextI    int
	regs     map[int]ast.Expr
	declared map[int]bool
	builders map[int]*ast.TableConstructor
}

func (l *Lifter) liftStructuredIf(
	proto *bc.Proto, insts []*bc.Instruction, pcToIdx map[int]int,
	branchIndex int, branchInst *bc.Instruction,
	state *blockState, condition ast.Expr,
) *branchResult {
	targetIdx := jumpTargetIndex(insts, pcToIdx, branchInst)
	if targetIdx < 0 || targetIdx <= branchIndex {
		return nil
	}

	elseEntry := targetIdx
	thenEnd := elseEntry

	// Check for else branch: if instruction before target is a JUMP
	if elseEntry > branchIndex+1 {
		prevIdx := elseEntry - 1
		prevInst := insts[prevIdx]
		if prevInst.Opcode == bc.OpJUMP || prevInst.Opcode == bc.OpJUMPBACK {
			afterElse := jumpTargetIndex(insts, pcToIdx, prevInst)
			if afterElse > elseEntry {
				thenEnd = prevIdx

				elseState := state.clone()
				elseBody, elseStateOut := l.liftBlock(proto, elseEntry, afterElse, elseState)

				thenState := state.clone()
				thenBody, thenStateOut := l.liftBlock(proto, branchIndex+1, thenEnd, thenState)

				if len(thenBody) == 0 && len(elseBody) == 0 {
					merged := mergeBranchState(
						proto, state, thenStateOut, elseStateOut,
						condition, true,
					)
					return &branchResult{
						nextI: afterElse, regs: merged.regs,
						declared: merged.declared, builders: merged.builders,
					}
				}

				ifStmt := ast.IfStat{
					Condition: condition,
					ThenBody:  thenBody,
					ElseBody:  elseBody,
				}
				merged := mergeBranchState(
					proto, state, thenStateOut, elseStateOut,
					condition, false,
				)
				return &branchResult{
					stmt: ifStmt, nextI: afterElse,
					regs: merged.regs, declared: merged.declared,
					builders: merged.builders,
				}
			}
		}
	}

	// No else branch
	thenState := state.clone()
	thenBody, thenStateOut := l.liftBlock(proto, branchIndex+1, thenEnd, thenState)

	if len(thenBody) == 0 {
		merged := mergeBranchState(
			proto, state, thenStateOut, nil,
			condition, true,
		)
		return &branchResult{
			nextI: elseEntry, regs: merged.regs,
			declared: merged.declared, builders: merged.builders,
		}
	}

	ifStmt := ast.IfStat{Condition: condition, ThenBody: thenBody}
	merged := mergeBranchState(
		proto, state, thenStateOut, nil,
		condition, false,
	)
	return &branchResult{
		stmt: ifStmt, nextI: elseEntry,
		regs: merged.regs, declared: merged.declared,
		builders: merged.builders,
	}
}

func jumpTargetIndex(insts []*bc.Instruction, pcToIdx map[int]int, inst *bc.Instruction) int {
	targetPC := inst.PC + 1 + inst.D
	if idx, ok := pcToIdx[targetPC]; ok {
		return idx
	}
	for idx, probe := range insts {
		if probe.PC >= targetPC {
			return idx
		}
	}
	return -1
}

type mergedState struct {
	regs     map[int]ast.Expr
	declared map[int]bool
	builders map[int]*ast.TableConstructor
}

func mergeBranchState(
	proto *bc.Proto,
	incoming *blockState,
	thenState *blockState,
	elseState *blockState,
	condition ast.Expr,
	emitIfExpr bool,
) mergedState {
	merged := mergedState{
		regs:     make(map[int]ast.Expr),
		declared: make(map[int]bool),
		builders: make(map[int]*ast.TableConstructor),
	}

	// Copy incoming
	for k, v := range incoming.regs {
		merged.regs[k] = v
	}
	for k, v := range incoming.declaredRegs {
		merged.declared[k] = v
	}
	for k, v := range incoming.tableBuilders {
		merged.builders[k] = v
	}

	// Merge then declarations
	for k, v := range thenState.declaredRegs {
		merged.declared[k] = v
	}

	if elseState == nil {
		for k, v := range thenState.tableBuilders {
			merged.builders[k] = v
		}
		if emitIfExpr {
			allRegs := allRegKeys(incoming.regs, thenState.regs)
			for _, reg := range allRegs {
				thenVal := thenState.regs[reg]
				if thenVal == nil {
					thenVal = incoming.regs[reg]
				}
				elseVal := incoming.regs[reg]
				if !sameExpr(thenVal, elseVal) {
					merged.regs[reg] = ast.IfExpr{Condition: condition, ThenExpr: thenVal, ElseExpr: elseVal}
				}
			}
		}
		return merged
	}

	// Merge else
	for k, v := range elseState.declaredRegs {
		merged.declared[k] = v
	}
	for k, v := range thenState.tableBuilders {
		merged.builders[k] = v
	}
	for k, v := range elseState.tableBuilders {
		merged.builders[k] = v
	}

	allRegs := allRegKeys3(incoming.regs, thenState.regs, elseState.regs)
	for _, reg := range allRegs {
		thenVal := thenState.regs[reg]
		if thenVal == nil {
			thenVal = incoming.regs[reg]
		}
		elseVal := elseState.regs[reg]
		if elseVal == nil {
			elseVal = incoming.regs[reg]
		}
		if sameExpr(thenVal, elseVal) {
			merged.regs[reg] = thenVal
			continue
		}
		if emitIfExpr {
			merged.regs[reg] = ast.IfExpr{Condition: condition, ThenExpr: thenVal, ElseExpr: elseVal}
		} else {
			merged.regs[reg] = ast.LocalVar{
				Name: regName(proto, incoming.regNames, reg, 0),
				Reg:  reg,
			}
		}
	}

	return merged
}

func allRegKeys(a, b map[int]ast.Expr) []int {
	seen := make(map[int]bool)
	for k := range a {
		seen[k] = true
	}
	for k := range b {
		seen[k] = true
	}
	out := make([]int, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}

func allRegKeys3(a, b, c map[int]ast.Expr) []int {
	seen := make(map[int]bool)
	for k := range a {
		seen[k] = true
	}
	for k := range b {
		seen[k] = true
	}
	for k := range c {
		seen[k] = true
	}
	out := make([]int, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}
