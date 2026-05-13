package lifter

import (
	"fmt"

	"Geckocompiler/ast"
	bc "Geckocompiler/bytecode"
)

// getConstExpr converts a constant table entry into an AST expression.
func (l *Lifter) getConstExpr(proto *bc.Proto, idx int) ast.Expr {
	if idx < 0 || idx >= len(proto.Constants) {
		return ast.NilLiteral{}
	}

	c := proto.Constants[idx]
	switch c.Type {
	case bc.ConstNil:
		return ast.NilLiteral{}
	case bc.ConstBoolean:
		if bv, ok := c.Value.(bool); ok {
			return ast.BoolLiteral{Value: bv}
		}
		return ast.BoolLiteral{Value: false}
	case bc.ConstNumber:
		if nv, ok := c.Value.(float64); ok {
			return ast.NumberLiteral{Value: nv}
		}
		return ast.NumberLiteral{Value: 0}
	case bc.ConstString:
		if sv, ok := c.Value.(string); ok {
			return ast.StringLiteral{Value: sv}
		}
		return ast.StringLiteral{Value: ""}
	case bc.ConstImport:
		if iv, ok := c.Value.(uint32); ok {
			return l.resolveImport(proto, iv)
		}
		return ast.GlobalRef{Name: fmt.Sprintf("import_%d", idx)}
	case bc.ConstTable:
		return &ast.TableConstructor{}
	case bc.ConstClosure:
		if pid, ok := c.Value.(int); ok && pid >= 0 && pid < len(l.BC.Protos) {
			return l.liftProto(l.BC.Protos[pid], nil)
		}
		return ast.NilLiteral{}
	}
	return ast.NilLiteral{}
}

// resolveImport decodes a GETIMPORT encoded value into chained IndexExprs.
func (l *Lifter) resolveImport(proto *bc.Proto, importValue uint32) ast.Expr {
	ids := bc.DecodeImportID(importValue)
	if len(ids) == 0 {
		return ast.GlobalRef{Name: fmt.Sprintf("import_%x", importValue)}
	}

	parts := make([]string, 0, len(ids))
	for _, rawID := range ids {
		if rawID >= 0 && rawID < len(proto.Constants) {
			c := proto.Constants[rawID]
			if c.Type == bc.ConstString {
				if sv, ok := c.Value.(string); ok {
					parts = append(parts, sv)
					continue
				}
			}
		}
		parts = append(parts, fmt.Sprintf("id_%d", rawID))
	}

	var expr ast.Expr = ast.GlobalRef{Name: parts[0]}
	for _, name := range parts[1:] {
		expr = ast.IndexExpr{Obj: expr, Key: ast.StringLiteral{Value: name}, IsDot: true}
	}
	return expr
}

// auxIndex extracts the constant index from an AUX word.
func auxIndex(aux int) int {
	if aux < 0 {
		return 0
	}
	return aux & 0xFFFFFF
}

// auxString resolves an AUX word to a string value from the constant table.
func (l *Lifter) auxString(proto *bc.Proto, aux int) string {
	idx := auxIndex(aux)
	if idx >= 0 && idx < len(proto.Constants) {
		c := proto.Constants[idx]
		if c.Type == bc.ConstString {
			if sv, ok := c.Value.(string); ok {
				return sv
			}
		}
	}
	if idx > 0 {
		return l.BC.GetString(idx)
	}
	return ""
}

// fallbackUpvalue creates an UpvalueRef when the binding can't be resolved.
func (l *Lifter) fallbackUpvalue(proto *bc.Proto, index int) ast.Expr {
	name := fmt.Sprintf("upval_%d", index)
	if index < len(proto.UpvalueNames) {
		name = proto.UpvalueNames[index]
	}
	return ast.UpvalueRef{Name: name, Index: index}
}

// appendNamedTableField adds a named field to an active table builder.
func (l *Lifter) appendNamedTableField(state *blockState, reg int, keyName string, value ast.Expr) bool {
	builder := state.tableBuilders[reg]
	if builder == nil || state.sealedTableBuilders[id(builder)] {
		return false
	}
	if _, ok := state.regNames[reg]; ok {
		return false
	}

	// Update existing field if already present
	for i, field := range builder.Fields {
		if field.IsStringKey {
			if sk, ok := field.Key.(ast.StringLiteral); ok && sk.Value == keyName {
				builder.Fields[i].Value = value
				return true
			}
		}
	}

	builder.Fields = append(builder.Fields, ast.TableField{
		Key: ast.StringLiteral{Value: keyName}, Value: value, IsStringKey: true,
	})
	return true
}

// appendTableField adds a keyed field to an active table builder.
func (l *Lifter) appendTableField(state *blockState, reg int, key, value ast.Expr) bool {
	builder := state.tableBuilders[reg]
	if builder == nil || state.sealedTableBuilders[id(builder)] {
		return false
	}
	if _, ok := state.regNames[reg]; ok {
		return false
	}
	builder.Fields = append(builder.Fields, ast.TableField{Key: key, Value: value})
	return true
}
