package lifter

import (
	"reflect"

	"Geckocompiler/ast"
)

// foldBooleanAST performs peephole optimization on if statements,
// folding nested ifs into boolean 'and' expressions and merging
// adjacent ifs with identical bodies into 'or' expressions.
func foldBooleanAST(stmts []ast.Stmt) []ast.Stmt {
	folded := make([]ast.Stmt, 0, len(stmts))
	for _, stmt := range stmts {
		ifSt, ok := stmt.(ast.IfStat)
		if !ok {
			folded = append(folded, stmt)
			continue
		}

		// Recursively fold interior blocks
		ifSt.ThenBody = foldBooleanAST(ifSt.ThenBody)
		ifSt.ElseBody = foldBooleanAST(ifSt.ElseBody)
		normalizedElseifs := make([]ast.ElseifClause, 0, len(ifSt.ElseifClauses))
		for _, ec := range ifSt.ElseifClauses {
			normalizedElseifs = append(normalizedElseifs, ast.ElseifClause{
				Condition: canonicalizeBooleanExpr(ec.Condition),
				Body:      foldBooleanAST(ec.Body),
			})
		}
		ifSt.ElseifClauses = normalizedElseifs

		// 'and' folding: if A then if B then ... end end → if A and B then ... end
		for {
			if len(ifSt.ThenBody) != 1 {
				break
			}
			inner, ok := ifSt.ThenBody[0].(ast.IfStat)
			if !ok {
				break
			}
			if len(ifSt.ElseBody) > 0 || len(ifSt.ElseifClauses) > 0 ||
				len(inner.ElseBody) > 0 || len(inner.ElseifClauses) > 0 {
				break
			}
			ifSt.Condition = ast.BinaryOp{Op: "and", Left: ifSt.Condition, Right: inner.Condition}
			ifSt.ThenBody = inner.ThenBody
		}

		ifSt.Condition = canonicalizeBooleanExpr(ifSt.Condition)
		ifSt = collapseElseifChain(ifSt)
		folded = append(folded, ifSt)
	}

	// 'or' folding: adjacent ifs with identical bodies
	i := 0
	for i < len(folded)-1 {
		curr, okC := folded[i].(ast.IfStat)
		nxt, okN := folded[i+1].(ast.IfStat)
		if okC && okN &&
			len(curr.ElseBody) == 0 && len(curr.ElseifClauses) == 0 &&
			len(nxt.ElseBody) == 0 && len(nxt.ElseifClauses) == 0 &&
			areNodesIdentical(curr.ThenBody, nxt.ThenBody) {
			curr.Condition = canonicalizeBooleanExpr(
				ast.BinaryOp{Op: "or", Left: curr.Condition, Right: nxt.Condition},
			)
			folded[i] = curr
			folded = append(folded[:i+1], folded[i+2:]...)
			continue
		}
		i++
	}
	return folded
}

func collapseElseifChain(stmt ast.IfStat) ast.IfStat {
	for len(stmt.ElseBody) == 1 {
		nested, ok := stmt.ElseBody[0].(ast.IfStat)
		if !ok {
			break
		}
		stmt.ElseifClauses = append(stmt.ElseifClauses, ast.ElseifClause{
			Condition: nested.Condition,
			Body:      nested.ThenBody,
		})
		stmt.ElseifClauses = append(stmt.ElseifClauses, nested.ElseifClauses...)
		stmt.ElseBody = nested.ElseBody
	}
	return stmt
}

func canonicalizeBooleanExpr(expr ast.Expr) ast.Expr {
	switch e := expr.(type) {
	case ast.BinaryOp:
		left := canonicalizeBooleanExpr(e.Left)
		right := canonicalizeBooleanExpr(e.Right)
		result := ast.BinaryOp{Op: e.Op, Left: left, Right: right}

		if areNodesIdentical(left, right) {
			return left
		}
		if e.Op == "or" {
			if simplified := simplifyAbsorption(left, right, "and"); simplified != nil {
				return simplified
			}
		}
		if e.Op == "and" {
			if simplified := simplifyAbsorption(left, right, "or"); simplified != nil {
				return simplified
			}
		}
		return result

	case ast.UnaryOp:
		return ast.UnaryOp{Op: e.Op, Operand: canonicalizeBooleanExpr(e.Operand)}
	}
	return expr
}

func simplifyAbsorption(left, right ast.Expr, nestedOp string) ast.Expr {
	for _, node := range flattenBooleanChain(left, nestedOp) {
		if areNodesIdentical(node, right) {
			return right
		}
	}
	for _, node := range flattenBooleanChain(right, nestedOp) {
		if areNodesIdentical(node, left) {
			return left
		}
	}
	return nil
}

func flattenBooleanChain(expr ast.Expr, op string) []ast.Expr {
	if bo, ok := expr.(ast.BinaryOp); ok && bo.Op == op {
		left := flattenBooleanChain(bo.Left, op)
		right := flattenBooleanChain(bo.Right, op)
		return append(left, right...)
	}
	return []ast.Expr{expr}
}

// sameExpr does a shallow structural equality check on two expressions.
func sameExpr(a, b ast.Expr) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return reflect.DeepEqual(a, b)
}

// areNodesIdentical does a deep structural equality check.
func areNodesIdentical(a, b interface{}) bool {
	return reflect.DeepEqual(a, b)
}
