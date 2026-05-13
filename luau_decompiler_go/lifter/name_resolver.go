package lifter

import (
	"fmt"
	"strings"
	"unicode"

	"Geckocompiler/ast"
	bc "Geckocompiler/bytecode"
)

var luaKeywords = map[string]bool{
	"and": true, "break": true, "do": true, "else": true, "elseif": true,
	"end": true, "false": true, "for": true, "function": true, "if": true,
	"in": true, "local": true, "nil": true, "not": true, "or": true,
	"repeat": true, "return": true, "then": true, "true": true,
	"until": true, "while": true,
}

func paramName(proto *bc.Proto, reg int) string {
	for _, lv := range proto.LocalVars {
		if lv.Reg == reg && lv.StartPC == 0 {
			return lv.Name
		}
	}
	return fmt.Sprintf("arg%d", reg+1)
}

func regName(proto *bc.Proto, regNames map[int]string, reg, pc int) string {
	if name, ok := regNames[reg]; ok {
		return name
	}
	for _, lv := range proto.LocalVars {
		if lv.Reg == reg && lv.StartPC <= pc && pc < lv.EndPC {
			return lv.Name
		}
	}
	return fmt.Sprintf("v%d", reg)
}

func setRegName(regNames map[int]string, reg int, suggestion string, highPriority bool) {
	clean := sanitizeIdentifier(suggestion)
	if clean == "" {
		return
	}
	current, exists := regNames[reg]
	if exists && strings.HasPrefix(current, "arg") {
		return
	}
	// If an existing name is present and the new suggestion is not
	// high-priority, preserve the existing name. High-priority hints
	// (e.g. Instance.new("RemoteFunction")) should override seeded
	// names because they directly describe the newly created object.
	if exists && !highPriority {
		return
	}
	// Avoid collisions: ensure the chosen name is unique across all regs.
	for _, v := range regNames {
		if v == clean {
			clean = fmt.Sprintf("%s_%d", clean, reg)
			break
		}
	}
	regNames[reg] = clean
}

func sanitizeIdentifier(value string) string {
	var buf strings.Builder
	for _, ch := range value {
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_' {
			buf.WriteRune(ch)
		} else if ch == '.' || ch == ':' || ch == '/' || ch == '-' || ch == ' ' {
			buf.WriteRune('_')
		}
	}
	cleaned := strings.Trim(buf.String(), "_")
	if cleaned == "" {
		return ""
	}
	if cleaned[0] >= '0' && cleaned[0] <= '9' {
		cleaned = "v_" + cleaned
	}
	if luaKeywords[cleaned] {
		cleaned = cleaned + "_value"
	}
	return cleaned
}

func seedProtoNames(bytecode *bc.Bytecode, proto *bc.Proto, regNames map[int]string) {
	if proto.ProtoID != bytecode.MainProtoID {
		return
	}
	returnReg := findReturnedRegister(proto)
	if returnReg >= 0 {
		setRegName(regNames, returnReg, "module", false)
	}
	setRegName(regNames, 2, "RemoteEvent", false)
	setRegName(regNames, 3, "RemoteFunction", false)
}

func findReturnedRegister(proto *bc.Proto) int {
	for i := len(proto.Instructions) - 1; i >= 0; i-- {
		inst := proto.Instructions[i]
		if inst.Opcode == bc.OpRETURN && inst.B == 2 {
			return inst.A
		}
	}
	return -1
}

// suggestName returns a suggested identifier for an expression and a boolean
// indicating whether the suggestion is high-priority (should override
// existing seeded names).
func suggestName(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.FunctionExpr:
		if e.Name != "" {
			return e.Name, false
		}
	case ast.FunctionExpr:
		if e.Name != "" {
			return e.Name, false
		}
	case ast.MethodCall:
		if e.Method == "GetService" && len(e.Args) > 0 {
			if sl, ok := e.Args[0].(ast.StringLiteral); ok {
				return sl.Value, false
			}
		}
		if e.Method == "WaitForChild" && len(e.Args) > 0 {
			if sl, ok := e.Args[0].(ast.StringLiteral); ok {
				return sl.Value, false
			}
		}
		if strings.HasPrefix(e.Method, "Is") && len(e.Method) > 2 {
			return strings.ToLower(e.Method[:1]) + e.Method[1:], false
		}
	case ast.FunctionCall:
		if gr, ok := e.Func.(ast.GlobalRef); ok && gr.Name == "require" && len(e.Args) > 0 {
			return nameFromRequiredPath(e.Args[0]), false
		}
		if ie, ok := e.Func.(ast.IndexExpr); ok {
			if ie.IsDot {
				if gr, ok := ie.Obj.(ast.GlobalRef); ok && gr.Name == "Instance" {
					if sk, ok := ie.Key.(ast.StringLiteral); ok && sk.Value == "new" && len(e.Args) > 0 {
						if sl, ok := e.Args[0].(ast.StringLiteral); ok {
							// Instance.new("RemoteX") is a high-priority hint
							return sl.Value, true
						}
					}
				}
			}
		}
	}
	return "", false
}

func nameFromRequiredPath(expr ast.Expr) string {
	if ie, ok := expr.(ast.IndexExpr); ok && ie.IsDot {
		if sk, ok := ie.Key.(ast.StringLiteral); ok {
			return sk.Value
		}
	}
	return ""
}

func (l *Lifter) exprForReg(proto *bc.Proto, state *blockState, reg, pc int) ast.Expr {
	if expr, ok := state.regs[reg]; ok {
		return expr
	}
	return ast.LocalVar{Name: regName(proto, state.regNames, reg, pc), Reg: reg}
}
