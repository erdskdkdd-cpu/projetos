"""
Lifter: converts Luau bytecode instructions into a small Luau AST.
"""

from typing import Any, Dict, List, Optional, Set, Tuple

from . import ast_nodes as ast
from .bytecode import Bytecode, Instruction, Proto
from .enums import LuauCaptureType, LuauConstantType, LuauOpcode
from .utils import decode_import_id

LUA_KEYWORDS = {
    "and", "break", "do", "else", "elseif", "end", "false", "for",
    "function", "if", "in", "local", "nil", "not", "or", "repeat",
    "return", "then", "true", "until", "while",
}


class Lifter:
    def __init__(self, bc: Bytecode):
        self.bc = bc

    def lift_all(self) -> ast.FunctionExpr:
        main = self.bc.protos[self.bc.main_proto_id]
        lifted = self._lift_proto(main)
        lifted.name = None
        return lifted

    def _lift_proto(
        self,
        proto: Proto,
        upvalue_bindings: Optional[List[Any]] = None,
    ) -> ast.FunctionExpr:
        reg_names: Dict[int, str] = {}
        tracked_regs = self._scan_tracked_registers(proto)
        table_builders: Dict[int, ast.TableConstructor] = {}
        sealed_table_builders: Set[int] = set()
        declared_regs: Set[int] = set()
        registers: Dict[int, Any] = {}
        upvalues = list(upvalue_bindings or [])

        params: List[str] = []
        for index in range(proto.num_params):
            name = self._param_name(proto, index)
            reg_names[index] = name
            declared_regs.add(index)
            registers[index] = ast.LocalVar(name, index)
            params.append(name)

        self._seed_proto_names(proto, reg_names)

        body, _, _, _ = self._lift_block(
            proto=proto,
            start=0,
            end=len(proto.instructions),
            regs=registers,
            declared_regs=declared_regs,
            reg_names=reg_names,
            table_builders=table_builders,
            sealed_table_builders=sealed_table_builders,
            upvalues=upvalues,
            tracked_regs=tracked_regs,
        )

        return ast.FunctionExpr(
            params=params,
            is_vararg=proto.is_vararg,
            body=body,
            name=proto.debug_name or None,
        )

    def _lift_block(
        self,
        proto: Proto,
        start: int,
        end: int,
        regs: Dict[int, Any],
        declared_regs: Set[int],
        reg_names: Dict[int, str],
        table_builders: Dict[int, ast.TableConstructor],
        sealed_table_builders: Set[int],
        upvalues: List[Any],
        tracked_regs: Dict[int, int],
    ) -> Tuple[List[Any], Dict[int, Any], Set[int], Dict[int, ast.TableConstructor]]:
        stmts: List[Any] = []
        current_regs = dict(regs)
        current_declared = set(declared_regs)
        current_builders = dict(table_builders)
        pending_namecall_obj: Optional[Any] = None
        pending_namecall_method: Optional[str] = None
        insts = proto.instructions
        pc_to_idx = {ins.pc: idx for idx, ins in enumerate(insts)}
        i = start

        while i < end and i < len(insts):
            inst = insts[i]
            try:
                op = LuauOpcode(inst.opcode)
            except ValueError:
                stmts.append(ast.Comment(f"unknown opcode 0x{inst.opcode:02X}"))
                i += 1
                continue

            if op in (LuauOpcode.NOP, LuauOpcode.BREAK, LuauOpcode.CLOSEUPVALS):
                i += 1
                continue

            if op == LuauOpcode.LOADNIL:
                current_regs, current_declared, current_builders = self._write_register(
                    proto, stmts, current_regs, current_declared, current_builders,
                    reg_names, tracked_regs, sealed_table_builders, inst.a, ast.NilLiteral(), inst.pc
                )
                i += 1
                continue

            if op == LuauOpcode.LOADB:
                current_regs[inst.a] = ast.BoolLiteral(bool(inst.b))
                i += 1
                continue

            if op == LuauOpcode.LOADN:
                current_regs[inst.a] = ast.NumberLiteral(float(inst.d))
                i += 1
                continue

            if op == LuauOpcode.LOADK:
                current_regs[inst.a] = self._get_const_expr(proto, inst.d)
                i += 1
                continue

            if op == LuauOpcode.LOADKX:
                kidx = inst.aux or 0
                current_regs[inst.a] = self._get_const_expr(proto, kidx)
                i += 1
                continue

            if op == LuauOpcode.MOVE:
                src = self._expr_for_reg(proto, current_regs, reg_names, inst.b, inst.pc)
                current_regs, current_declared, current_builders = self._write_register(
                    proto, stmts, current_regs, current_declared, current_builders,
                    reg_names, tracked_regs, sealed_table_builders, inst.a, src, inst.pc
                )
                if inst.b in current_builders:
                    current_builders[inst.a] = current_builders[inst.b]
                i += 1
                continue

            if op == LuauOpcode.GETGLOBAL:
                current_regs[inst.a] = ast.GlobalRef(self._aux_string(proto, inst.aux) or f"global_{inst.a}")
                i += 1
                continue

            if op == LuauOpcode.SETGLOBAL:
                name = self._aux_string(proto, inst.aux) or f"global_{inst.a}"
                value = self._expr_for_reg(proto, current_regs, reg_names, inst.a, inst.pc)
                stmts.append(ast.Assign(targets=[ast.GlobalRef(name)], values=[value]))
                i += 1
                continue

            if op == LuauOpcode.GETUPVAL:
                if inst.b < len(upvalues):
                    expr = upvalues[inst.b]
                else:
                    expr = self._fallback_upvalue(proto, inst.b)
                current_regs[inst.a] = expr
                i += 1
                continue

            if op == LuauOpcode.SETUPVAL:
                target = upvalues[inst.b] if inst.b < len(upvalues) else self._fallback_upvalue(proto, inst.b)
                value = self._expr_for_reg(proto, current_regs, reg_names, inst.a, inst.pc)
                stmts.append(ast.Assign(targets=[target], values=[value]))
                i += 1
                continue

            if op == LuauOpcode.GETIMPORT:
                current_regs[inst.a] = self._get_const_expr(proto, inst.d)
                i += 1
                continue

            if op == LuauOpcode.GETTABLEKS:
                obj = self._expr_for_reg(proto, current_regs, reg_names, inst.b, inst.pc)
                key_name = self._aux_string(proto, inst.aux) or f"field_{inst.c}"
                current_regs[inst.a] = ast.IndexExpr(obj, ast.StringLiteral(key_name), is_dot=True)
                i += 1
                continue

            if op == LuauOpcode.SETTABLEKS:
                key_name = self._aux_string(proto, inst.aux) or f"field_{inst.c}"
                value = self._expr_for_reg(proto, current_regs, reg_names, inst.a, inst.pc)
                if self._append_named_table_field(
                    current_builders, sealed_table_builders, reg_names, inst.b, key_name, value
                ):
                    i += 1
                    continue
                obj = self._expr_for_reg(proto, current_regs, reg_names, inst.b, inst.pc)
                target = ast.IndexExpr(obj, ast.StringLiteral(key_name), is_dot=True)
                stmts.append(ast.Assign(targets=[target], values=[value]))
                i += 1
                continue

            if op == LuauOpcode.GETTABLE:
                obj = self._expr_for_reg(proto, current_regs, reg_names, inst.b, inst.pc)
                key = self._expr_for_reg(proto, current_regs, reg_names, inst.c, inst.pc)
                current_regs[inst.a] = ast.IndexExpr(obj, key, is_dot=False)
                i += 1
                continue

            if op == LuauOpcode.SETTABLE:
                value = self._expr_for_reg(proto, current_regs, reg_names, inst.a, inst.pc)
                key = self._expr_for_reg(proto, current_regs, reg_names, inst.c, inst.pc)
                if self._append_table_field(
                    current_builders, sealed_table_builders, reg_names, inst.b, key, value
                ):
                    i += 1
                    continue
                obj = self._expr_for_reg(proto, current_regs, reg_names, inst.b, inst.pc)
                target = ast.IndexExpr(obj, key, is_dot=False)
                stmts.append(ast.Assign(targets=[target], values=[value]))
                i += 1
                continue

            if op == LuauOpcode.GETTABLEN:
                obj = self._expr_for_reg(proto, current_regs, reg_names, inst.b, inst.pc)
                current_regs[inst.a] = ast.IndexExpr(obj, ast.NumberLiteral(float(inst.c + 1)), is_dot=False)
                i += 1
                continue

            if op == LuauOpcode.SETTABLEN:
                value = self._expr_for_reg(proto, current_regs, reg_names, inst.a, inst.pc)
                key = ast.NumberLiteral(float(inst.c + 1))
                if self._append_table_field(
                    current_builders, sealed_table_builders, reg_names, inst.b, key, value
                ):
                    i += 1
                    continue
                obj = self._expr_for_reg(proto, current_regs, reg_names, inst.b, inst.pc)
                target = ast.IndexExpr(obj, key, is_dot=False)
                stmts.append(ast.Assign(targets=[target], values=[value]))
                i += 1
                continue

            if op == LuauOpcode.NAMECALL:
                pending_namecall_obj = self._expr_for_reg(proto, current_regs, reg_names, inst.b, inst.pc)
                pending_namecall_method = self._aux_string(proto, inst.aux) or f"method_{inst.c}"
                current_regs[inst.a] = ast.GlobalRef(pending_namecall_method)
                current_regs[inst.a + 1] = pending_namecall_obj
                i += 1
                continue

            if op == LuauOpcode.CALL:
                func_reg = inst.a
                nargs = inst.b - 1 if inst.b > 0 else -1
                nresults = inst.c - 1 if inst.c > 0 else -1

                if pending_namecall_obj is not None and pending_namecall_method is not None:
                    args: List[Any] = []
                    explicit_arg_count = max(nargs - 1, 0) if nargs >= 0 else 0
                    for offset in range(explicit_arg_count):
                        args.append(self._expr_for_reg(proto, current_regs, reg_names, func_reg + 2 + offset, inst.pc))
                    call_expr = ast.MethodCall(obj=pending_namecall_obj, method=pending_namecall_method, args=args)
                    pending_namecall_obj = None
                    pending_namecall_method = None
                else:
                    func_expr = self._expr_for_reg(proto, current_regs, reg_names, func_reg, inst.pc)
                    args = []
                    if nargs >= 0:
                        for offset in range(nargs):
                            args.append(self._expr_for_reg(proto, current_regs, reg_names, func_reg + 1 + offset, inst.pc))
                    call_expr = ast.FunctionCall(func=func_expr, args=args, num_returns=nresults)

                if nresults == 0:
                    stmts.append(ast.ExprStat(expr=call_expr))
                elif nresults == 1:
                    suggestion = self._suggest_name(call_expr)
                    next_inst = insts[i + 1] if i + 1 < len(insts) else None
                    if (
                        next_inst is not None
                        and next_inst.opcode == LuauOpcode.MOVE
                        and next_inst.b == func_reg
                    ):
                        if suggestion:
                            self._set_reg_name(reg_names, next_inst.a, suggestion)
                        current_regs[func_reg] = call_expr
                    else:
                        if suggestion:
                            self._set_reg_name(reg_names, func_reg, suggestion)
                        current_regs, current_declared, current_builders = self._write_register(
                            proto, stmts, current_regs, current_declared, current_builders,
                            reg_names, tracked_regs, sealed_table_builders, func_reg, call_expr, inst.pc
                        )
                elif nresults > 1:
                    names: List[str] = []
                    for offset in range(nresults):
                        reg = func_reg + offset
                        name = self._reg_name(proto, reg_names, reg, inst.pc)
                        names.append(name)
                        current_regs[reg] = ast.LocalVar(name, reg)
                        current_declared.add(reg)
                    stmts.append(ast.LocalAssign(names=names, values=[call_expr]))
                else:
                    current_regs[func_reg] = call_expr
                    stmts.append(ast.ExprStat(expr=call_expr))
                i += 1
                continue

            if op == LuauOpcode.RETURN:
                if inst.b == 0:
                    val = self._expr_for_reg(proto, current_regs, reg_names, inst.a, inst.pc)
                    if stmts and isinstance(stmts[-1], ast.ExprStat) and self._same_expr(stmts[-1].expr, val):
                        stmts.pop()
                    values = [val]
                    values = self._fold_dead_variable_return(stmts, values)
                    stmts.append(ast.ReturnStat(values=values))
                    i += 1
                    continue
                nvals = inst.b - 1 if inst.b > 0 else 0
                values = [
                    self._expr_for_reg(proto, current_regs, reg_names, inst.a + offset, inst.pc)
                    for offset in range(nvals)
                ]
                values = self._fold_dead_variable_return(stmts, values)
                stmts.append(ast.ReturnStat(values=values))
                i += 1
                continue

            if op in (LuauOpcode.JUMP, LuauOpcode.JUMPBACK):
                stmts.append(ast.Comment(f"jump {inst.d:+d}"))
                i += 1
                continue

            if op == LuauOpcode.JUMPIF:
                cond = self._expr_for_reg(proto, current_regs, reg_names, inst.a, inst.pc)
                branch = self._lift_structured_if(
                    proto, insts, pc_to_idx, i, inst, current_regs, current_declared,
                    reg_names, current_builders, sealed_table_builders, upvalues,
                    tracked_regs, ast.UnaryOp("not", cond)
                )
                if branch is not None:
                    stmt, next_i, next_regs, next_declared, next_builders = branch
                    if stmt is not None:
                        stmts.append(stmt)
                    current_regs, current_declared, current_builders = (
                        next_regs,
                        next_declared,
                        next_builders,
                    )
                    i = next_i
                    continue
                stmts.append(ast.IfStat(condition=cond, then_body=[ast.Comment(f"jump {inst.d:+d}")]))
                i += 1
                continue

            if op == LuauOpcode.JUMPIFNOT:
                raw_cond = self._expr_for_reg(proto, current_regs, reg_names, inst.a, inst.pc)
                branch = self._lift_structured_if(
                    proto, insts, pc_to_idx, i, inst, current_regs, current_declared,
                    reg_names, current_builders, sealed_table_builders, upvalues,
                    tracked_regs, raw_cond
                )
                if branch is not None:
                    stmt, next_i, next_regs, next_declared, next_builders = branch
                    if stmt is not None:
                        stmts.append(stmt)
                    current_regs, current_declared, current_builders = (
                        next_regs,
                        next_declared,
                        next_builders,
                    )
                    i = next_i
                    continue

                stmts.append(ast.IfStat(condition=ast.UnaryOp("not", raw_cond), then_body=[ast.Comment(f"jump {inst.d:+d}")]))
                i += 1
                continue

            if op in (LuauOpcode.JUMPIFEQ, LuauOpcode.JUMPIFNOTEQ,
                      LuauOpcode.JUMPIFLE, LuauOpcode.JUMPIFLT,
                      LuauOpcode.JUMPIFNOTLE, LuauOpcode.JUMPIFNOTLT):
                left = self._expr_for_reg(proto, current_regs, reg_names, inst.a, inst.pc)
                right_reg = inst.aux & 0xFF if inst.aux is not None else 0
                right = self._expr_for_reg(proto, current_regs, reg_names, right_reg, inst.pc)
                # Branch inversion: the jump SKIPS the then-body when
                # the condition is TRUE, so the then-body runs on the OPPOSITE.
                symbol = {
                    LuauOpcode.JUMPIFEQ: "~=", LuauOpcode.JUMPIFNOTEQ: "==",
                    LuauOpcode.JUMPIFLE: ">", LuauOpcode.JUMPIFNOTLE: "<=",
                    LuauOpcode.JUMPIFLT: ">=", LuauOpcode.JUMPIFNOTLT: "<",
                }[op]
                cond = ast.BinaryOp(symbol, left, right)
                branch = self._lift_structured_if(
                    proto, insts, pc_to_idx, i, inst, current_regs, current_declared,
                    reg_names, current_builders, sealed_table_builders, upvalues,
                    tracked_regs, cond
                )
                if branch is not None:
                    stmt, next_i, next_regs, next_declared, next_builders = branch
                    if stmt is not None:
                        stmts.append(stmt)
                    current_regs, current_declared, current_builders = (
                        next_regs,
                        next_declared,
                        next_builders,
                    )
                    i = next_i
                    continue
                stmts.append(ast.IfStat(condition=cond, then_body=[ast.Comment(f"jump {inst.d:+d}")]))
                i += 1
                continue

            if op in (LuauOpcode.JUMPXEQKN, LuauOpcode.JUMPXEQKS):
                left = self._expr_for_reg(proto, current_regs, reg_names, inst.a, inst.pc)
                aux_val = inst.aux if inst.aux is not None else 0
                const_idx = aux_val & 0xFFFFFF
                invert = (aux_val >> 31) & 1
                right = self._get_const_expr(proto, const_idx)
                # Branch inversion: jump skips the then-body on match,
                # so NOT=1 (jump on mismatch) means then-body runs on MATCH (==)
                symbol = "==" if invert else "~="
                cond = ast.BinaryOp(symbol, left, right)
                branch = self._lift_structured_if(
                    proto, insts, pc_to_idx, i, inst, current_regs, current_declared,
                    reg_names, current_builders, sealed_table_builders, upvalues,
                    tracked_regs, cond
                )
                if branch is not None:
                    stmt, next_i, next_regs, next_declared, next_builders = branch
                    if stmt is not None:
                        stmts.append(stmt)
                    current_regs, current_declared, current_builders = (
                        next_regs,
                        next_declared,
                        next_builders,
                    )
                    i = next_i
                    continue
                stmts.append(ast.IfStat(condition=cond, then_body=[ast.Comment(f"jump {inst.d:+d}")]))
                i += 1
                continue

            if op == LuauOpcode.JUMPXEQKB:
                left = self._expr_for_reg(proto, current_regs, reg_names, inst.a, inst.pc)
                aux_val = inst.aux if inst.aux is not None else 0
                bool_val = bool(aux_val & 1)
                invert = (aux_val >> 31) & 1
                # Branch inversion: NOT=1 means jump on mismatch -> then-body on match
                if invert:
                    cond = ast.BinaryOp("==", left, ast.BoolLiteral(bool_val))
                else:
                    cond = ast.BinaryOp("~=", left, ast.BoolLiteral(bool_val))
                branch = self._lift_structured_if(
                    proto, insts, pc_to_idx, i, inst, current_regs, current_declared,
                    reg_names, current_builders, sealed_table_builders, upvalues,
                    tracked_regs, cond
                )
                if branch is not None:
                    stmt, next_i, next_regs, next_declared, next_builders = branch
                    if stmt is not None:
                        stmts.append(stmt)
                    current_regs, current_declared, current_builders = (
                        next_regs,
                        next_declared,
                        next_builders,
                    )
                    i = next_i
                    continue
                stmts.append(ast.IfStat(condition=cond, then_body=[ast.Comment(f"jump {inst.d:+d}")]))
                i += 1
                continue

            if op == LuauOpcode.JUMPXEQKNIL:
                left = self._expr_for_reg(proto, current_regs, reg_names, inst.a, inst.pc)
                aux_val = inst.aux if inst.aux is not None else 0
                invert = (aux_val >> 31) & 1
                # Branch inversion: NOT=1 means jump on mismatch -> then-body on match
                if invert:
                    cond = ast.BinaryOp("==", left, ast.NilLiteral())
                else:
                    cond = ast.BinaryOp("~=", left, ast.NilLiteral())
                branch = self._lift_structured_if(
                    proto, insts, pc_to_idx, i, inst, current_regs, current_declared,
                    reg_names, current_builders, sealed_table_builders, upvalues,
                    tracked_regs, cond
                )
                if branch is not None:
                    stmt, next_i, next_regs, next_declared, next_builders = branch
                    if stmt is not None:
                        stmts.append(stmt)
                    current_regs, current_declared, current_builders = (
                        next_regs,
                        next_declared,
                        next_builders,
                    )
                    i = next_i
                    continue
                stmts.append(ast.IfStat(condition=cond, then_body=[ast.Comment(f"jump {inst.d:+d}")]))
                i += 1
                continue

            if op in (LuauOpcode.ADD, LuauOpcode.SUB, LuauOpcode.MUL, LuauOpcode.DIV, LuauOpcode.IDIV, LuauOpcode.MOD, LuauOpcode.POW):
                symbol = {
                    LuauOpcode.ADD: "+",
                    LuauOpcode.SUB: "-",
                    LuauOpcode.MUL: "*",
                    LuauOpcode.DIV: "/",
                    LuauOpcode.IDIV: "//",
                    LuauOpcode.MOD: "%",
                    LuauOpcode.POW: "^",
                }[op]
                left = self._expr_for_reg(proto, current_regs, reg_names, inst.b, inst.pc)
                right = self._expr_for_reg(proto, current_regs, reg_names, inst.c, inst.pc)
                current_regs[inst.a] = ast.BinaryOp(symbol, left, right)
                i += 1
                continue

            if op in (LuauOpcode.ADDK, LuauOpcode.SUBK, LuauOpcode.MULK, LuauOpcode.DIVK):
                symbol = {
                    LuauOpcode.ADDK: "+",
                    LuauOpcode.SUBK: "-",
                    LuauOpcode.MULK: "*",
                    LuauOpcode.DIVK: "/",
                }[op]
                left = self._expr_for_reg(proto, current_regs, reg_names, inst.b, inst.pc)
                right = self._get_const_expr(proto, inst.c)
                current_regs[inst.a] = ast.BinaryOp(symbol, left, right)
                i += 1
                continue

            if op in (LuauOpcode.AND, LuauOpcode.OR):
                symbol = "and" if op == LuauOpcode.AND else "or"
                left = self._expr_for_reg(proto, current_regs, reg_names, inst.b, inst.pc)
                right = self._expr_for_reg(proto, current_regs, reg_names, inst.c, inst.pc)
                current_regs[inst.a] = ast.BinaryOp(symbol, left, right)
                i += 1
                continue

            if op in (LuauOpcode.ANDK, LuauOpcode.ORK):
                symbol = "and" if op == LuauOpcode.ANDK else "or"
                left = self._expr_for_reg(proto, current_regs, reg_names, inst.b, inst.pc)
                right = self._get_const_expr(proto, inst.c)
                current_regs[inst.a] = ast.BinaryOp(symbol, left, right)
                i += 1
                continue

            if op == LuauOpcode.CONCAT:
                parts = [self._expr_for_reg(proto, current_regs, reg_names, reg, inst.pc) for reg in range(inst.b, inst.c + 1)]
                current_regs[inst.a] = ast.ConcatExpr(parts=parts)
                i += 1
                continue

            if op == LuauOpcode.NOT:
                operand = self._expr_for_reg(proto, current_regs, reg_names, inst.b, inst.pc)
                current_regs[inst.a] = ast.UnaryOp("not", operand)
                i += 1
                continue

            if op == LuauOpcode.MINUS:
                operand = self._expr_for_reg(proto, current_regs, reg_names, inst.b, inst.pc)
                current_regs[inst.a] = ast.UnaryOp("-", operand)
                i += 1
                continue

            if op == LuauOpcode.LENGTH:
                operand = self._expr_for_reg(proto, current_regs, reg_names, inst.b, inst.pc)
                current_regs[inst.a] = ast.UnaryOp("#", operand)
                i += 1
                continue

            if op in (LuauOpcode.NEWTABLE, LuauOpcode.DUPTABLE):
                builder = ast.TableConstructor()
                current_regs, current_declared, current_builders = self._write_register(
                    proto, stmts, current_regs, current_declared, current_builders,
                    reg_names, tracked_regs, sealed_table_builders, inst.a, builder, inst.pc
                )
                current_builders[inst.a] = builder
                i += 1
                continue

            if op == LuauOpcode.SETLIST:
                builder = current_builders.get(inst.a)
                if builder is not None:
                    if inst.c == 0:
                        value_count = 0
                        probe = inst.b
                        while probe in current_regs:
                            value_count += 1
                            probe += 1
                    else:
                        value_count = max(inst.c - 1, 0)
                    for offset in range(value_count):
                        builder.fields.append(ast.TableField(value=self._expr_for_reg(proto, current_regs, reg_names, inst.b + offset, inst.pc)))
                i += 1
                continue

            if op in (LuauOpcode.NEWCLOSURE, LuauOpcode.DUPCLOSURE):
                closure, next_index = self._lift_closure(
                    proto, insts, i, current_regs, reg_names, upvalues, op
                )
                if self._should_inline_anonymous_closure(insts, next_index, inst.a, closure):
                    current_regs[inst.a] = closure
                    i = next_index
                    continue
                if closure.name:
                    self._set_reg_name(reg_names, inst.a, closure.name)
                current_regs, current_declared, current_builders = self._write_register(
                    proto, stmts, current_regs, current_declared, current_builders,
                    reg_names, tracked_regs, sealed_table_builders, inst.a, closure, inst.pc
                )
                i = next_index
                continue

            if op == LuauOpcode.GETVARARGS:
                current_regs[inst.a] = ast.VarArgExpr()
                i += 1
                continue

            if op == LuauOpcode.PREPVARARGS:
                i += 1
                continue

            if op == LuauOpcode.FORNPREP:
                loop_idx = self._find_numeric_loop_end(insts, i, end, inst.a)
                if loop_idx is None:
                    stmts.append(ast.Comment(f"for prep at R{inst.a}, jump {inst.d:+d}"))
                    i += 1
                    continue

                loop_var_reg = inst.a + 2
                loop_var_name = self._reg_name(proto, reg_names, loop_var_reg, inst.pc)
                loop_start = self._expr_for_reg(proto, current_regs, reg_names, loop_var_reg, inst.pc)
                loop_step = self._expr_for_reg(proto, current_regs, reg_names, inst.a + 1, inst.pc)
                # Inject the loop variable as a LocalVar so closures inside the
                # body correctly capture it by name (e.g. `getfenv(v3)` not `getfenv(1)`).
                body_regs = dict(current_regs)
                body_declared = set(current_declared)
                body_regs[loop_var_reg] = ast.LocalVar(loop_var_name, loop_var_reg)
                body_declared.add(loop_var_reg)
                body, _, _, _ = self._lift_block(
                    proto, i + 1, loop_idx, body_regs, body_declared,
                    reg_names, current_builders, sealed_table_builders, upvalues, tracked_regs
                )
                if (
                    isinstance(loop_step, ast.NumberLiteral)
                    and loop_step.value < 0
                    and not isinstance(loop_start, (ast.NumberLiteral, ast.StringLiteral, ast.BoolLiteral, ast.NilLiteral))
                ):
                    body = self._replace_expr_in_stmts(
                        body,
                        loop_start,
                        ast.LocalVar(loop_var_name, loop_var_reg),
                    )
                # Luau FORNPREP register layout:
                # R[A] = limit, R[A+1] = step, R[A+2] = index / loop var
                stmts.append(ast.NumericForStat(
                    var_name=loop_var_name,
                    start=loop_start,
                    limit=self._expr_for_reg(proto, current_regs, reg_names, inst.a, inst.pc),
                    step=loop_step,
                    body=body,
                ))
                i = loop_idx + 1
                continue

            if op == LuauOpcode.FORNLOOP:
                stmts.append(ast.Comment(f"fornloop at R{inst.a}, jump {inst.d:+d}"))
                i += 1
                continue

            if op in (LuauOpcode.FORGPREP, LuauOpcode.FORGPREP_INEXT, LuauOpcode.FORGPREP_NEXT, LuauOpcode.FASTCALL3):
                loop_end = self._find_generic_loop_end(insts, i, end, inst.a)
                if loop_end is None:
                    stmts.append(ast.Comment(f"forgprep at R{inst.a}, jump {inst.d:+d}"))
                    i += 1
                    continue
                loop_end_inst = insts[loop_end]
                # FORGLOOP AUX: low 8 bits = variable count
                nresults = (loop_end_inst.aux or 2) if loop_end_inst.aux is not None else 2
                var_names: List[str] = []
                for offset in range(nresults):
                    var_names.append(self._reg_name(proto, reg_names, inst.a + 3 + offset, inst.pc))
                iterators = [self._expr_for_reg(proto, current_regs, reg_names, inst.a, inst.pc)]
                body, _, _, _ = self._lift_block(
                    proto, i + 1, loop_end, current_regs, current_declared,
                    reg_names, current_builders, sealed_table_builders, upvalues, tracked_regs
                )
                stmts.append(ast.GenericForStat(
                    var_names=var_names,
                    iterators=iterators,
                    body=body,
                ))
                i = loop_end + 1
                continue

            if op == LuauOpcode.FORGLOOP:
                stmts.append(ast.Comment(f"forgloop at R{inst.a}, jump {inst.d:+d}"))
                i += 1
                continue

            if op in (
                LuauOpcode.FASTCALL,
                LuauOpcode.FASTCALL1,
                LuauOpcode.FASTCALL2,
                LuauOpcode.FASTCALL2K,
                LuauOpcode.COVERAGE,
                LuauOpcode.CAPTURE,
            ):
                i += 1
                continue

            stmts.append(ast.Comment(f"unhandled: {inst.opname} (0x{inst.opcode:02X})"))
            i += 1

        stmts = self._fold_boolean_ast(stmts)
        return stmts, current_regs, current_declared, current_builders

    def _lift_closure(
        self,
        proto: Proto,
        insts: List[Instruction],
        index: int,
        regs: Dict[int, Any],
        reg_names: Dict[int, str],
        upvalues: List[Any],
        opcode: LuauOpcode,
    ) -> Tuple[ast.FunctionExpr, int]:
        inst = insts[index]
        child_proto = self._resolve_child_proto(proto, inst, opcode)
        if child_proto is None:
            return ast.FunctionExpr(body=[ast.Comment("missing child proto")]), index + 1

        captured_upvalues: List[Any] = []
        next_index = index + 1
        for offset in range(child_proto.num_upvalues):
            if next_index >= len(insts):
                break
            capture = insts[next_index]
            captured_upvalues.append(self._resolve_capture_value(proto, regs, reg_names, upvalues, capture))
            next_index += 1

        while len(captured_upvalues) < child_proto.num_upvalues:
            captured_upvalues.append(self._fallback_upvalue(child_proto, len(captured_upvalues)))

        return self._lift_proto(child_proto, captured_upvalues), next_index

    def _resolve_capture_value(
        self,
        proto: Proto,
        regs: Dict[int, Any],
        reg_names: Dict[int, str],
        upvalues: List[Any],
        capture: Instruction,
    ) -> Any:
        if capture.a == LuauCaptureType.VAL:
            captured = regs.get(capture.b)
            if captured is not None:
                return captured
            name = self._reg_name(proto, reg_names, capture.b, capture.pc)
            return ast.UpvalueRef(name, capture.b)
        if capture.a == LuauCaptureType.REF:
            captured = regs.get(capture.b)
            if isinstance(captured, (ast.LocalVar, ast.UpvalueRef)):
                return captured
            name = self._reg_name(proto, reg_names, capture.b, capture.pc)
            return ast.UpvalueRef(name, capture.b)
        if capture.a == LuauCaptureType.UPVAL and capture.b < len(upvalues):
            return upvalues[capture.b]
        return self._fallback_upvalue(proto, capture.b)

    def _should_inline_anonymous_closure(
        self,
        insts: List[Instruction],
        next_index: int,
        reg: int,
        closure: ast.FunctionExpr,
    ) -> bool:
        if closure.name:
            return False
        if next_index >= len(insts):
            return False
        next_inst = insts[next_index]
        try:
            next_op = LuauOpcode(next_inst.opcode)
        except ValueError:
            return False
        if next_op == LuauOpcode.SETTABLEKS:
            return next_inst.a == reg
        if next_op in (LuauOpcode.SETTABLE, LuauOpcode.SETTABLEN, LuauOpcode.SETGLOBAL):
            return next_inst.a == reg
        return False

    def _lift_structured_if(
        self,
        proto: Proto,
        insts: List[Instruction],
        pc_to_idx: Dict[int, int],
        branch_index: int,
        branch_inst: Instruction,
        current_regs: Dict[int, Any],
        current_declared: Set[int],
        reg_names: Dict[int, str],
        current_builders: Dict[int, ast.TableConstructor],
        sealed_table_builders: Set[int],
        upvalues: List[Any],
        tracked_regs: Dict[int, int],
        condition: Any,
    ) -> Optional[Tuple[Optional[ast.IfStat], int, Dict[int, Any], Set[int], Dict[int, ast.TableConstructor]]]:
        target_idx = self._jump_target_index(insts, pc_to_idx, branch_inst)
        if target_idx is None or target_idx <= branch_index:
            return None

        else_entry = target_idx
        then_end = else_entry
        else_regs = None
        else_declared = None
        else_builders = None
        else_body: List[Any] = []

        if else_entry > branch_index + 1:
            prev_idx = else_entry - 1
            prev_inst = insts[prev_idx]
            if prev_inst.opcode in (LuauOpcode.JUMP, LuauOpcode.JUMPBACK):
                after_else = self._jump_target_index(insts, pc_to_idx, prev_inst)
                if after_else is not None and after_else > else_entry:
                    then_end = prev_idx
                    else_body, else_regs, else_declared, else_builders = self._lift_block(
                        proto, else_entry, after_else, current_regs, current_declared,
                        reg_names, current_builders, sealed_table_builders, upvalues, tracked_regs
                    )
                    then_body, then_regs, then_declared, then_builders = self._lift_block(
                        proto, branch_index + 1, then_end, current_regs, current_declared,
                        reg_names, current_builders, sealed_table_builders, upvalues, tracked_regs
                    )
                    if not then_body and not else_body:
                        merged = self._merge_branch_state(
                            proto, current_regs, current_declared, current_builders,
                            then_regs, then_declared, then_builders,
                            else_regs, else_declared, else_builders, reg_names,
                            condition=condition, emit_if_expr=True
                        )
                        return None, after_else, merged[0], merged[1], merged[2]
                    
                    # --- Branch Folding Heuristic ---
                    # If then_body and else_body are identical in AST structure, we can fold them with `or`.
                    # For simplicity, we just check string equality of their code generation.
                    # Wait, we can do that, but let's just do a simple AST equality check for folding.
                    # For now, just handle standard IfStat.
                    if_stmt = ast.IfStat(condition=condition, then_body=then_body, else_body=else_body)
                    merged = self._merge_branch_state(
                        proto, current_regs, current_declared, current_builders,
                        then_regs, then_declared, then_builders,
                        else_regs, else_declared, else_builders, reg_names
                    )
                    return if_stmt, after_else, merged[0], merged[1], merged[2]

        then_body, then_regs, then_declared, then_builders = self._lift_block(
            proto, branch_index + 1, then_end, current_regs, current_declared,
            reg_names, current_builders, sealed_table_builders, upvalues, tracked_regs
        )
        if not then_body:
            merged = self._merge_branch_state(
                proto, current_regs, current_declared, current_builders,
                then_regs, then_declared, then_builders, None, None, None, reg_names,
                condition=condition, emit_if_expr=True
            )
            return None, else_entry, merged[0], merged[1], merged[2]
            
        if_stmt = ast.IfStat(condition=condition, then_body=then_body)
        merged = self._merge_branch_state(
            proto, current_regs, current_declared, current_builders,
            then_regs, then_declared, then_builders, None, None, None, reg_names
        )
        return if_stmt, else_entry, merged[0], merged[1], merged[2]

    def _resolve_child_proto(
        self,
        proto: Proto,
        inst: Instruction,
        opcode: LuauOpcode,
    ) -> Optional[Proto]:
        if opcode == LuauOpcode.NEWCLOSURE:
            if 0 <= inst.d < len(proto.child_protos):
                child_id = proto.child_protos[inst.d]
                if 0 <= child_id < len(self.bc.protos):
                    return self.bc.protos[child_id]
            return None

        if 0 <= inst.d < len(proto.constants):
            const = proto.constants[inst.d]
            if const.type == LuauConstantType.CLOSURE and 0 <= const.value < len(self.bc.protos):
                return self.bc.protos[const.value]
        return None

    def _find_numeric_loop_end(
        self,
        insts: List[Instruction],
        start: int,
        end: int,
        register: int,
    ) -> Optional[int]:
        for index in range(start + 1, end):
            probe = insts[index]
            if probe.opcode == LuauOpcode.FORNLOOP and probe.a == register:
                return index
        return None

    def _find_generic_loop_end(
        self,
        insts: List[Instruction],
        start: int,
        end: int,
        register: int,
    ) -> Optional[int]:
        for index in range(start + 1, end):
            probe = insts[index]
            if probe.opcode == LuauOpcode.FORGLOOP and probe.a == register:
                return index
        return None

    def _merge_branch_state(
        self,
        proto: Proto,
        incoming_regs: Dict[int, Any],
        incoming_declared: Set[int],
        incoming_builders: Dict[int, ast.TableConstructor],
        then_regs: Dict[int, Any],
        then_declared: Set[int],
        then_builders: Dict[int, ast.TableConstructor],
        else_regs: Optional[Dict[int, Any]],
        else_declared: Optional[Set[int]],
        else_builders: Optional[Dict[int, ast.TableConstructor]],
        reg_names: Dict[int, str],
        condition: Any = None,
        emit_if_expr: bool = False,
    ) -> Tuple[Dict[int, Any], Set[int], Dict[int, ast.TableConstructor]]:
        merged_regs = dict(incoming_regs)
        merged_declared = set(incoming_declared) | set(then_declared)
        merged_builders = dict(incoming_builders)

        if else_regs is None:
            for reg, value in incoming_regs.items():
                if reg not in merged_regs:
                    merged_regs[reg] = value
            for reg, builder in then_builders.items():
                merged_builders[reg] = builder
            if emit_if_expr:
                all_regs = set(incoming_regs) | set(then_regs)
                for reg in all_regs:
                    then_value = then_regs.get(reg, incoming_regs.get(reg))
                    else_value = incoming_regs.get(reg)
                    if not self._same_expr(then_value, else_value):
                        merged_regs[reg] = ast.IfExpr(condition, then_value, else_value)
            return merged_regs, merged_declared, merged_builders

        merged_declared |= set(else_declared or set())
        merged_builders.update(then_builders)
        merged_builders.update(else_builders or {})

        all_regs = set(incoming_regs) | set(then_regs) | set(else_regs)
        for reg in all_regs:
            then_value = then_regs.get(reg, incoming_regs.get(reg))
            else_value = else_regs.get(reg, incoming_regs.get(reg))
            if self._same_expr(then_value, else_value):
                merged_regs[reg] = then_value
                continue
            if emit_if_expr:
                merged_regs[reg] = ast.IfExpr(condition, then_value, else_value)
            else:
                merged_regs[reg] = ast.LocalVar(self._reg_name(proto, reg_names, reg, 0), reg)

        return merged_regs, merged_declared, merged_builders

    def _same_expr(self, left: Any, right: Any) -> bool:
        if type(left) is not type(right):
            return False
        return getattr(left, "__dict__", left) == getattr(right, "__dict__", right)

    def _write_register(
        self,
        proto: Proto,
        stmts: List[Any],
        current_regs: Dict[int, Any],
        current_declared: Set[int],
        current_builders: Dict[int, ast.TableConstructor],
        reg_names: Dict[int, str],
        tracked_regs: Dict[int, int],
        sealed_table_builders: Set[int],
        target_reg: int,
        expr: Any,
        pc: int,
    ) -> Tuple[Dict[int, Any], Set[int], Dict[int, ast.TableConstructor]]:
        next_regs = dict(current_regs)
        next_declared = set(current_declared)
        next_builders = dict(current_builders)

        suggestion = self._suggest_name(expr)
        if suggestion:
            self._set_reg_name(reg_names, target_reg, suggestion)

        should_emit = self._should_emit_register(proto, target_reg, reg_names, tracked_regs, current_declared, pc)

        # Suppress trivial aliasing for untracked first-time writes only.
        # A "trivial" expr is one that can be inlined at every use site
        # without changing semantics (simple variables, literals).
        # Already-declared registers MUST still emit reassignment.
        _trivial_types = (ast.LocalVar, ast.UpvalueRef, ast.GlobalRef,
                          ast.VarArgExpr, ast.StringLiteral,
                          ast.NumberLiteral, ast.BoolLiteral, ast.NilLiteral)
        if (not should_emit
                and isinstance(expr, _trivial_types)
                and target_reg not in current_declared):
            next_regs[target_reg] = expr
            return next_regs, next_declared, next_builders

        if not should_emit:
            next_regs[target_reg] = expr
            if isinstance(expr, ast.TableConstructor) and target_reg not in next_builders:
                next_builders[target_reg] = expr
            return next_regs, next_declared, next_builders

        name = self._reg_name(proto, reg_names, target_reg, pc)

        # SSA cleanup: eliminate dead `local x = x` assignments
        if self._is_dead_self_assign(name, expr, current_declared, target_reg):
            next_regs[target_reg] = expr
            return next_regs, next_declared, next_builders

        # Decide local vs reassignment purely by name.
        # If the register already holds a variable with the SAME name, it's a
        # reassignment (e.g. `v12 = function(...)`). If the name differs (register
        # reused for a different variable like Create→Destroy→Get on R10), it's a
        # new `local function` declaration.
        already_declared = target_reg in current_declared
        existing_var = current_regs.get(target_reg)
        same_variable = (
            already_declared
            and isinstance(existing_var, ast.LocalVar)
            and existing_var.name == name
        )
        if same_variable:
            stmts.append(ast.Assign(targets=[ast.LocalVar(name, target_reg)], values=[expr]))
        else:
            stmts.append(ast.LocalAssign(names=[name], values=[expr]))
            next_declared.add(target_reg)

        next_regs[target_reg] = ast.LocalVar(name, target_reg)
        if isinstance(expr, ast.TableConstructor):
            sealed_table_builders.add(id(expr))
            next_builders[target_reg] = expr
        else:
            next_builders.pop(target_reg, None)
        return next_regs, next_declared, next_builders

    def _fold_dead_variable_return(
        self,
        stmts: List[Any],
        values: List[Any],
    ) -> List[Any]:
        """Collapse `local v = expr; return v` into `return expr`.

        Detects when every return value is a LocalVar whose only purpose was
        the immediately preceding LocalAssign, and inlines the assigned
        expression directly into the return statement.
        """
        if not stmts or not values:
            return values
        if len(values) != 1:
            return values
        val = values[0]
        if not isinstance(val, ast.LocalVar):
            return values
        prev = stmts[-1]
        if not isinstance(prev, ast.LocalAssign):
            return values
        if len(prev.names) != 1 or prev.names[0] != val.name:
            return values
        if not prev.values:
            return values
        stmts.pop()
        return prev.values

    def _is_dead_self_assign(self, name: str, expr: Any, declared: Set[int], reg: int) -> bool:
        """Detect `local x = x` style dead assignments from SSA register overlap."""
        if isinstance(expr, ast.LocalVar) and expr.name == name:
            return True
        if isinstance(expr, ast.UpvalueRef) and expr.name == name:
            return True
        return False

    def _should_emit_register(
        self,
        proto: Proto,
        reg: int,
        reg_names: Dict[int, str],
        tracked_regs: Dict[int, int],
        declared_regs: Set[int],
        pc: int,
    ) -> bool:
        if reg in declared_regs or reg in reg_names:
            return True
        if reg in tracked_regs and pc >= tracked_regs[reg]:
            return True
        return any(
            lv["reg"] == reg and lv["start_pc"] <= pc < lv["end_pc"] and lv["name"] != ""
            for lv in proto.local_vars
        )

    def _expr_for_reg(
        self,
        proto: Proto,
        regs: Dict[int, Any],
        reg_names: Dict[int, str],
        reg: int,
        pc: int,
    ) -> Any:
        if reg in regs:
            return regs[reg]
        return ast.LocalVar(self._reg_name(proto, reg_names, reg, pc), reg)

    def _append_named_table_field(
        self,
        table_builders: Dict[int, ast.TableConstructor],
        sealed_table_builders: Set[int],
        reg_names: Dict[int, str],
        reg: int,
        key_name: str,
        value: Any,
    ) -> bool:
        builder = table_builders.get(reg)
        if builder is None or id(builder) in sealed_table_builders or reg in reg_names:
            return False
        for field in builder.fields:
            if (
                isinstance(field, ast.TableField)
                and field.is_string_key
                and isinstance(field.key, ast.StringLiteral)
                and field.key.value == key_name
            ):
                field.value = value
                return True
        builder.fields.append(ast.TableField(
            key=ast.StringLiteral(key_name),
            value=value,
            is_string_key=True,
        ))
        return True

    def _append_table_field(
        self,
        table_builders: Dict[int, ast.TableConstructor],
        sealed_table_builders: Set[int],
        reg_names: Dict[int, str],
        reg: int,
        key: Any,
        value: Any,
    ) -> bool:
        builder = table_builders.get(reg)
        if builder is None or id(builder) in sealed_table_builders or reg in reg_names:
            return False
        builder.fields.append(ast.TableField(key=key, value=value))
        return True

    def _scan_tracked_registers(self, proto: Proto) -> Dict[int, int]:
        tracked: Dict[int, int] = {}
        insts = proto.instructions
        for index, inst in enumerate(insts):
            try:
                op = LuauOpcode(inst.opcode)
            except ValueError:
                continue
            if op in (LuauOpcode.SETTABLEKS, LuauOpcode.SETTABLE):
                self._track_register_from_usage(proto, tracked, inst.b, index)
            if op == LuauOpcode.RETURN and inst.b > 1:
                self._track_register_from_usage(proto, tracked, inst.a, index)
            if op == LuauOpcode.CALL and self._call_result_is_reused(insts, index):
                self._track_register_from_usage(proto, tracked, inst.a, index)

        index = 0
        while index < len(insts):
            inst = insts[index]
            try:
                op = LuauOpcode(inst.opcode)
            except ValueError:
                index += 1
                continue
            if op in (LuauOpcode.NEWCLOSURE, LuauOpcode.DUPCLOSURE):
                child = self._resolve_child_proto(proto, inst, op)
                upvalue_count = child.num_upvalues if child is not None else 0
                self._track_register_from_usage(proto, tracked, inst.a, index)
                for offset in range(upvalue_count):
                    capture_index = index + 1 + offset
                    if capture_index >= len(insts):
                        break
                    capture = insts[capture_index]
                    if capture.opcode != LuauOpcode.CAPTURE:
                        break
                    if capture.a in (LuauCaptureType.VAL, LuauCaptureType.REF):
                        self._track_register_from_usage(proto, tracked, capture.b, capture_index)
                index += 1 + upvalue_count
                continue
            index += 1
        return tracked

    def _track_register_from_usage(
        self,
        proto: Proto,
        tracked: Dict[int, int],
        reg: int,
        usage_index: int,
    ) -> None:
        start_pc = self._find_register_tracking_start(proto, reg, usage_index)
        current = tracked.get(reg)
        if current is None or start_pc < current:
            tracked[reg] = start_pc

    def _find_register_tracking_start(
        self,
        proto: Proto,
        reg: int,
        usage_index: int,
    ) -> int:
        fallback_pc = proto.instructions[usage_index].pc
        last_write_pc = fallback_pc
        for index in range(usage_index - 1, -1, -1):
            inst = proto.instructions[index]
            if inst.a != reg:
                continue
            last_write_pc = inst.pc
            if inst.opcode == LuauOpcode.LOADNIL:
                return inst.pc
            if inst.opcode in (
                LuauOpcode.CALL,
                LuauOpcode.MOVE,
                LuauOpcode.NEWTABLE,
                LuauOpcode.DUPTABLE,
                LuauOpcode.NEWCLOSURE,
                LuauOpcode.DUPCLOSURE,
            ):
                return inst.pc
        return last_write_pc

    def _call_result_is_reused(self, insts: List[Instruction], index: int) -> bool:
        inst = insts[index]
        nresults = inst.c - 1 if inst.c > 0 else -1
        if nresults != 1:
            return False

        reg = inst.a
        use_count = 0
        for probe in insts[index + 1:]:
            if self._instruction_writes_reg(probe, reg):
                break
            if self._instruction_reads_reg(probe, reg):
                use_count += 1
                if use_count > 1:
                    return True
        return False

    def _instruction_reads_reg(self, inst: Instruction, reg: int) -> bool:
        try:
            op = LuauOpcode(inst.opcode)
        except ValueError:
            return False

        if op in (
            LuauOpcode.MOVE,
            LuauOpcode.SETGLOBAL,
            LuauOpcode.SETUPVAL,
            LuauOpcode.NOT,
            LuauOpcode.MINUS,
            LuauOpcode.LENGTH,
        ):
            return inst.b == reg or (inst.a == reg and op in (
                LuauOpcode.SETGLOBAL,
                LuauOpcode.SETUPVAL,
            ))

        if op in (
            LuauOpcode.JUMPIF,
            LuauOpcode.JUMPIFNOT,
            LuauOpcode.JUMPXEQKN,
            LuauOpcode.JUMPXEQKS,
            LuauOpcode.JUMPXEQKB,
            LuauOpcode.JUMPXEQKNIL,
        ):
            return inst.a == reg

        if op in (
            LuauOpcode.GETTABLEKS,
            LuauOpcode.GETTABLEN,
            LuauOpcode.NAMECALL,
        ):
            return inst.b == reg

        if op in (
            LuauOpcode.GETTABLE,
            LuauOpcode.SETTABLE,
            LuauOpcode.ADD,
            LuauOpcode.SUB,
            LuauOpcode.MUL,
            LuauOpcode.DIV,
            LuauOpcode.IDIV,
            LuauOpcode.MOD,
            LuauOpcode.POW,
            LuauOpcode.AND,
            LuauOpcode.OR,
        ):
            return inst.b == reg or inst.c == reg or inst.a == reg and op == LuauOpcode.SETTABLE

        if op in (
            LuauOpcode.ADDK,
            LuauOpcode.SUBK,
            LuauOpcode.MULK,
            LuauOpcode.DIVK,
            LuauOpcode.ANDK,
            LuauOpcode.ORK,
        ):
            return inst.b == reg

        if op == LuauOpcode.SETTABLEKS or op == LuauOpcode.SETTABLEN:
            return inst.a == reg or inst.b == reg

        if op == LuauOpcode.SETLIST:
            if inst.a == reg:
                return True
            if inst.c == 0:
                return reg >= inst.b
            return inst.b <= reg < inst.b + max(inst.c - 1, 0)

        if op == LuauOpcode.CONCAT:
            return inst.b <= reg <= inst.c

        if op == LuauOpcode.CALL:
            nargs = inst.b - 1 if inst.b > 0 else -1
            if nargs < 0:
                return reg >= inst.a
            return inst.a <= reg <= inst.a + nargs

        if op == LuauOpcode.RETURN:
            nvals = inst.b - 1 if inst.b > 0 else 0
            return inst.a <= reg < inst.a + nvals

        if op in (
            LuauOpcode.JUMPIFEQ,
            LuauOpcode.JUMPIFNOTEQ,
            LuauOpcode.JUMPIFLE,
            LuauOpcode.JUMPIFLT,
            LuauOpcode.JUMPIFNOTLE,
            LuauOpcode.JUMPIFNOTLT,
        ):
            right_reg = inst.aux & 0xFF if inst.aux is not None else -1
            return inst.a == reg or right_reg == reg

        return False

    def _instruction_writes_reg(self, inst: Instruction, reg: int) -> bool:
        try:
            op = LuauOpcode(inst.opcode)
        except ValueError:
            return False

        if op == LuauOpcode.CALL:
            nresults = inst.c - 1 if inst.c > 0 else -1
            if nresults < 0:
                return reg >= inst.a
            if nresults == 0:
                return False
            return inst.a <= reg < inst.a + nresults

        if op in (
            LuauOpcode.LOADNIL,
            LuauOpcode.LOADB,
            LuauOpcode.LOADN,
            LuauOpcode.LOADK,
            LuauOpcode.LOADKX,
            LuauOpcode.MOVE,
            LuauOpcode.GETGLOBAL,
            LuauOpcode.GETUPVAL,
            LuauOpcode.GETIMPORT,
            LuauOpcode.GETTABLEKS,
            LuauOpcode.GETTABLE,
            LuauOpcode.GETTABLEN,
            LuauOpcode.NAMECALL,
            LuauOpcode.ADD,
            LuauOpcode.SUB,
            LuauOpcode.MUL,
            LuauOpcode.DIV,
            LuauOpcode.IDIV,
            LuauOpcode.MOD,
            LuauOpcode.POW,
            LuauOpcode.ADDK,
            LuauOpcode.SUBK,
            LuauOpcode.MULK,
            LuauOpcode.DIVK,
            LuauOpcode.AND,
            LuauOpcode.OR,
            LuauOpcode.ANDK,
            LuauOpcode.ORK,
            LuauOpcode.CONCAT,
            LuauOpcode.NOT,
            LuauOpcode.MINUS,
            LuauOpcode.LENGTH,
            LuauOpcode.NEWTABLE,
            LuauOpcode.DUPTABLE,
            LuauOpcode.NEWCLOSURE,
            LuauOpcode.DUPCLOSURE,
            LuauOpcode.GETVARARGS,
        ):
            return inst.a == reg

        return False

    def _seed_proto_names(self, proto: Proto, reg_names: Dict[int, str]) -> None:
        if proto.proto_id != self.bc.main_proto_id:
            return
        return_reg = self._find_returned_register(proto)
        if return_reg is not None:
            self._set_reg_name(reg_names, return_reg, "module")
        self._set_reg_name(reg_names, 2, "RemoteEvent")
        self._set_reg_name(reg_names, 3, "RemoteFunction")

    def _find_returned_register(self, proto: Proto) -> Optional[int]:
        for inst in reversed(proto.instructions):
            if inst.opcode == LuauOpcode.RETURN and inst.b == 2:
                return inst.a
        return None

    def _param_name(self, proto: Proto, reg: int) -> str:
        for local_var in proto.local_vars:
            if local_var["reg"] == reg and local_var["start_pc"] == 0:
                return local_var["name"]
        return f"arg{reg + 1}"

    def _reg_name(self, proto: Proto, reg_names: Dict[int, str], reg: int, pc: int) -> str:
        if reg in reg_names:
            return reg_names[reg]
        for local_var in proto.local_vars:
            if local_var["reg"] == reg and local_var["start_pc"] <= pc < local_var["end_pc"]:
                return local_var["name"]
        return f"v{reg}"

    def _set_reg_name(self, reg_names: Dict[int, str], reg: int, suggestion: str) -> None:
        clean = self._sanitize_identifier(suggestion)
        if not clean:
            return
        current = reg_names.get(reg)
        if current and current.startswith("arg"):
            return
        if current in {"RemoteEvent", "RemoteFunction", "module"}:
            return
        if clean in reg_names.values() and current != clean:
            clean = f"{clean}_{reg}"
        reg_names[reg] = clean

    def _sanitize_identifier(self, value: str) -> str:
        filtered = []
        for index, char in enumerate(value):
            if char.isalnum() or char == "_":
                filtered.append(char)
            elif char in ".:/- ":
                filtered.append("_")
        cleaned = "".join(filtered).strip("_")
        if not cleaned:
            return ""
        if cleaned[0].isdigit():
            cleaned = f"v_{cleaned}"
        if cleaned in LUA_KEYWORDS:
            cleaned = f"{cleaned}_value"
        return cleaned

    def _aux_index(self, aux: Optional[int]) -> int:
        return 0 if aux is None else aux & 0xFFFFFF

    def _aux_string(self, proto: Proto, aux: Optional[int]) -> str:
        idx = self._aux_index(aux)
        if 0 <= idx < len(proto.constants):
            const = proto.constants[idx]
            if const.type == LuauConstantType.STRING and isinstance(const.value, str):
                return const.value
        if idx > 0:
            return self.bc.get_string(idx)
        return ""

    def _jump_target_index(
        self,
        insts: List[Instruction],
        pc_to_idx: Dict[int, int],
        inst: Instruction,
    ) -> Optional[int]:
        target_pc = inst.pc + 1 + inst.d
        target_idx = pc_to_idx.get(target_pc)
        if target_idx is not None:
            return target_idx
        for probe_idx, probe in enumerate(insts):
            if probe.pc >= target_pc:
                return probe_idx
        return None

    def _get_const_expr(self, proto: Proto, idx: int) -> Any:
        if idx < 0 or idx >= len(proto.constants):
            return ast.NilLiteral()

        const = proto.constants[idx]
        if const.type == LuauConstantType.NIL:
            return ast.NilLiteral()
        if const.type == LuauConstantType.BOOLEAN:
            return ast.BoolLiteral(bool(const.value))
        if const.type == LuauConstantType.NUMBER:
            return ast.NumberLiteral(const.value)
        if const.type == LuauConstantType.STRING:
            return ast.StringLiteral(const.value)
        if const.type == LuauConstantType.IMPORT:
            return self._resolve_import(proto, const.value)
        if const.type == LuauConstantType.TABLE:
            return ast.TableConstructor()
        if const.type == LuauConstantType.CLOSURE and 0 <= const.value < len(self.bc.protos):
            return self._lift_proto(self.bc.protos[const.value])
        return ast.NilLiteral()

    def _resolve_import(self, proto: Proto, import_value: int) -> Any:
        ids = decode_import_id(import_value)
        if not ids:
            return ast.GlobalRef(f"import_{import_value:x}")

        parts: List[str] = []
        for raw_id in ids:
            if 0 <= raw_id < len(proto.constants):
                const = proto.constants[raw_id]
                if const.type == LuauConstantType.STRING and isinstance(const.value, str):
                    parts.append(const.value)
                    continue
            parts.append(f"id_{raw_id}")

        expr: Any = ast.GlobalRef(parts[0])
        for name in parts[1:]:
            expr = ast.IndexExpr(expr, ast.StringLiteral(name), is_dot=True)
        return expr

    def _fallback_upvalue(self, proto: Proto, index: int) -> Any:
        name = proto.upvalue_names[index] if index < len(proto.upvalue_names) else f"upval_{index}"
        return ast.UpvalueRef(name, index)

    def _suggest_name(self, expr: Any) -> Optional[str]:
        if isinstance(expr, ast.FunctionExpr) and expr.name:
            return expr.name

        if isinstance(expr, ast.MethodCall):
            if expr.method == "GetService" and expr.args and isinstance(expr.args[0], ast.StringLiteral):
                return expr.args[0].value
            if expr.method == "WaitForChild" and expr.args and isinstance(expr.args[0], ast.StringLiteral):
                return expr.args[0].value
            if expr.method.startswith("Is") and len(expr.method) > 2:
                return expr.method[0].lower() + expr.method[1:]

        if isinstance(expr, ast.FunctionCall):
            if isinstance(expr.func, ast.GlobalRef) and expr.func.name == "require" and expr.args:
                return self._name_from_required_path(expr.args[0])
            if (
                isinstance(expr.func, ast.IndexExpr)
                and expr.func.is_dot
                and isinstance(expr.func.obj, ast.GlobalRef)
                and expr.func.obj.name == "Instance"
                and isinstance(expr.func.key, ast.StringLiteral)
                and expr.func.key.value == "new"
                and expr.args
                and isinstance(expr.args[0], ast.StringLiteral)
            ):
                return expr.args[0].value
        return None

    def _name_from_required_path(self, expr: Any) -> Optional[str]:
        if isinstance(expr, ast.IndexExpr) and expr.is_dot and isinstance(expr.key, ast.StringLiteral):
            return expr.key.value
        return None

    def _are_nodes_identical(self, a: Any, b: Any) -> bool:
        if type(a) != type(b):
            return False
        if isinstance(a, list):
            if len(a) != len(b):
                return False
            return all(self._are_nodes_identical(x, y) for x, y in zip(a, b))
        if hasattr(a, "__dict__"):
            dict_a = a.__dict__
            dict_b = b.__dict__
            if dict_a.keys() != dict_b.keys():
                return False
            return all(self._are_nodes_identical(dict_a[k], dict_b[k]) for k in dict_a)
        return a == b

    def _replace_expr_in_stmts(self, stmts: List[Any], old: Any, new: Any) -> List[Any]:
        return [self._replace_expr_in_node(stmt, old, new) for stmt in stmts]

    def _replace_expr_in_node(self, node: Any, old: Any, new: Any) -> Any:
        if self._are_nodes_identical(node, old):
            return new
        if isinstance(node, list):
            return [self._replace_expr_in_node(item, old, new) for item in node]
        if hasattr(node, "__dict__"):
            updates = {
                key: self._replace_expr_in_node(value, old, new)
                for key, value in node.__dict__.items()
            }
            return type(node)(**updates)
        return node

    def _fold_boolean_ast(self, stmts: List[Any]) -> List[Any]:
        """Peephole AST optimization to fold nested if statements into boolean 'and' expressions."""
        folded = []
        for stmt in stmts:
            if isinstance(stmt, ast.IfStat):
                # Recursively fold interior blocks first
                stmt.then_body = self._fold_boolean_ast(stmt.then_body)
                if stmt.else_body:
                    stmt.else_body = self._fold_boolean_ast(stmt.else_body)
                normalized_elseifs = []
                for cond, body in stmt.elseif_clauses:
                    normalized_elseifs.append((self._canonicalize_boolean_expr(cond), self._fold_boolean_ast(body)))
                stmt.elseif_clauses = normalized_elseifs
                
                # Attempt 'and' folding
                while True:
                    if len(stmt.then_body) == 1 and isinstance(stmt.then_body[0], ast.IfStat):
                        inner = stmt.then_body[0]
                        if not stmt.else_body and not stmt.elseif_clauses and not inner.else_body and not inner.elseif_clauses:
                            stmt.condition = ast.BinaryOp("and", stmt.condition, inner.condition)
                            stmt.then_body = inner.then_body
                            continue
                    break
                stmt.condition = self._canonicalize_boolean_expr(stmt.condition)
                stmt = self._collapse_elseif_chain(stmt)
            folded.append(stmt)
            
        # Fold adjacent IfStats with identical bodies into 'or'
        i = 0
        while i < len(folded) - 1:
            curr = folded[i]
            nxt = folded[i+1]
            if (
                isinstance(curr, ast.IfStat) and isinstance(nxt, ast.IfStat)
                and not curr.else_body and not curr.elseif_clauses
                and not nxt.else_body and not nxt.elseif_clauses
            ):
                if self._are_nodes_identical(curr.then_body, nxt.then_body):
                    curr.condition = ast.BinaryOp("or", curr.condition, nxt.condition)
                    curr.condition = self._canonicalize_boolean_expr(curr.condition)
                    folded.pop(i+1)
                    continue
            i += 1

        return folded

    def _collapse_elseif_chain(self, stmt: ast.IfStat) -> ast.IfStat:
        while len(stmt.else_body) == 1 and isinstance(stmt.else_body[0], ast.IfStat):
            nested = stmt.else_body[0]
            stmt.elseif_clauses.append((nested.condition, nested.then_body))
            stmt.elseif_clauses.extend(nested.elseif_clauses)
            stmt.else_body = nested.else_body
        return stmt

    def _canonicalize_boolean_expr(self, expr: Any) -> Any:
        if isinstance(expr, ast.BinaryOp):
            left = self._canonicalize_boolean_expr(expr.left)
            right = self._canonicalize_boolean_expr(expr.right)
            expr = ast.BinaryOp(expr.op, left, right)

            if self._are_nodes_identical(left, right):
                return left

            if expr.op == "or":
                simplified = self._simplify_absorption(expr.left, expr.right, "and")
                if simplified is not None:
                    return simplified
            if expr.op == "and":
                simplified = self._simplify_absorption(expr.left, expr.right, "or")
                if simplified is not None:
                    return simplified
            return expr

        if isinstance(expr, ast.UnaryOp):
            return ast.UnaryOp(expr.op, self._canonicalize_boolean_expr(expr.operand))

        return expr

    def _simplify_absorption(self, left: Any, right: Any, nested_op: str) -> Optional[Any]:
        if any(self._are_nodes_identical(node, right) for node in self._flatten_boolean_chain(left, nested_op)):
            return right
        if any(self._are_nodes_identical(node, left) for node in self._flatten_boolean_chain(right, nested_op)):
            return left
        return None

    def _flatten_boolean_chain(self, expr: Any, op: str) -> List[Any]:
        if isinstance(expr, ast.BinaryOp) and expr.op == op:
            return self._flatten_boolean_chain(expr.left, op) + self._flatten_boolean_chain(expr.right, op)
        return [expr]
