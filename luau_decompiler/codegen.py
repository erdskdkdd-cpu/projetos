"""
Code Generator: Converts AST nodes back into readable Luau source code.
"""

from . import ast_nodes as ast


class CodeGen:
    def __init__(self):
        self.indent_level = 0
        self.indent_str = "\t"

    def generate(self, node) -> str:
        if isinstance(node, ast.FunctionExpr) and node.name is None:
            # Main chunk - just emit body
            lines = []
            for stmt in node.body:
                lines.append(self._emit_stmt(stmt))
            return "\n".join(lines)
        return self._emit_expr(node)

    def _indent(self) -> str:
        return self.indent_str * self.indent_level

    def _emit_stmt(self, node) -> str:
        if isinstance(node, ast.LocalAssign):
            return self._emit_local_assign(node)
        elif isinstance(node, ast.Assign):
            return self._emit_assign(node)
        elif isinstance(node, ast.ReturnStat):
            return self._emit_return(node)
        elif isinstance(node, ast.ExprStat):
            return f"{self._indent()}{self._emit_expr(node.expr)}"
        elif isinstance(node, ast.IfStat):
            return self._emit_if(node)
        elif isinstance(node, ast.WhileStat):
            return self._emit_while(node)
        elif isinstance(node, ast.NumericForStat):
            return self._emit_numeric_for(node)
        elif isinstance(node, ast.GenericForStat):
            return self._emit_generic_for(node)
        elif isinstance(node, ast.DoBlock):
            return self._emit_do_block(node)
        elif isinstance(node, ast.BreakStat):
            return f"{self._indent()}break"
        elif isinstance(node, ast.ContinueStat):
            return f"{self._indent()}continue"
        elif isinstance(node, ast.Comment):
            return f"{self._indent()}-- {node.text}"
        elif isinstance(node, ast.RepeatStat):
            return self._emit_repeat(node)
        return f"{self._indent()}-- ?? {type(node).__name__}"

    def _emit_expr(self, node) -> str:
        if node is None:
            return "nil"
        if isinstance(node, ast.NilLiteral):
            return "nil"
        elif isinstance(node, ast.BoolLiteral):
            return "true" if node.value else "false"
        elif isinstance(node, ast.NumberLiteral):
            v = node.value
            if v == int(v) and abs(v) < 1e15:
                return str(int(v))
            return str(v)
        elif isinstance(node, ast.StringLiteral):
            return self._escape_string(node.value)
        elif isinstance(node, ast.VarArgExpr):
            return "..."
        elif isinstance(node, ast.LocalVar):
            return node.name
        elif isinstance(node, ast.UpvalueRef):
            return node.name
        elif isinstance(node, ast.GlobalRef):
            return node.name
        elif isinstance(node, ast.IndexExpr):
            return self._emit_index(node)
        elif isinstance(node, ast.MethodCall):
            return self._emit_method_call(node)
        elif isinstance(node, ast.FunctionCall):
            return self._emit_function_call(node)
        elif isinstance(node, ast.BinaryOp):
            return self._emit_binary_op(node)
        elif isinstance(node, ast.UnaryOp):
            return self._emit_unary_op(node)
        elif isinstance(node, ast.ConcatExpr):
            parts = [self._emit_expr(p) for p in node.parts]
            return " .. ".join(parts)
        elif isinstance(node, ast.TableConstructor):
            return self._emit_table(node)
        elif isinstance(node, ast.FunctionExpr):
            return self._emit_function_expr(node)
        elif isinstance(node, ast.IfExpr):
            cond = self._emit_expr(node.condition)
            then_expr = self._emit_expr(node.then_expr)
            else_expr = self._emit_expr(node.else_expr)
            return f"if {cond} then {then_expr} else {else_expr}"
        return f"--[[unknown: {type(node).__name__}]]"

    def _escape_string(self, s: str) -> str:
        escaped = s.replace("\\", "\\\\").replace('"', '\\"')
        escaped = escaped.replace("\n", "\\n").replace("\r", "\\r").replace("\t", "\\t")
        escaped = escaped.replace("\0", "\\0")
        return f'"{escaped}"'

    def _emit_index(self, node: ast.IndexExpr) -> str:
        obj = self._emit_expr(node.obj)
        if node.is_dot and isinstance(node.key, ast.StringLiteral):
            key = node.key.value
            if key.isidentifier() and not key.startswith("__"):
                return f"{obj}.{key}"
            return f'{obj}["{key}"]'
        key = self._emit_expr(node.key)
        return f"{obj}[{key}]"

    def _emit_method_call(self, node: ast.MethodCall) -> str:
        obj = self._emit_expr(node.obj)
        args = ", ".join(self._emit_expr(a) for a in node.args)
        return f"{obj}:{node.method}({args})"

    def _emit_function_call(self, node: ast.FunctionCall) -> str:
        func = self._emit_expr(node.func)
        args = ", ".join(self._emit_expr(a) for a in node.args)
        return f"{func}({args})"

    def _emit_binary_op(self, node: ast.BinaryOp) -> str:
        left = self._emit_expr(node.left)
        right = self._emit_expr(node.right)
        return f"({left} {node.op} {right})"

    def _emit_unary_op(self, node: ast.UnaryOp) -> str:
        operand = self._emit_expr(node.operand)
        if node.op == "#":
            return f"#{operand}"
        elif node.op == "-":
            return f"-{operand}"
        return f"{node.op} {operand}"

    def _emit_table(self, node: ast.TableConstructor) -> str:
        if not node.fields:
            return "{}"
        parts = []
        for f in node.fields:
            if isinstance(f, ast.TableField):
                if f.key is None:
                    parts.append(self._emit_expr(f.value))
                elif f.is_string_key and isinstance(f.key, ast.StringLiteral):
                    parts.append(f"{f.key.value} = {self._emit_expr(f.value)}")
                else:
                    parts.append(f"[{self._emit_expr(f.key)}] = {self._emit_expr(f.value)}")
            else:
                parts.append(self._emit_expr(f))
        if len(parts) <= 3:
            return "{ " + ", ".join(parts) + " }"
        inner = ",\n".join(f"{self._indent()}{self.indent_str}{p}" for p in parts)
        return "{\n" + inner + "\n" + self._indent() + "}"

    def _emit_function_expr(self, node: ast.FunctionExpr) -> str:
        params = ", ".join(node.params)
        if node.is_vararg:
            params = (params + ", ...") if params else "..."

        if not node.body:
            return f"function({params}) end"

        self.indent_level += 1
        body_lines = []
        for stmt in node.body:
            body_lines.append(self._emit_stmt(stmt))
        self.indent_level -= 1

        body_str = "\n".join(body_lines)
        return f"function({params})\n{body_str}\n{self._indent()}end"

    def _emit_local_assign(self, node: ast.LocalAssign) -> str:
        if (
            len(node.names) == 1
            and len(node.values) == 1
            and isinstance(node.values[0], ast.FunctionExpr)
        ):
            return self._emit_local_function(node.names[0], node.values[0])
        names = ", ".join(node.names)
        if node.values:
            vals = ", ".join(self._emit_expr(v) for v in node.values)
            return f"{self._indent()}local {names} = {vals}"
        return f"{self._indent()}local {names}"

    def _emit_assign(self, node: ast.Assign) -> str:
        if (
            len(node.targets) == 1
            and len(node.values) == 1
            and isinstance(node.values[0], ast.FunctionExpr)
        ):
            function_assign = self._emit_named_function(node.targets[0], node.values[0])
            if function_assign is not None:
                return function_assign
        targets = ", ".join(self._emit_expr(t) for t in node.targets)
        vals = ", ".join(self._emit_expr(v) for v in node.values)
        return f"{self._indent()}{targets} = {vals}"

    def _emit_local_function(self, name: str, node: ast.FunctionExpr) -> str:
        params = ", ".join(node.params)
        if node.is_vararg:
            params = (params + ", ...") if params else "..."
        lines = [f"{self._indent()}local function {name}({params})"]
        self.indent_level += 1
        for stmt in node.body:
            lines.append(self._emit_stmt(stmt))
        self.indent_level -= 1
        lines.append(f"{self._indent()}end")
        return "\n".join(lines)

    def _emit_named_function(self, target, node: ast.FunctionExpr):
        if not isinstance(target, ast.IndexExpr):
            return None
        if not target.is_dot or not isinstance(target.key, ast.StringLiteral):
            return None
        params = ", ".join(node.params)
        if node.is_vararg:
            params = (params + ", ...") if params else "..."
        head = f"{self._indent()}function {self._emit_index(target)}({params})"
        lines = [head]
        self.indent_level += 1
        for stmt in node.body:
            lines.append(self._emit_stmt(stmt))
        self.indent_level -= 1
        lines.append(f"{self._indent()}end")
        return "\n".join(lines)

    def _emit_return(self, node: ast.ReturnStat) -> str:
        if not node.values:
            return f"{self._indent()}return"
        vals = ", ".join(self._emit_expr(v) for v in node.values)
        return f"{self._indent()}return {vals}"

    def _emit_if(self, node: ast.IfStat) -> str:
        cond = self._emit_expr(node.condition)
        lines = [f"{self._indent()}if {cond} then"]
        self.indent_level += 1
        for s in node.then_body:
            lines.append(self._emit_stmt(s))
        self.indent_level -= 1
        for clause in node.elseif_clauses:
            ec, eb = clause
            lines.append(f"{self._indent()}elseif {self._emit_expr(ec)} then")
            self.indent_level += 1
            for s in eb:
                lines.append(self._emit_stmt(s))
            self.indent_level -= 1
        if node.else_body:
            lines.append(f"{self._indent()}else")
            self.indent_level += 1
            for s in node.else_body:
                lines.append(self._emit_stmt(s))
            self.indent_level -= 1
        lines.append(f"{self._indent()}end")
        return "\n".join(lines)

    def _emit_while(self, node: ast.WhileStat) -> str:
        cond = self._emit_expr(node.condition)
        lines = [f"{self._indent()}while {cond} do"]
        self.indent_level += 1
        for s in node.body:
            lines.append(self._emit_stmt(s))
        self.indent_level -= 1
        lines.append(f"{self._indent()}end")
        return "\n".join(lines)

    def _emit_repeat(self, node: ast.RepeatStat) -> str:
        lines = [f"{self._indent()}repeat"]
        self.indent_level += 1
        for s in node.body:
            lines.append(self._emit_stmt(s))
        self.indent_level -= 1
        lines.append(f"{self._indent()}until {self._emit_expr(node.condition)}")
        return "\n".join(lines)

    def _emit_numeric_for(self, node: ast.NumericForStat) -> str:
        start = self._emit_expr(node.start)
        limit = self._emit_expr(node.limit)
        step = self._emit_expr(node.step) if node.step else None
        header = f"for {node.var_name} = {start}, {limit}"
        if step:
            header += f", {step}"
        lines = [f"{self._indent()}{header} do"]
        self.indent_level += 1
        for s in node.body:
            lines.append(self._emit_stmt(s))
        self.indent_level -= 1
        lines.append(f"{self._indent()}end")
        return "\n".join(lines)

    def _emit_generic_for(self, node: ast.GenericForStat) -> str:
        vars_str = ", ".join(node.var_names)
        iters = ", ".join(self._emit_expr(it) for it in node.iterators)
        lines = [f"{self._indent()}for {vars_str} in {iters} do"]
        self.indent_level += 1
        for s in node.body:
            lines.append(self._emit_stmt(s))
        self.indent_level -= 1
        lines.append(f"{self._indent()}end")
        return "\n".join(lines)

    def _emit_do_block(self, node: ast.DoBlock) -> str:
        lines = [f"{self._indent()}do"]
        self.indent_level += 1
        for s in node.body:
            lines.append(self._emit_stmt(s))
        self.indent_level -= 1
        lines.append(f"{self._indent()}end")
        return "\n".join(lines)
