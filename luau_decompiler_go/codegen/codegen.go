// Package codegen converts AST nodes back into readable Luau source code.
package codegen

import (
	"fmt"
	"math"
	"strings"

	"Geckocompiler/ast"
)

// CodeGen transforms AST nodes into Luau source text.
type CodeGen struct {
	indentLevel int
	indentStr   string
}

// NewCodeGen creates a CodeGen with tab indentation.
func NewCodeGen() *CodeGen {
	return &CodeGen{indentStr: "\t"}
}

// Generate emits the full source for a top-level FunctionExpr.
func (g *CodeGen) Generate(node ast.Expr) string {
	if fe, ok := node.(*ast.FunctionExpr); ok && fe.Name == "" {
		lines := make([]string, 0, len(fe.Body))
		for _, stmt := range fe.Body {
			lines = append(lines, g.emitStmt(stmt))
		}
		return strings.Join(lines, "\n")
	}
	return g.emitExpr(node)
}

func (g *CodeGen) indent() string {
	return strings.Repeat(g.indentStr, g.indentLevel)
}

// --- Statement Emission ---

func (g *CodeGen) emitStmt(node ast.Stmt) string {
	switch s := node.(type) {
	case ast.LocalAssign:
		return g.emitLocalAssign(s)
	case ast.Assign:
		return g.emitAssign(s)
	case ast.ReturnStat:
		return g.emitReturn(s)
	case ast.ExprStat:
		return fmt.Sprintf("%s%s", g.indent(), g.emitExpr(s.Expr))
	case ast.IfStat:
		return g.emitIf(s)
	case ast.WhileStat:
		return g.emitWhile(s)
	case ast.NumericForStat:
		return g.emitNumericFor(s)
	case ast.GenericForStat:
		return g.emitGenericFor(s)
	case ast.DoBlock:
		return g.emitDoBlock(s)
	case ast.BreakStat:
		return g.indent() + "break"
	case ast.ContinueStat:
		return g.indent() + "continue"
	case ast.Comment:
		return fmt.Sprintf("%s-- %s", g.indent(), s.Text)
	case ast.RepeatStat:
		return g.emitRepeat(s)
	}
	return fmt.Sprintf("%s-- ?? %T", g.indent(), node)
}

// --- Expression Emission ---

func (g *CodeGen) emitExpr(node ast.Expr) string {
	if node == nil {
		return "nil"
	}
	switch e := node.(type) {
	case ast.NilLiteral:
		return "nil"
	case ast.BoolLiteral:
		if e.Value {
			return "true"
		}
		return "false"
	case ast.NumberLiteral:
		return formatNumber(e.Value)
	case ast.StringLiteral:
		return escapeString(e.Value)
	case ast.VarArgExpr:
		return "..."
	case ast.LocalVar:
		return e.Name
	case ast.UpvalueRef:
		return e.Name
	case ast.GlobalRef:
		return e.Name
	case ast.IndexExpr:
		return g.emitIndex(e)
	case ast.MethodCall:
		return g.emitMethodCall(e)
	case ast.FunctionCall:
		return g.emitFunctionCall(e)
	case ast.BinaryOp:
		return g.emitBinaryOp(e)
	case ast.UnaryOp:
		return g.emitUnaryOp(e)
	case ast.ConcatExpr:
		parts := make([]string, 0, len(e.Parts))
		for _, p := range e.Parts {
			parts = append(parts, g.emitExpr(p))
		}
		return strings.Join(parts, " .. ")
	case *ast.TableConstructor:
		return g.emitTable(e)
	case ast.TableConstructor:
		return g.emitTable(&e)
	case *ast.FunctionExpr:
		return g.emitFunctionExpr(e)
	case ast.FunctionExpr:
		return g.emitFunctionExpr(&e)
	case ast.IfExpr:
		return fmt.Sprintf("if %s then %s else %s",
			g.emitExpr(e.Condition), g.emitExpr(e.ThenExpr), g.emitExpr(e.ElseExpr))
	}
	return fmt.Sprintf("--[[unknown: %T]]", node)
}

func formatNumber(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%g", v)
}

func escapeString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	s = strings.ReplaceAll(s, "\x00", "\\0")
	return fmt.Sprintf("\"%s\"", s)
}

func (g *CodeGen) emitIndex(e ast.IndexExpr) string {
	obj := g.emitExpr(e.Obj)
	if e.IsDot {
		if sl, ok := e.Key.(ast.StringLiteral); ok {
			if isValidIdentifier(sl.Value) {
				return fmt.Sprintf("%s.%s", obj, sl.Value)
			}
			return fmt.Sprintf("%s[\"%s\"]", obj, sl.Value)
		}
	}
	return fmt.Sprintf("%s[%s]", obj, g.emitExpr(e.Key))
}

func isValidIdentifier(s string) bool {
	if s == "" || strings.HasPrefix(s, "__") {
		return false
	}
	for i, ch := range s {
		if i == 0 {
			if ch != '_' && (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') {
				return false
			}
		} else {
			if ch != '_' && (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') {
				return false
			}
		}
	}
	return true
}

func (g *CodeGen) emitMethodCall(e ast.MethodCall) string {
	obj := g.emitExpr(e.Obj)
	args := make([]string, 0, len(e.Args))
	for _, a := range e.Args {
		args = append(args, g.emitExpr(a))
	}
	return fmt.Sprintf("%s:%s(%s)", obj, e.Method, strings.Join(args, ", "))
}

func (g *CodeGen) emitFunctionCall(e ast.FunctionCall) string {
	fn := g.emitExpr(e.Func)
	args := make([]string, 0, len(e.Args))
	for _, a := range e.Args {
		args = append(args, g.emitExpr(a))
	}
	return fmt.Sprintf("%s(%s)", fn, strings.Join(args, ", "))
}

func (g *CodeGen) emitBinaryOp(e ast.BinaryOp) string {
	return fmt.Sprintf("(%s %s %s)", g.emitExpr(e.Left), e.Op, g.emitExpr(e.Right))
}

func (g *CodeGen) emitUnaryOp(e ast.UnaryOp) string {
	operand := g.emitExpr(e.Operand)
	switch e.Op {
	case "#":
		return "#" + operand
	case "-":
		return "-" + operand
	}
	return fmt.Sprintf("%s %s", e.Op, operand)
}

func (g *CodeGen) emitTable(tc *ast.TableConstructor) string {
	if len(tc.Fields) == 0 {
		return "{}"
	}
	parts := make([]string, 0, len(tc.Fields))
	for _, f := range tc.Fields {
		if f.Key == nil {
			parts = append(parts, g.emitExpr(f.Value))
		} else if f.IsStringKey {
			if sk, ok := f.Key.(ast.StringLiteral); ok {
				parts = append(parts, fmt.Sprintf("%s = %s", sk.Value, g.emitExpr(f.Value)))
			} else {
				parts = append(parts, fmt.Sprintf("[%s] = %s", g.emitExpr(f.Key), g.emitExpr(f.Value)))
			}
		} else {
			parts = append(parts, fmt.Sprintf("[%s] = %s", g.emitExpr(f.Key), g.emitExpr(f.Value)))
		}
	}
	if len(parts) <= 3 {
		return "{ " + strings.Join(parts, ", ") + " }"
	}
	inner := make([]string, 0, len(parts))
	for _, p := range parts {
		inner = append(inner, fmt.Sprintf("%s%s%s", g.indent(), g.indentStr, p))
	}
	return "{\n" + strings.Join(inner, ",\n") + "\n" + g.indent() + "}"
}

func (g *CodeGen) emitFunctionExpr(fe *ast.FunctionExpr) string {
	params := strings.Join(fe.Params, ", ")
	if fe.IsVararg {
		if params != "" {
			params += ", ..."
		} else {
			params = "..."
		}
	}
	if len(fe.Body) == 0 {
		return fmt.Sprintf("function(%s) end", params)
	}
	g.indentLevel++
	bodyLines := make([]string, 0, len(fe.Body))
	for _, stmt := range fe.Body {
		bodyLines = append(bodyLines, g.emitStmt(stmt))
	}
	g.indentLevel--
	return fmt.Sprintf("function(%s)\n%s\n%send", params, strings.Join(bodyLines, "\n"), g.indent())
}

func (g *CodeGen) emitLocalAssign(s ast.LocalAssign) string {
	// Special case: local function
	if len(s.Names) == 1 && len(s.Values) == 1 {
		if fe, ok := s.Values[0].(*ast.FunctionExpr); ok {
			return g.emitLocalFunction(s.Names[0], fe)
		}
		if fe, ok := s.Values[0].(ast.FunctionExpr); ok {
			return g.emitLocalFunction(s.Names[0], &fe)
		}
	}
	names := strings.Join(s.Names, ", ")
	if len(s.Values) > 0 {
		vals := make([]string, 0, len(s.Values))
		for _, v := range s.Values {
			vals = append(vals, g.emitExpr(v))
		}
		return fmt.Sprintf("%slocal %s = %s", g.indent(), names, strings.Join(vals, ", "))
	}
	return fmt.Sprintf("%slocal %s", g.indent(), names)
}

func (g *CodeGen) emitAssign(s ast.Assign) string {
	// Special case: named function assignment
	if len(s.Targets) == 1 && len(s.Values) == 1 {
		var fe *ast.FunctionExpr
		if f, ok := s.Values[0].(*ast.FunctionExpr); ok {
			fe = f
		} else if f, ok := s.Values[0].(ast.FunctionExpr); ok {
			fe = &f
		}
		if fe != nil {
			if result := g.emitNamedFunction(s.Targets[0], fe); result != "" {
				return result
			}
		}
	}
	targets := make([]string, 0, len(s.Targets))
	for _, t := range s.Targets {
		targets = append(targets, g.emitExpr(t))
	}
	vals := make([]string, 0, len(s.Values))
	for _, v := range s.Values {
		vals = append(vals, g.emitExpr(v))
	}
	return fmt.Sprintf("%s%s = %s", g.indent(), strings.Join(targets, ", "), strings.Join(vals, ", "))
}

func (g *CodeGen) emitLocalFunction(name string, fe *ast.FunctionExpr) string {
	params := strings.Join(fe.Params, ", ")
	if fe.IsVararg {
		if params != "" {
			params += ", ..."
		} else {
			params = "..."
		}
	}
	lines := []string{fmt.Sprintf("%slocal function %s(%s)", g.indent(), name, params)}
	g.indentLevel++
	for _, stmt := range fe.Body {
		lines = append(lines, g.emitStmt(stmt))
	}
	g.indentLevel--
	lines = append(lines, g.indent()+"end")
	return strings.Join(lines, "\n")
}

func (g *CodeGen) emitNamedFunction(target ast.Expr, fe *ast.FunctionExpr) string {
	ie, ok := target.(ast.IndexExpr)
	if !ok || !ie.IsDot {
		return ""
	}
	if _, ok := ie.Key.(ast.StringLiteral); !ok {
		return ""
	}
	params := strings.Join(fe.Params, ", ")
	if fe.IsVararg {
		if params != "" {
			params += ", ..."
		} else {
			params = "..."
		}
	}
	head := fmt.Sprintf("%sfunction %s(%s)", g.indent(), g.emitIndex(ie), params)
	lines := []string{head}
	g.indentLevel++
	for _, stmt := range fe.Body {
		lines = append(lines, g.emitStmt(stmt))
	}
	g.indentLevel--
	lines = append(lines, g.indent()+"end")
	return strings.Join(lines, "\n")
}

func (g *CodeGen) emitReturn(s ast.ReturnStat) string {
	if len(s.Values) == 0 {
		return g.indent() + "return"
	}
	vals := make([]string, 0, len(s.Values))
	for _, v := range s.Values {
		vals = append(vals, g.emitExpr(v))
	}
	return fmt.Sprintf("%sreturn %s", g.indent(), strings.Join(vals, ", "))
}

func (g *CodeGen) emitIf(s ast.IfStat) string {
	lines := []string{fmt.Sprintf("%sif %s then", g.indent(), g.emitExpr(s.Condition))}
	g.indentLevel++
	for _, stmt := range s.ThenBody {
		lines = append(lines, g.emitStmt(stmt))
	}
	g.indentLevel--
	for _, ec := range s.ElseifClauses {
		lines = append(lines, fmt.Sprintf("%selseif %s then", g.indent(), g.emitExpr(ec.Condition)))
		g.indentLevel++
		for _, stmt := range ec.Body {
			lines = append(lines, g.emitStmt(stmt))
		}
		g.indentLevel--
	}
	if len(s.ElseBody) > 0 {
		lines = append(lines, g.indent()+"else")
		g.indentLevel++
		for _, stmt := range s.ElseBody {
			lines = append(lines, g.emitStmt(stmt))
		}
		g.indentLevel--
	}
	lines = append(lines, g.indent()+"end")
	return strings.Join(lines, "\n")
}

func (g *CodeGen) emitWhile(s ast.WhileStat) string {
	lines := []string{fmt.Sprintf("%swhile %s do", g.indent(), g.emitExpr(s.Condition))}
	g.indentLevel++
	for _, stmt := range s.Body {
		lines = append(lines, g.emitStmt(stmt))
	}
	g.indentLevel--
	lines = append(lines, g.indent()+"end")
	return strings.Join(lines, "\n")
}

func (g *CodeGen) emitRepeat(s ast.RepeatStat) string {
	lines := []string{g.indent() + "repeat"}
	g.indentLevel++
	for _, stmt := range s.Body {
		lines = append(lines, g.emitStmt(stmt))
	}
	g.indentLevel--
	lines = append(lines, fmt.Sprintf("%suntil %s", g.indent(), g.emitExpr(s.Condition)))
	return strings.Join(lines, "\n")
}

func (g *CodeGen) emitNumericFor(s ast.NumericForStat) string {
	start := g.emitExpr(s.Start)
	limit := g.emitExpr(s.Limit)
	header := fmt.Sprintf("for %s = %s, %s", s.VarName, start, limit)
	if s.Step != nil {
		header += ", " + g.emitExpr(s.Step)
	}
	lines := []string{fmt.Sprintf("%s%s do", g.indent(), header)}
	g.indentLevel++
	for _, stmt := range s.Body {
		lines = append(lines, g.emitStmt(stmt))
	}
	g.indentLevel--
	lines = append(lines, g.indent()+"end")
	return strings.Join(lines, "\n")
}

func (g *CodeGen) emitGenericFor(s ast.GenericForStat) string {
	varsStr := strings.Join(s.VarNames, ", ")
	iters := make([]string, 0, len(s.Iterators))
	for _, it := range s.Iterators {
		iters = append(iters, g.emitExpr(it))
	}
	lines := []string{fmt.Sprintf("%sfor %s in %s do", g.indent(), varsStr, strings.Join(iters, ", "))}
	g.indentLevel++
	for _, stmt := range s.Body {
		lines = append(lines, g.emitStmt(stmt))
	}
	g.indentLevel--
	lines = append(lines, g.indent()+"end")
	return strings.Join(lines, "\n")
}

func (g *CodeGen) emitDoBlock(s ast.DoBlock) string {
	lines := []string{g.indent() + "do"}
	g.indentLevel++
	for _, stmt := range s.Body {
		lines = append(lines, g.emitStmt(stmt))
	}
	g.indentLevel--
	lines = append(lines, g.indent()+"end")
	return strings.Join(lines, "\n")
}
