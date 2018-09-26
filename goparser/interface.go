package goparser

import (
	"go/types"
	"sort"
	"strings"

	gobatis "github.com/runner-mei/GoBatis"
)

func IsIgnoreStructTypes(typ types.Type) bool {
	if _, ok := typ.(*types.Struct); ok {
		return false
	}

	if ptr, ok := typ.(*types.Pointer); ok {
		return IsIgnoreStructTypes(ptr.Elem())
	}

	if m, ok := typ.(*types.Map); ok {
		return IsIgnoreStructTypes(m.Elem())
	}

	if ar, ok := typ.(*types.Array); ok {
		return IsIgnoreStructTypes(ar.Elem())
	}

	if slice, ok := typ.(*types.Slice); ok {
		return IsIgnoreStructTypes(slice.Elem())
	}

	if named, ok := typ.(*types.Named); ok {
		typName := named.Obj().Pkg().Name() + "." + named.Obj().Name()
		for _, nm := range []string{"time.Time", "net.IP"} {
			if nm == typName {
				return true
			}
		}
		return false
	}

	return false
}

func IsStructType(typ types.Type) bool {
	if _, ok := typ.(*types.Struct); ok {
		return true
	}

	if ptr, ok := typ.(*types.Pointer); ok {
		return IsStructType(ptr.Elem())
	}

	if m, ok := typ.(*types.Map); ok {
		return IsStructType(m.Elem())
	}

	if ar, ok := typ.(*types.Array); ok {
		return IsStructType(ar.Elem())
	}

	if slice, ok := typ.(*types.Slice); ok {
		return IsStructType(slice.Elem())
	}

	if named, ok := typ.(*types.Named); ok {
		return IsStructType(named.Underlying())
	}

	return false
}

func GetElemType(typ types.Type) types.Type {
	switch t := typ.(type) {
	case *types.Struct:
		return t
	case *types.Array:
		return GetElemType(t.Elem())
	case *types.Slice:
		return GetElemType(t.Elem())
	case *types.Pointer:
		return GetElemType(t.Elem())
	case *types.Map:
		return GetElemType(t.Elem())
	case *types.Named:
		return t // underlyingType(t.Underlying())
	default:
		return nil
	}
}

type Interface struct {
	File *File `json:"-"`
	Pos  int
	Name string

	Methods []*Method
}

func (itf *Interface) DetectRecordType(method *Method) types.Type {
	return itf.detectRecordType(method, true)
}

func (itf *Interface) detectRecordType(method *Method, fuzzy bool) types.Type {
	if method == nil {
		for _, name := range []string{
			"Get",
			"Insert",
			"List",
			"FindByID",
			"QueryByID",
		} {
			method = itf.MethodByName(name)
			if method != nil {
				typ := itf.detectRecordType(method, false)
				if typ != nil {
					return typ
				}
			}
		}
		return nil
	}

	switch method.StatementType() {
	case gobatis.StatementTypeInsert:
		if len(method.Params.List) == 1 {
			if IsStructType(method.Params.List[0].Type) && !IsIgnoreStructTypes(method.Params.List[0].Type) {
				return GetElemType(method.Params.List[0].Type)
			}
		}

		foundIndex := -1
		for idx := range method.Params.List {
			if method.Params.List[idx].Type.String() == "context.Context" {
				foundIndex = idx
				break
			}
		}

		if foundIndex >= 0 && len(method.Params.List) == 2 {
			for idx := range method.Params.List {
				if method.Params.List[idx].Type.String() == "context.Context" {
					continue
				}

				if IsStructType(method.Params.List[idx].Type) && !IsIgnoreStructTypes(method.Params.List[idx].Type) {
					return GetElemType(method.Params.List[idx].Type)
				}
			}
		}

		return nil
	case gobatis.StatementTypeUpdate:
		if len(method.Params.List) > 0 {
			param := method.Params.List[len(method.Params.List)-1]
			if IsStructType(param.Type) && !IsIgnoreStructTypes(param.Type) {
				return GetElemType(param.Type)
			}
		}
		return itf.detectRecordType(nil, false)
	case gobatis.StatementTypeSelect:
		if len(method.Results.List) == 2 {
			if !IsStructType(method.Results.List[0].Type) &&
				strings.Contains(strings.ToLower(strings.TrimSuffix(method.Name, "Context")), "count") {
				if fuzzy {
					return itf.detectRecordType(nil, false)
				}
				return nil
			}

			resultType := GetElemType(method.Results.List[0].Type)
			if !fuzzy {
				if IsStructType(resultType) && !IsIgnoreStructTypes(resultType) {
					return resultType
				}
				return nil
			}

			fuzzyType := itf.detectRecordType(nil, false)
			if types.Identical(resultType, fuzzyType) {
				return resultType
			}
		}
		return nil
	case gobatis.StatementTypeDelete:
		if fuzzy {
			return itf.detectRecordType(nil, false)
		}
	}
	return nil
}

func (itf *Interface) Print(ctx *PrintContext, sb *strings.Builder) {
	sb.WriteString("type ")
	sb.WriteString(itf.Name)
	sb.WriteString(" interface {")
	var oldIndent string
	if ctx != nil {
		oldIndent = ctx.Indent
		ctx.Indent = ctx.Indent + "	"
	}
	for idx, m := range itf.Methods {
		if idx > 0 {
			sb.WriteString("\r\n")
		}
		sb.WriteString("\r\n")
		m.Print(ctx, true, sb)
	}

	if ctx != nil {
		ctx.Indent = oldIndent
	}
	sb.WriteString("\r\n")
	sb.WriteString("}")
}

func (itf *Interface) String() string {
	var sb strings.Builder
	itf.Print(&PrintContext{}, &sb)
	return sb.String()
}

func (itf *Interface) MethodByName(name string) *Method {
	for idx := range itf.Methods {
		if itf.Methods[idx].Name == name {
			return itf.Methods[idx]
		}
	}
	return nil
}

func (itf *Interface) ReferenceInterfaces() []string {
	var names []string
	for _, m := range itf.Methods {
		if m.Config == nil {
			continue
		}
		if m.Config.Reference == nil {
			continue
		}

		names = append(names, m.Config.Reference.Interface)
	}

	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	offset := 1
	for i := 1; i < len(names); i++ {
		if names[i] == names[i-1] {
			continue
		}

		if offset != i {
			names[offset] = names[i]
		}
		offset++
	}

	names = names[:offset]
	return names
}
