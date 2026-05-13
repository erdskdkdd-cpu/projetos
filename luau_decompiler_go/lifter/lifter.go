// Package lifter converts Luau bytecode instructions into AST nodes.
package lifter

import (
	"reflect"

	"Geckocompiler/ast"
	bc "Geckocompiler/bytecode"
)

// Lifter transforms deserialized bytecode into a high-level AST.
type Lifter struct {
	BC *bc.Bytecode
	nameVersion map[int]int // per-register version counter for fresh local names
}

// NewLifter creates a Lifter for the given bytecode.
func NewLifter(bytecode *bc.Bytecode) *Lifter {
	return &Lifter{BC: bytecode, nameVersion: make(map[int]int)}
}

// LiftAll decompiles the main proto into a FunctionExpr AST tree.
func (l *Lifter) LiftAll() *ast.FunctionExpr {
	main := l.BC.Protos[l.BC.MainProtoID]
	lifted := l.liftProto(main, nil)
	lifted.Name = ""
	return lifted
}

func (l *Lifter) liftProto(proto *bc.Proto, upvalueBindings []ast.Expr) *ast.FunctionExpr {
	regNames := make(map[int]string)
	trackedRegs := l.scanTrackedRegisters(proto)
	tableBuilders := make(map[int]*ast.TableConstructor)
	sealedBuilders := make(map[int]bool)
	declaredRegs := make(map[int]bool)
	registers := make(map[int]ast.Expr)
	upvalues := make([]ast.Expr, len(upvalueBindings))
	copy(upvalues, upvalueBindings)

	params := make([]string, 0, proto.NumParams)
	for i := 0; i < proto.NumParams; i++ {
		name := paramName(proto, i)
		regNames[i] = name
		declaredRegs[i] = true
		registers[i] = ast.LocalVar{Name: name, Reg: i}
		params = append(params, name)
	}

	seedProtoNames(l.BC, proto, regNames)

	state := &blockState{
		regs: registers, declaredRegs: declaredRegs,
		regNames: regNames, tableBuilders: tableBuilders,
		sealedTableBuilders: sealedBuilders, upvalues: upvalues,
		trackedRegs: trackedRegs,
	}

	body, _ := l.liftBlock(proto, 0, len(proto.Instructions), state)

	// Post-process the lifted body:
	// 1) propagate trivial copies (remove redundant MOVE aliases)
	// 2) reorder top-level local assigns (requires) by dependency
	// 3) deduplicate table constructor fields to avoid orphaned references
	body = l.propagateCopies(body)
	body = reorderLocalAssigns(body)
	body = mergeAdjacentIfs(body)
	body = dedupeTableFieldsInStmts(body)
	// Merge adjacent if-statements that have identical bodies into a single
	// if with an `or`-combined condition. This helps recover compound OR
	// conditions that were lowered into multiple conditional jumps.
	body = l.mergeAdjacentIfs(body)

	return &ast.FunctionExpr{
		Params: params, IsVararg: proto.IsVararg,
		Body: body, Name: proto.DebugName,
	}
}

// reorderLocalAssigns looks for contiguous LocalAssign statements at the
// start of the function body and sorts them by trailing _<num> suffix in
// the declared name when present. This helps preserve register order for
// require() assignments which include a reg-based suffix.
func reorderLocalAssigns(body []ast.Stmt) []ast.Stmt {
	if len(body) == 0 {
		return body
	}
	// collect prefix local assigns
	end := 0
	for end < len(body) {
		if _, ok := body[end].(ast.LocalAssign); !ok {
			break
		}
		end++
	}
	if end <= 1 {
		return body
	}
	prefix := body[:end]
	rest := body[end:]
	// Build map of local-assigned names and their ASTs
	localNames := make([]string, 0, len(prefix))
	assigns := make([]ast.LocalAssign, 0, len(prefix))
	for _, s := range prefix {
		ls := s.(ast.LocalAssign)
		assigns = append(assigns, ls)
		if len(ls.Names) > 0 {
			localNames = append(localNames, ls.Names[0])
		} else {
			localNames = append(localNames, "")
		}
	}

	// helper: collect referenced local names inside an expr
	var collectRefs func(ast.Expr, map[string]bool)
	collectRefs = func(e ast.Expr, out map[string]bool) {
		if e == nil {
			return
		}
		switch v := e.(type) {
		case ast.LocalVar:
			out[v.Name] = true
		case ast.UpvalueRef:
			out[v.Name] = true
		case ast.GlobalRef:
			out[v.Name] = true
		case *ast.TableConstructor:
			for _, f := range v.Fields { collectRefs(f.Value, out) }
		case ast.TableConstructor:
			for _, f := range v.Fields { collectRefs(f.Value, out) }
		case ast.MethodCall:
			collectRefs(v.Obj, out)
			for _, a := range v.Args { collectRefs(a, out) }
		case ast.FunctionCall:
			collectRefs(v.Func, out)
			for _, a := range v.Args { collectRefs(a, out) }
		case ast.IndexExpr:
			collectRefs(v.Obj, out)
			collectRefs(v.Key, out)
		case ast.BinaryOp:
			collectRefs(v.Left, out)
			collectRefs(v.Right, out)
		case ast.UnaryOp:
			collectRefs(v.Operand, out)
		case ast.ConcatExpr:
			for _, p := range v.Parts { collectRefs(p, out) }
		case *ast.FunctionExpr:
			for _, stmt := range v.Body {
				// only shallow scan: we don't dive into nested function internals
				switch st := stmt.(type) {
				case ast.ExprStat:
					collectRefs(st.Expr, out)
				case ast.Assign:
					for _, val := range st.Values { collectRefs(val, out) }
				case ast.LocalAssign:
					for _, val := range st.Values { collectRefs(val, out) }
				}
			}
		case ast.FunctionExpr:
			for _, stmt := range v.Body {
				switch st := stmt.(type) {
				case ast.ExprStat:
					collectRefs(st.Expr, out)
				case ast.Assign:
					for _, val := range st.Values { collectRefs(val, out) }
				case ast.LocalAssign:
					for _, val := range st.Values { collectRefs(val, out) }
				}
			}
		}
	}

	// Build dependency graph among prefix locals based on references inside RHS
	n := len(assigns)
	nameToIdx := make(map[string]int)
	for i, name := range localNames { nameToIdx[name] = i }

	deps := make([]map[int]bool, n)
	for i := 0; i < n; i++ { deps[i] = make(map[int]bool) }

	for i, ls := range assigns {
		// consider only requires as primary reordering candidates
		isRequire := false
		if len(ls.Values) == 1 {
			if fc, ok := ls.Values[0].(ast.FunctionCall); ok {
				if gr, ok := fc.Func.(ast.GlobalRef); ok && gr.Name == "require" {
					isRequire = true
				}
			}
		}
		if !isRequire {
			continue
		}
		// collect refs inside RHS
		refs := map[string]bool{}
		collectRefs(ls.Values[0], refs)
		for r := range refs {
			if j, ok := nameToIdx[r]; ok && j != i {
				// i depends on j (j must come before i)
				deps[i][j] = true
			}
		}
	}

	// Kahn's algorithm for topological sort (on requires); keep non-require locals in-place
	indeg := make([]int, n)
	for i := 0; i < n; i++ {
		for range deps[i] { indeg[i]++ }
	}
	q := make([]int, 0)
	for i := 0; i < n; i++ {
		// nodes with zero indegree that are requires or non-requires
		if indeg[i] == 0 {
			q = append(q, i)
		}
	}
	order := make([]int, 0, n)
	for len(q) > 0 {
		v := q[0]
		q = q[1:]
		order = append(order, v)
		for i := 0; i < n; i++ {
			if deps[i][v] {
				indeg[i]--
				delete(deps[i], v)
				if indeg[i] == 0 {
					q = append(q, i)
				}
			}
		}
	}

	if len(order) != n {
		// cycle detected or sort failed — fallback to original prefix order
		out := make([]ast.Stmt, 0, len(body))
		out = append(out, prefix...)
		out = append(out, rest...)
		return out
	}

	out := make([]ast.Stmt, 0, len(body))
	for _, idx := range order {
		out = append(out, prefix[idx])
	}
	out = append(out, rest...)
	return out
}

func (l *Lifter) isRegLiveLater(proto *bc.Proto, reg, pc int) bool {
	for _, inst := range proto.Instructions {
		if inst.PC <= pc {
			continue
		}
		if instructionReadsReg(inst, reg) {
			return true
		}
		if instructionWritesReg(inst, reg) {
			break
		}
	}
	return false
}

// isRegCapturedLater returns true if a CAPTURE instruction referring to
// the given register appears after the provided program counter.
func (l *Lifter) isRegCapturedLater(proto *bc.Proto, reg, pc int) bool {
	for _, inst := range proto.Instructions {
		if inst.PC <= pc {
			continue
		}
		if inst.Opcode == bc.OpCAPTURE {
			if inst.B == reg {
				return true
			}
		}
	}
	return false
}

// hasAnyCapture returns true if any CAPTURE instruction in the proto
// references the given register (conservative check used to avoid
// splitting live ranges that are captured by closures).
func (l *Lifter) hasAnyCapture(proto *bc.Proto, reg int) bool {
	for _, inst := range proto.Instructions {
		if inst.Opcode == bc.OpCAPTURE {
			if inst.B == reg {
				return true
			}
		}
	}
	return false
}

// mergeAdjacentIfs looks for consecutive IfStat statements with identical
// bodies and merges their conditions with logical OR to restore patterns
// that were split across multiple conditional jumps in bytecode.
func mergeAdjacentIfs(stmts []ast.Stmt) []ast.Stmt {
	if len(stmts) < 2 {
		return stmts
	}
	out := make([]ast.Stmt, 0, len(stmts))
	i := 0
	for i < len(stmts) {
		cur := stmts[i]
		if if1, ok := cur.(ast.IfStat); ok && i+1 < len(stmts) {
			if2, ok2 := stmts[i+1].(ast.IfStat)
			if ok2 {
				// compare ThenBody and ElseBody shallowly via reflect
				if reflect.DeepEqual(if1.ThenBody, if2.ThenBody) && reflect.DeepEqual(if1.ElseBody, if2.ElseBody) {
					// merge conditions: if (c1) then B end; if (c2) then B end -> if (c1 or c2) then B end
					mergedCond := ast.BinaryOp{Op: "or", Left: if1.Condition, Right: if2.Condition}
					newIf := ast.IfStat{Condition: mergedCond, ThenBody: if1.ThenBody, ElseBody: if1.ElseBody}
					out = append(out, newIf)
					i += 2
					continue
				}
			}
		}
		out = append(out, cur)
		i++
	}
	return out
}

func (l *Lifter) bumpNameVersion(reg int) int {
	v := l.nameVersion[reg] + 1
	l.nameVersion[reg] = v
	return v
}

func (l *Lifter) mergeAdjacentIfs(stmts []ast.Stmt) []ast.Stmt {
	// recursively process nested statement lists
	var walk func([]ast.Stmt) []ast.Stmt
	walk = func(list []ast.Stmt) []ast.Stmt {
		out := make([]ast.Stmt, 0, len(list))
		i := 0
		for i < len(list) {
			s := list[i]
			// recurse into function bodies and if branches
			switch t := s.(type) {
			case ast.IfStat:
				// process children first
				t.ThenBody = walk(t.ThenBody)
				for idx := range t.ElseifClauses { t.ElseifClauses[idx].Body = walk(t.ElseifClauses[idx].Body) }
				t.ElseBody = walk(t.ElseBody)
				// Attempt to merge with following IfStat(s) that have identical bodies
				j := i + 1
				combined := t
				for j < len(list) {
					next, ok := list[j].(ast.IfStat)
					if !ok {
						break
					}
					// only merge simple ifs without elseif/else and identical ThenBody
					if len(combined.ElseifClauses) != 0 || len(combined.ElseBody) != 0 ||
						len(next.ElseifClauses) != 0 || len(next.ElseBody) != 0 {
						break
					}
					if !reflect.DeepEqual(combined.ThenBody, next.ThenBody) {
						break
					}
					// merge conditions: cond = combined.Cond or next.Cond
					combined.Condition = ast.BinaryOp{Op: "or", Left: combined.Condition, Right: next.Condition}
					j++
				}
				out = append(out, combined)
				i = j
			case ast.ExprStat:
				out = append(out, t)
			case ast.LocalAssign:
				out = append(out, t)
			case ast.Assign:
				out = append(out, t)
			case ast.ReturnStat:
				out = append(out, t)
			// other statements: just append (nested blocks handled above for IfStat)
			default:
				out = append(out, t)
			}
		}
		return out
	}
	return walk(stmts)
}

// propagateCopies removes trivial MOVE aliasing by inlining source locals
// into subsequent uses until the target is reassigned.
func (l *Lifter) propagateCopies(stmts []ast.Stmt) []ast.Stmt {
	mapping := make(map[string]ast.Expr)
	out := make([]ast.Stmt, 0, len(stmts))

	for _, s := range stmts {
		s = replaceStmtExprs(s, mapping)

		// After replacement, update mapping based on the statement
		switch t := s.(type) {
		case ast.LocalAssign:
			// if single-name trivial copy, record mapping
			if len(t.Names) == 1 && len(t.Values) == 1 {
				if isTrivialExpr(t.Values[0]) {
					if lv, ok := t.Values[0].(ast.LocalVar); ok {
						mapping[t.Names[0]] = lv
					} else if uv, ok := t.Values[0].(ast.UpvalueRef); ok {
						mapping[t.Names[0]] = uv
					}
				}
			}
			// if local assigned, it shadows any previous mapping for that name
			for _, n := range t.Names { delete(mapping, n) }
		case ast.Assign:
			// remove mappings for assigned targets
			for _, tgt := range t.Targets {
				if lv, ok := tgt.(ast.LocalVar); ok {
					delete(mapping, lv.Name)
				}
			}
		case ast.ReturnStat:
			// conservative: clear mapping on returns
			mapping = make(map[string]ast.Expr)
		}

		out = append(out, s)
	}
	return out
}

func replaceStmtExprs(s ast.Stmt, mapping map[string]ast.Expr) ast.Stmt {
	switch t := s.(type) {
	case ast.LocalAssign:
		vals := make([]ast.Expr, 0, len(t.Values))
		for _, v := range t.Values { vals = append(vals, replaceExpr(v, mapping, 0)) }
		t.Values = vals
		return t
	case ast.Assign:
		vals := make([]ast.Expr, 0, len(t.Values))
		for _, v := range t.Values { vals = append(vals, replaceExpr(v, mapping, 0)) }
		t.Values = vals
		return t
	case ast.ExprStat:
		return ast.ExprStat{Expr: replaceExpr(t.Expr, mapping, 0)}
	case ast.IfStat:
		t.Condition = replaceExpr(t.Condition, mapping, 0)
		for i := range t.ThenBody { t.ThenBody[i] = replaceStmtExprs(t.ThenBody[i], mapping) }
		for i := range t.ElseBody { t.ElseBody[i] = replaceStmtExprs(t.ElseBody[i], mapping) }
		return t
	default:
		return s
	}
}

func replaceExpr(e ast.Expr, mapping map[string]ast.Expr, depth int) ast.Expr {
	if e == nil || depth > 10 {
		return e
	}
	switch v := e.(type) {
	case ast.LocalVar:
		if rep, ok := mapping[v.Name]; ok {
			// avoid infinite chains
			if lv, ok2 := rep.(ast.LocalVar); ok2 && lv.Name == v.Name {
				return v
			}
			return replaceExpr(rep, mapping, depth+1)
		}
		return v
	case ast.UpvalueRef:
		if rep, ok := mapping[v.Name]; ok {
			return replaceExpr(rep, mapping, depth+1)
		}
		return v
	case *ast.TableConstructor:
		for i := range v.Fields { v.Fields[i].Value = replaceExpr(v.Fields[i].Value, mapping, depth+1) }
		return v
	case ast.TableConstructor:
		for i := range v.Fields { v.Fields[i].Value = replaceExpr(v.Fields[i].Value, mapping, depth+1) }
		return v
	case ast.MethodCall:
		v.Obj = replaceExpr(v.Obj, mapping, depth+1)
		for i := range v.Args { v.Args[i] = replaceExpr(v.Args[i], mapping, depth+1) }
		return v
	case ast.FunctionCall:
		v.Func = replaceExpr(v.Func, mapping, depth+1)
		for i := range v.Args { v.Args[i] = replaceExpr(v.Args[i], mapping, depth+1) }
		return v
	case ast.IndexExpr:
		v.Obj = replaceExpr(v.Obj, mapping, depth+1)
		v.Key = replaceExpr(v.Key, mapping, depth+1)
		return v
	case ast.BinaryOp:
		v.Left = replaceExpr(v.Left, mapping, depth+1)
		v.Right = replaceExpr(v.Right, mapping, depth+1)
		return v
	case ast.UnaryOp:
		v.Operand = replaceExpr(v.Operand, mapping, depth+1)
		return v
	case ast.ConcatExpr:
		for i := range v.Parts { v.Parts[i] = replaceExpr(v.Parts[i], mapping, depth+1) }
		return v
	case *ast.FunctionExpr:
		// shallow: replace in body statements
		for i := range v.Body { v.Body[i] = replaceStmtExprs(v.Body[i], mapping) }
		return v
	case ast.FunctionExpr:
		for i := range v.Body { v.Body[i] = replaceStmtExprs(v.Body[i], mapping) }
		return v
	default:
		return e
	}
}

// dedupeTableFieldsInStmts traverses statements and removes duplicate
// entries inside table constructors (keeps first occurrence). This is a
// defensive pass against accidentally duplicated upvalue/local references
// showing up in table literals passed as args (e.g. ListenerAdded payloads).
func dedupeTableFieldsInStmts(stmts []ast.Stmt) []ast.Stmt {
	for i, s := range stmts {
		stmts[i] = dedupeStmt(s)
	}
	return stmts
}

func dedupeStmt(s ast.Stmt) ast.Stmt {
	switch t := s.(type) {
	case ast.LocalAssign:
		vals := make([]ast.Expr, 0, len(t.Values))
		for _, v := range t.Values {
			vals = append(vals, dedupeExpr(v))
		}
		t.Values = vals
		return t
	case ast.Assign:
		vals := make([]ast.Expr, 0, len(t.Values))
		for _, v := range t.Values {
			vals = append(vals, dedupeExpr(v))
		}
		t.Values = vals
		return t
	case ast.ExprStat:
		return ast.ExprStat{Expr: dedupeExpr(t.Expr)}
	case ast.IfStat:
		for i := range t.ThenBody { t.ThenBody[i] = dedupeStmt(t.ThenBody[i]) }
		for i := range t.ElseBody { t.ElseBody[i] = dedupeStmt(t.ElseBody[i]) }
		return t
	case ast.DoBlock:
		for i := range t.Body { t.Body[i] = dedupeStmt(t.Body[i]) }
		return t
	default:
		return s
	}
}

func dedupeExpr(e ast.Expr) ast.Expr {
	switch v := e.(type) {
	case *ast.TableConstructor:
		return dedupeTable(v)
	case ast.TableConstructor:
		tc := v
		tptr := &tc
		return dedupeTable(tptr)
	case ast.MethodCall:
		args := make([]ast.Expr, 0, len(v.Args))
		seen := make([]ast.Expr, 0, len(v.Args))
		for _, a := range v.Args {
			da := dedupeExpr(a)
			// simple dedupe: skip if identical (compare basic types)
			dup := false
			for _, s := range seen {
				if simpleExprEqual(s, da) { dup = true; break }
			}
			if !dup {
				args = append(args, da)
				seen = append(seen, da)
			}
		}
		v.Args = args
		return v
	case ast.FunctionCall:
		args := make([]ast.Expr, 0, len(v.Args))
		for _, a := range v.Args { args = append(args, dedupeExpr(a)) }
		v.Args = args
		return v
	default:
		return e
	}
}

func dedupeTable(tc *ast.TableConstructor) ast.Expr {
	out := &ast.TableConstructor{Fields: make([]ast.TableField, 0, len(tc.Fields))}
	seen := make([]ast.Expr, 0, len(tc.Fields))
	for _, f := range tc.Fields {
		v := dedupeExpr(f.Value)
		dup := false
		for _, s := range seen {
			if simpleExprEqual(s, v) { dup = true; break }
		}
		if !dup {
			// Targeted fix: if the value is a nested table constructor, and its
			// last element is a LocalVar/UpvalueRef that matches the receiver
			// of a MethodCall inside the same nested table (e.g., {"X", r:GetIndex(), r}),
			// drop that trailing receiver entry.
			if nt, ok := v.(*ast.TableConstructor); ok {
				if len(nt.Fields) > 0 {
					inner := nt.Fields
					// find any method call receivers inside inner
					recvNames := map[string]bool{}
					for _, iv := range inner {
						if mc, ok := iv.Value.(ast.MethodCall); ok {
							switch obj := mc.Obj.(type) {
							case ast.LocalVar:
								recvNames[obj.Name] = true
							case ast.UpvalueRef:
								recvNames[obj.Name] = true
							}
						}
					}
					// check last field
					last := inner[len(inner)-1].Value
					if lv, ok := last.(ast.LocalVar); ok {
						if recvNames[lv.Name] {
							// drop last
							nt.Fields = nt.Fields[:len(nt.Fields)-1]
							v = nt
						}
					} else if uv, ok := last.(ast.UpvalueRef); ok {
						if recvNames[uv.Name] {
							nt.Fields = nt.Fields[:len(nt.Fields)-1]
							v = nt
						}
					}
				}
			}
			seen = append(seen, v)
			out.Fields = append(out.Fields, ast.TableField{Key: f.Key, Value: v, IsStringKey: f.IsStringKey})
		}
	}
	return out
}

func simpleExprEqual(a, b ast.Expr) bool {
	switch x := a.(type) {
	case ast.LocalVar:
		if y, ok := b.(ast.LocalVar); ok { return x.Name == y.Name }
	case ast.UpvalueRef:
		if y, ok := b.(ast.UpvalueRef); ok { return x.Name == y.Name }
	case ast.StringLiteral:
		if y, ok := b.(ast.StringLiteral); ok { return x.Value == y.Value }
	case ast.NumberLiteral:
		if y, ok := b.(ast.NumberLiteral); ok { return x.Value == y.Value }
	}
	return false
}

// blockState carries mutable register/name state through block lifting.
type blockState struct {
	regs                map[int]ast.Expr
	declaredRegs        map[int]bool
	regNames            map[int]string
	tableBuilders       map[int]*ast.TableConstructor
	sealedTableBuilders map[int]bool
	upvalues            []ast.Expr
	trackedRegs         map[int]int
}

func (s *blockState) clone() *blockState {
	newRegs := make(map[int]ast.Expr, len(s.regs))
	for k, v := range s.regs {
		newRegs[k] = v
	}
	newDecl := make(map[int]bool, len(s.declaredRegs))
	for k, v := range s.declaredRegs {
		newDecl[k] = v
	}
	newTB := make(map[int]*ast.TableConstructor, len(s.tableBuilders))
	for k, v := range s.tableBuilders {
		newTB[k] = v
	}
	return &blockState{
		regs: newRegs, declaredRegs: newDecl,
		regNames: s.regNames, tableBuilders: newTB,
		sealedTableBuilders: s.sealedTableBuilders,
		upvalues: s.upvalues, trackedRegs: s.trackedRegs,
	}
}
