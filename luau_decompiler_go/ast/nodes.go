// Package ast defines all AST node types used by the Luau decompiler to
// represent high-level Luau constructs reconstructed from bytecode.
package ast

// Node is the marker interface for all AST nodes.
type Node interface{ isNode() }

// Expr is the marker interface for expression nodes.
type Expr interface {
	Node
	isExpr()
}

// Stmt is the marker interface for statement nodes.
type Stmt interface {
	Node
	isStmt()
}

// --- Expression Nodes ---

type NilLiteral struct{}

func (NilLiteral) isNode() {}
func (NilLiteral) isExpr() {}

type BoolLiteral struct{ Value bool }

func (BoolLiteral) isNode() {}
func (BoolLiteral) isExpr() {}

type NumberLiteral struct{ Value float64 }

func (NumberLiteral) isNode() {}
func (NumberLiteral) isExpr() {}

type StringLiteral struct{ Value string }

func (StringLiteral) isNode() {}
func (StringLiteral) isExpr() {}

type VarArgExpr struct{}

func (VarArgExpr) isNode() {}
func (VarArgExpr) isExpr() {}

type LocalVar struct {
	Name string
	Reg  int
}

func (LocalVar) isNode() {}
func (LocalVar) isExpr() {}

type UpvalueRef struct {
	Name  string
	Index int
}

func (UpvalueRef) isNode() {}
func (UpvalueRef) isExpr() {}

type GlobalRef struct{ Name string }

func (GlobalRef) isNode() {}
func (GlobalRef) isExpr() {}

// IndexExpr represents obj[key] or obj.key (when IsDot is true).
type IndexExpr struct {
	Obj   Expr
	Key   Expr
	IsDot bool
}

func (IndexExpr) isNode() {}
func (IndexExpr) isExpr() {}

// MethodCall represents obj:method(args).
type MethodCall struct {
	Obj    Expr
	Method string
	Args   []Expr
}

func (MethodCall) isNode() {}
func (MethodCall) isExpr() {}

// FunctionCall represents func(args).
type FunctionCall struct {
	Func       Expr
	Args       []Expr
	NumReturns int // -1 = unknown/vararg
}

func (FunctionCall) isNode() {}
func (FunctionCall) isExpr() {}

// BinaryOp represents left op right.
type BinaryOp struct {
	Op    string // +, -, *, /, //, %, ^, .., ==, ~=, <, <=, >, >=, and, or
	Left  Expr
	Right Expr
}

func (BinaryOp) isNode() {}
func (BinaryOp) isExpr() {}

// UnaryOp represents op operand (-, not, #).
type UnaryOp struct {
	Op      string
	Operand Expr
}

func (UnaryOp) isNode() {}
func (UnaryOp) isExpr() {}

// ConcatExpr represents a .. b .. c.
type ConcatExpr struct{ Parts []Expr }

func (ConcatExpr) isNode() {}
func (ConcatExpr) isExpr() {}

// TableConstructor represents { fields... }.
type TableConstructor struct{ Fields []TableField }

func (TableConstructor) isNode() {}
func (TableConstructor) isExpr() {}

// TableField represents a single field in a table constructor.
type TableField struct {
	Key         Expr // nil for array-style
	Value       Expr
	IsStringKey bool // true for {key = val}
}

// FunctionExpr represents function(params) ... end.
type FunctionExpr struct {
	Params   []string
	IsVararg bool
	Body     []Stmt
	Name     string // for named functions (empty if anonymous)
}

func (FunctionExpr) isNode() {}
func (FunctionExpr) isExpr() {}

// IfExpr represents inline: if cond then expr1 else expr2.
type IfExpr struct {
	Condition Expr
	ThenExpr  Expr
	ElseExpr  Expr
}

func (IfExpr) isNode() {}
func (IfExpr) isExpr() {}

// --- Statement Nodes ---

// LocalAssign represents: local x, y = expr1, expr2.
type LocalAssign struct {
	Names  []string
	Values []Expr
}

func (LocalAssign) isNode() {}
func (LocalAssign) isStmt() {}

// Assign represents: x, y = expr1, expr2.
type Assign struct {
	Targets []Expr
	Values  []Expr
}

func (Assign) isNode() {}
func (Assign) isStmt() {}

// ReturnStat represents: return expr1, expr2.
type ReturnStat struct{ Values []Expr }

func (ReturnStat) isNode() {}
func (ReturnStat) isStmt() {}

// ExprStat wraps a standalone expression used as a statement (usually a call).
type ExprStat struct{ Expr Expr }

func (ExprStat) isNode() {}
func (ExprStat) isStmt() {}

// IfStat represents: if cond then ... elseif ... else ... end.
type IfStat struct {
	Condition     Expr
	ThenBody      []Stmt
	ElseifClauses []ElseifClause
	ElseBody      []Stmt
}

func (IfStat) isNode() {}
func (IfStat) isStmt() {}

// ElseifClause holds one elseif condition + body pair.
type ElseifClause struct {
	Condition Expr
	Body      []Stmt
}

// WhileStat represents: while cond do ... end.
type WhileStat struct {
	Condition Expr
	Body      []Stmt
}

func (WhileStat) isNode() {}
func (WhileStat) isStmt() {}

// RepeatStat represents: repeat ... until cond.
type RepeatStat struct {
	Condition Expr
	Body      []Stmt
}

func (RepeatStat) isNode() {}
func (RepeatStat) isStmt() {}

// NumericForStat represents: for i = start, limit, step do ... end.
type NumericForStat struct {
	VarName string
	Start   Expr
	Limit   Expr
	Step    Expr
	Body    []Stmt
}

func (NumericForStat) isNode() {}
func (NumericForStat) isStmt() {}

// GenericForStat represents: for k, v in iter do ... end.
type GenericForStat struct {
	VarNames  []string
	Iterators []Expr
	Body      []Stmt
}

func (GenericForStat) isNode() {}
func (GenericForStat) isStmt() {}

type BreakStat struct{}

func (BreakStat) isNode() {}
func (BreakStat) isStmt() {}

type ContinueStat struct{}

func (ContinueStat) isNode() {}
func (ContinueStat) isStmt() {}

// DoBlock represents: do ... end.
type DoBlock struct{ Body []Stmt }

func (DoBlock) isNode() {}
func (DoBlock) isStmt() {}

// Comment represents: -- text.
type Comment struct{ Text string }

func (Comment) isNode() {}
func (Comment) isStmt() {}
