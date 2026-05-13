"""
AST Node definitions for the Luau decompiler.
These represent the high-level Luau constructs we reconstruct from bytecode.
"""

from dataclasses import dataclass, field
from typing import List, Optional, Any, Union


# --- Expression Nodes ---

@dataclass
class NilLiteral:
    pass

@dataclass
class BoolLiteral:
    value: bool

@dataclass
class NumberLiteral:
    value: float

@dataclass
class StringLiteral:
    value: str

@dataclass
class VarArgExpr:
    pass

@dataclass
class LocalVar:
    name: str
    reg: int = -1

@dataclass
class UpvalueRef:
    name: str
    index: int = -1

@dataclass
class GlobalRef:
    name: str

@dataclass
class IndexExpr:
    """obj[key] or obj.key"""
    obj: Any
    key: Any
    is_dot: bool = False  # True for obj.key, False for obj[key]

@dataclass
class MethodCall:
    """obj:method(args)"""
    obj: Any
    method: str
    args: List[Any] = field(default_factory=list)

@dataclass
class FunctionCall:
    """func(args)"""
    func: Any
    args: List[Any] = field(default_factory=list)
    num_returns: int = -1  # -1 = unknown/vararg

@dataclass
class BinaryOp:
    op: str  # +, -, *, /, //, %, ^, .., ==, ~=, <, <=, >, >=, and, or
    left: Any
    right: Any

@dataclass
class UnaryOp:
    op: str  # -, not, #
    operand: Any

@dataclass
class ConcatExpr:
    """a .. b .. c (multiple concatenations)"""
    parts: List[Any] = field(default_factory=list)

@dataclass
class TableConstructor:
    """{ [key]=val, ... } or { val1, val2, ... }"""
    fields: List[Any] = field(default_factory=list)  # List of TableField

@dataclass
class TableField:
    key: Optional[Any] = None  # None for array-style
    value: Any = None
    is_string_key: bool = False  # True for {key = val}

@dataclass
class FunctionExpr:
    """function(params) ... end"""
    params: List[str] = field(default_factory=list)
    is_vararg: bool = False
    body: List[Any] = field(default_factory=list)  # List of statement nodes
    name: Optional[str] = None  # For named functions

@dataclass
class IfExpr:
    """if cond then expr1 else expr2"""
    condition: Any
    then_expr: Any
    else_expr: Any

# --- Statement Nodes ---

@dataclass
class LocalAssign:
    """local x, y = expr1, expr2"""
    names: List[str] = field(default_factory=list)
    values: List[Any] = field(default_factory=list)

@dataclass
class Assign:
    """x, y = expr1, expr2"""
    targets: List[Any] = field(default_factory=list)
    values: List[Any] = field(default_factory=list)

@dataclass
class ReturnStat:
    values: List[Any] = field(default_factory=list)

@dataclass
class ExprStat:
    """A standalone expression used as a statement (usually a function call)."""
    expr: Any = None

@dataclass
class IfStat:
    condition: Any = None
    then_body: List[Any] = field(default_factory=list)
    elseif_clauses: List[Any] = field(default_factory=list)  # List of (cond, body)
    else_body: List[Any] = field(default_factory=list)

@dataclass
class WhileStat:
    condition: Any = None
    body: List[Any] = field(default_factory=list)

@dataclass
class RepeatStat:
    condition: Any = None
    body: List[Any] = field(default_factory=list)

@dataclass
class NumericForStat:
    var_name: str = "i"
    start: Any = None
    limit: Any = None
    step: Any = None
    body: List[Any] = field(default_factory=list)

@dataclass
class GenericForStat:
    var_names: List[str] = field(default_factory=list)
    iterators: List[Any] = field(default_factory=list)
    body: List[Any] = field(default_factory=list)

@dataclass
class BreakStat:
    pass

@dataclass
class ContinueStat:
    pass

@dataclass
class DoBlock:
    body: List[Any] = field(default_factory=list)

@dataclass
class Comment:
    text: str = ""
