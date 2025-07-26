package main

import (
	"embed"
	"fmt"
	"go/types"
	"io"
	"sort"
	"strings"
	"text/template"

	"github.com/ettle/strcase"
)

//go:embed templates.go.tmpl
var templatesFS embed.FS

// Syrup generates method mocks and mock.Call wrapper.
type Syrup struct {
	PkgPath       string
	InterfaceName string
	Method        *types.Func
	Signature     *types.Signature
	TypeParams    *types.TypeParamList
}

// Call generates mock.Call wrapper.
func (s Syrup) Call(writer io.Writer, methods []*types.Func) error {
	err := s.callBase(writer)
	if err != nil {
		return err
	}

	err = s.typedReturns(writer)
	if err != nil {
		return err
	}

	err = s.returnsFn(writer)
	if err != nil {
		return err
	}

	err = s.typedRun(writer)
	if err != nil {
		return err
	}

	err = s.callMethodsOn(writer, methods)
	if err != nil {
		return err
	}

	return s.callMethodOnRaw(writer, methods)
}

// MockMethod generates method mocks.
func (s Syrup) MockMethod(writer io.Writer) error {
	err := s.mockedMethod(writer)
	if err != nil {
		return err
	}

	err = s.methodOn(writer)
	if err != nil {
		return err
	}

	return s.methodOnRaw(writer)
}

// getTypeParamsUse returns type parameters for usage in method receivers.
func (s Syrup) getTypeParamsUse() string {
	if s.TypeParams == nil || s.TypeParams.Len() == 0 {
		return ""
	}

	var names []string
	for i := range s.TypeParams.Len() {
		tp := s.TypeParams.At(i)
		names = append(names, tp.Obj().Name())
	}
	return "[" + strings.Join(names, ", ") + "]"
}

func (s Syrup) mockedMethod(writer io.Writer) error {
	base := template.New("templates").Funcs(template.FuncMap{
		"ToGoCamel":  strcase.ToGoCamel,
		"ToGoPascal": strcase.ToGoPascal,
	})

	tmpl, err := base.ParseFS(templatesFS, "templates.go.tmpl")
	if err != nil {
		return err
	}

	params := s.Signature.Params()
	results := s.Signature.Results()

	// Generate parameter data
	var paramData []map[string]interface{}
	var callArgs []string
	for i := range params.Len() {
		param := params.At(i)
		isContext := param.Type().String() == contextType

		var name string
		if isContext {
			name = "_"
		} else {
			name = getParamName(param, i)
			callArgs = append(callArgs, name)
		}

		paramData = append(paramData, map[string]interface{}{
			"Name":      name,
			"Type":      s.getTypeName(param.Type(), i == params.Len()-1),
			"IsContext": isContext,
		})
	}

	// Generate result data
	var resultData []map[string]string
	for i := range results.Len() {
		rType := results.At(i).Type()
		resultData = append(resultData, map[string]string{
			"Name": getResultName(results.At(i), i),
			"Type": s.getTypeName(rType, false),
		})
	}

	data := map[string]interface{}{
		"InterfaceName": s.InterfaceName,
		"MethodName":    s.Method.Name(),
		"TypeParamsUse": s.getTypeParamsUse(),
		"Params":        paramData,
		"Results":       resultData,
		"CallArgs":      callArgs,
		"FnSignature":   s.createFuncSignature(params, results),
		"IsVariadic":    s.Signature.Variadic(),
	}

	return tmpl.ExecuteTemplate(writer, "mockedMethod", data)
}

func (s Syrup) methodOn(writer io.Writer) error {
	base := template.New("templates").Funcs(template.FuncMap{
		"ToGoCamel":  strcase.ToGoCamel,
		"ToGoPascal": strcase.ToGoPascal,
	})

	tmpl, err := base.ParseFS(templatesFS, "templates.go.tmpl")
	if err != nil {
		return err
	}

	params := s.Signature.Params()

	// Generate parameter data
	var paramData []map[string]string
	var callArgs []string
	for i := range params.Len() {
		param := params.At(i)

		if param.Type().String() == contextType {
			continue
		}

		name := getParamName(param, i)
		paramData = append(paramData, map[string]string{
			"Name": name,
			"Type": s.getTypeName(param.Type(), i == params.Len()-1),
		})

		// Function parameters use mock.Anything, others use the parameter name
		if _, ok := param.Type().(*types.Signature); ok {
			callArgs = append(callArgs, "mock.Anything")
		} else {
			callArgs = append(callArgs, name)
		}
	}

	data := map[string]interface{}{
		"InterfaceName": s.InterfaceName,
		"MethodName":    s.Method.Name(),
		"TypeParamsUse": s.getTypeParamsUse(),
		"Params":        paramData,
		"CallArgs":      callArgs,
	}

	return tmpl.ExecuteTemplate(writer, "methodOn", data)
}

func (s Syrup) methodOnRaw(writer io.Writer) error {
	base := template.New("templates").Funcs(template.FuncMap{
		"ToGoCamel":  strcase.ToGoCamel,
		"ToGoPascal": strcase.ToGoPascal,
	})

	tmpl, err := base.ParseFS(templatesFS, "templates.go.tmpl")
	if err != nil {
		return err
	}

	params := s.Signature.Params()

	// Generate parameter data
	var paramData []map[string]string
	var callArgs []string
	for i := range params.Len() {
		param := params.At(i)

		if param.Type().String() == contextType {
			continue
		}

		name := getParamName(param, i)
		paramData = append(paramData, map[string]string{
			"Name": name,
			"Type": "interface{}", // For raw methods, all params are interface{}
		})

		// Function parameters use mock.Anything, others use the parameter name
		if _, ok := param.Type().(*types.Signature); ok {
			callArgs = append(callArgs, "mock.Anything")
		} else {
			callArgs = append(callArgs, name)
		}
	}

	data := map[string]interface{}{
		"InterfaceName": s.InterfaceName,
		"MethodName":    s.Method.Name(),
		"TypeParamsUse": s.getTypeParamsUse(),
		"Params":        paramData,
		"CallArgs":      callArgs,
	}

	return tmpl.ExecuteTemplate(writer, "methodOnRaw", data)
}

func (s Syrup) callBase(writer io.Writer) error {
	base := template.New("templates").Funcs(template.FuncMap{
		"ToGoCamel":  strcase.ToGoCamel,
		"ToGoPascal": strcase.ToGoPascal,
	})

	tmpl, err := base.ParseFS(templatesFS, "templates.go.tmpl")
	if err != nil {
		return err
	}

	// Generate type parameter declarations and usage
	typeParamsDecl := ""
	typeParamsUse := ""
	if s.TypeParams != nil && s.TypeParams.Len() > 0 {
		var params []string
		var names []string
		for i := range s.TypeParams.Len() {
			tp := s.TypeParams.At(i)
			params = append(params, tp.Obj().Name()+" "+tp.Constraint().String())
			names = append(names, tp.Obj().Name())
		}
		typeParamsDecl = "[" + strings.Join(params, ", ") + "]"
		typeParamsUse = "[" + strings.Join(names, ", ") + "]"
	}

	data := map[string]string{
		"InterfaceName":  s.InterfaceName,
		"MethodName":     s.Method.Name(),
		"TypeParamsDecl": typeParamsDecl,
		"TypeParamsUse":  typeParamsUse,
	}

	return tmpl.ExecuteTemplate(writer, "callBase", data)
}

func (s Syrup) typedReturns(writer io.Writer) error {
	results := s.Signature.Results()
	if results.Len() <= 0 {
		return nil
	}

	base := template.New("templates").Funcs(template.FuncMap{
		"ToGoCamel":  strcase.ToGoCamel,
		"ToGoPascal": strcase.ToGoPascal,
	})

	tmpl, err := base.ParseFS(templatesFS, "templates.go.tmpl")
	if err != nil {
		return err
	}

	// Generate return parameters data
	var returnParams []map[string]string
	for i := range results.Len() {
		rName := string(rune(int('a') + i))
		returnParams = append(returnParams, map[string]string{
			"Name": rName,
			"Type": s.getTypeName(results.At(i).Type(), false),
		})
	}

	data := map[string]interface{}{
		"InterfaceName": s.InterfaceName,
		"MethodName":    s.Method.Name(),
		"TypeParamsUse": s.getTypeParamsUse(),
		"ReturnParams":  returnParams,
	}

	return tmpl.ExecuteTemplate(writer, "typedReturns", data)
}

func (s Syrup) typedRun(writer io.Writer) error {
	params := s.Signature.Params()

	base := template.New("templates").Funcs(template.FuncMap{
		"ToGoCamel":  strcase.ToGoCamel,
		"ToGoPascal": strcase.ToGoPascal,
	})

	tmpl, err := base.ParseFS(templatesFS, "templates.go.tmpl")
	if err != nil {
		return err
	}

	// Generate input parameters data
	var inputParams []map[string]interface{}
	var pos int
	for i := range params.Len() {
		param := params.At(i)
		pType := param.Type()

		if pType.String() == contextType {
			continue
		}

		paramName := "_" + getParamName(param, i)
		inputParams = append(inputParams, map[string]interface{}{
			"Name":     paramName,
			"Type":     s.getTypeName(pType, false),
			"Position": pos,
		})
		pos++
	}

	data := map[string]interface{}{
		"InterfaceName": s.InterfaceName,
		"MethodName":    s.Method.Name(),
		"TypeParamsUse": s.getTypeParamsUse(),
		"FnSignature":   s.createFuncSignature(params, nil),
		"InputParams":   inputParams,
		"IsVariadic":    s.Signature.Variadic(),
	}

	return tmpl.ExecuteTemplate(writer, "typedRun", data)
}

func (s Syrup) returnsFn(writer io.Writer) error {
	results := s.Signature.Results()
	if results.Len() < 1 {
		return nil
	}

	base := template.New("templates").Funcs(template.FuncMap{
		"ToGoCamel":  strcase.ToGoCamel,
		"ToGoPascal": strcase.ToGoPascal,
	})

	tmpl, err := base.ParseFS(templatesFS, "templates.go.tmpl")
	if err != nil {
		return err
	}

	params := s.Signature.Params()

	data := map[string]interface{}{
		"InterfaceName": s.InterfaceName,
		"MethodName":    s.Method.Name(),
		"TypeParamsUse": s.getTypeParamsUse(),
		"FnSignature":   s.createFuncSignature(params, results),
	}

	return tmpl.ExecuteTemplate(writer, "returnsFn", data)
}

func (s Syrup) callMethodsOn(writer io.Writer, methods []*types.Func) error {
	base := template.New("templates").Funcs(template.FuncMap{
		"ToGoCamel":  strcase.ToGoCamel,
		"ToGoPascal": strcase.ToGoPascal,
	})

	tmpl, err := base.ParseFS(templatesFS, "templates.go.tmpl")
	if err != nil {
		return err
	}

	typeParamsUse := s.getTypeParamsUse()
	callType := fmt.Sprintf("%s%sCall%s", strcase.ToGoCamel(s.InterfaceName), s.Method.Name(), typeParamsUse)

	// Generate method data for template
	var methodData []map[string]interface{}
	for _, method := range methods {
		sign := method.Type().(*types.Signature)
		params := sign.Params()

		var paramData []map[string]string
		for i := range params.Len() {
			param := params.At(i)

			if param.Type().String() == contextType {
				continue
			}

			name := getParamName(param, i)
			paramData = append(paramData, map[string]string{
				"Name": name,
				"Type": s.getTypeName(param.Type(), i == params.Len()-1),
			})
		}

		methodData = append(methodData, map[string]interface{}{
			"Name":       method.Name(),
			"Params":     paramData,
			"IsVariadic": sign.Variadic(),
		})
	}

	data := map[string]interface{}{
		"InterfaceName": s.InterfaceName,
		"CallType":      callType,
		"TypeParamsUse": typeParamsUse,
		"Methods":       methodData,
	}

	return tmpl.ExecuteTemplate(writer, "callMethodsOn", data)
}

func (s Syrup) callMethodOnRaw(writer io.Writer, methods []*types.Func) error {
	base := template.New("templates").Funcs(template.FuncMap{
		"ToGoCamel":  strcase.ToGoCamel,
		"ToGoPascal": strcase.ToGoPascal,
	})

	tmpl, err := base.ParseFS(templatesFS, "templates.go.tmpl")
	if err != nil {
		return err
	}

	typeParamsUse := s.getTypeParamsUse()
	callType := fmt.Sprintf("%s%sCall%s", strcase.ToGoCamel(s.InterfaceName), s.Method.Name(), typeParamsUse)

	// Generate method data for template
	var methodData []map[string]interface{}
	for _, method := range methods {
		sign := method.Type().(*types.Signature)
		params := sign.Params()

		var paramData []map[string]string
		for i := range params.Len() {
			param := params.At(i)

			if param.Type().String() == contextType {
				continue
			}

			name := getParamName(param, i)
			paramData = append(paramData, map[string]string{
				"Name": name,
				"Type": "interface{}", // For raw methods, all params are interface{}
			})
		}

		methodData = append(methodData, map[string]interface{}{
			"Name":       method.Name(),
			"Params":     paramData,
			"IsVariadic": sign.Variadic(),
		})
	}

	data := map[string]interface{}{
		"InterfaceName": s.InterfaceName,
		"CallType":      callType,
		"TypeParamsUse": typeParamsUse,
		"Methods":       methodData,
	}

	return tmpl.ExecuteTemplate(writer, "callMethodOnRaw", data)
}

func (s Syrup) getTypeName(t types.Type, last bool) string {
	switch v := t.(type) {
	case *types.Basic:
		return v.Name()

	case *types.Slice:
		if s.Signature.Variadic() && last {
			return "..." + s.getTypeName(v.Elem(), false)
		}

		return "[]" + s.getTypeName(v.Elem(), false)

	case *types.Map:
		return "map[" + s.getTypeName(v.Key(), false) + "]" + s.getTypeName(v.Elem(), false)

	case *types.Named:
		return s.getNamedTypeName(v)

	case *types.Pointer:
		return "*" + s.getTypeName(v.Elem(), false)

	case *types.Struct:
		return v.String()

	case *types.Interface:
		return v.String()

	case *types.Signature:
		fn := "func(" + strings.Join(s.getTupleTypes(v.Params()), ",") + ")"

		if v.Results().Len() > 0 {
			fn += " (" + strings.Join(s.getTupleTypes(v.Results()), ",") + ")"
		}

		return fn

	case *types.Chan:
		return s.getChanTypeName(v)

	case *types.Array:
		return fmt.Sprintf("[%d]%s", v.Len(), s.getTypeName(v.Elem(), false))

	case *types.TypeParam:
		return v.Obj().Name()

	default:
		panic(fmt.Sprintf("OOPS %[1]T %[1]s", t))
	}
}

func (s Syrup) getTupleTypes(t *types.Tuple) []string {
	var tupleTypes []string
	for i := range t.Len() {
		param := t.At(i)

		tupleTypes = append(tupleTypes, s.getTypeName(param.Type(), false))
	}

	return tupleTypes
}

func (s Syrup) getNamedTypeName(t *types.Named) string {
	if t.Obj() != nil && t.Obj().Pkg() != nil {
		if t.Obj().Pkg().Path() == s.PkgPath {
			return t.Obj().Name()
		}
		return t.Obj().Pkg().Name() + "." + t.Obj().Name()
	}

	name := t.String()

	i := strings.LastIndex(t.String(), "/")
	if i > -1 {
		name = name[i+1:]
	}
	return name
}

func (s Syrup) getChanTypeName(t *types.Chan) string {
	var typ string
	switch t.Dir() {
	case types.SendRecv:
		typ = "chan"
	case types.SendOnly:
		typ = "chan<-"
	case types.RecvOnly:
		typ = "<-chan"
	}

	return typ + " " + s.getTypeName(t.Elem(), false)
}

func (s Syrup) createFuncSignature(params, results *types.Tuple) string {
	fnSign := "func("
	for i := range params.Len() {
		param := params.At(i)
		if param.Type().String() == contextType {
			continue
		}

		fnSign += s.getTypeName(param.Type(), i == params.Len()-1)

		if i+1 < params.Len() {
			fnSign += ", "
		}
	}
	fnSign += ") "

	if results != nil {
		fnSign += "("
		for i := range results.Len() {
			rType := results.At(i).Type()
			fnSign += s.getTypeName(rType, false)
			if i+1 < results.Len() {
				fnSign += ", "
			}
		}
		fnSign += ")"
	}

	return fnSign
}

func writeImports(writer io.Writer, descPkg PackageDesc) error {
	base := template.New("templates").Funcs(template.FuncMap{
		"ToGoCamel":  strcase.ToGoCamel,
		"ToGoPascal": strcase.ToGoPascal,
	})

	tmpl, err := base.ParseFS(templatesFS, "templates.go.tmpl")
	if err != nil {
		return err
	}

	data := map[string]interface{}{
		"Name":    descPkg.Pkg.Name(),
		"Imports": quickGoImports(descPkg),
	}
	return tmpl.ExecuteTemplate(writer, "imports", data)
}

func writeMockBase(writer io.Writer, interfaceDesc InterfaceDesc, exported bool) error {
	base := template.New("templates").Funcs(template.FuncMap{
		"ToGoCamel":  strcase.ToGoCamel,
		"ToGoPascal": strcase.ToGoPascal,
	})

	constructorPrefix := "new"
	if exported {
		constructorPrefix = "New"
	}

	// Generate type parameter declarations and usage
	typeParamsDecl := ""
	typeParamsUse := ""
	if interfaceDesc.TypeParams != nil && interfaceDesc.TypeParams.Len() > 0 {
		var params []string
		var names []string
		for i := range interfaceDesc.TypeParams.Len() {
			tp := interfaceDesc.TypeParams.At(i)
			params = append(params, tp.Obj().Name()+" "+tp.Constraint().String())
			names = append(names, tp.Obj().Name())
		}
		typeParamsDecl = "[" + strings.Join(params, ", ") + "]"
		typeParamsUse = "[" + strings.Join(names, ", ") + "]"
	}

	tmpl, err := base.ParseFS(templatesFS, "templates.go.tmpl")
	if err != nil {
		return err
	}

	data := map[string]interface{}{
		"InterfaceName":     interfaceDesc.Name,
		"ConstructorPrefix": constructorPrefix,
		"TypeParamsDecl":    typeParamsDecl,
		"TypeParamsUse":     typeParamsUse,
	}
	return tmpl.ExecuteTemplate(writer, "mockBase", data)
}

func quickGoImports(descPkg PackageDesc) []string {
	imports := []string{
		"", // to separate std imports than the others
	}

	descPkg.Imports["testing"] = struct{}{}                          // require by test
	descPkg.Imports["time"] = struct{}{}                             // require by `WaitUntil(w <-chan time.Time)`
	descPkg.Imports["github.com/stretchr/testify/mock"] = struct{}{} // require by mock

	for imp := range descPkg.Imports {
		imports = append(imports, imp)
	}

	sort.Slice(imports, func(i, j int) bool {
		if imports[i] == "" {
			return strings.Contains(imports[j], ".")
		}
		if imports[j] == "" {
			return !strings.Contains(imports[i], ".")
		}

		if strings.Contains(imports[i], ".") && !strings.Contains(imports[j], ".") {
			return false
		}
		if !strings.Contains(imports[i], ".") && strings.Contains(imports[j], ".") {
			return true
		}

		return imports[i] < imports[j]
	})

	return imports
}

func getParamName(tVar *types.Var, i int) string {
	if tVar.Name() == "" {
		return fmt.Sprintf("%sParam", string(rune('a'+i)))
	}
	return tVar.Name()
}

func getResultName(tVar *types.Var, i int) string {
	if tVar.Name() == "" {
		return fmt.Sprintf("_r%s%d", string(rune('a'+i)), i)
	}
	return tVar.Name()
}

// Writer is a wrapper around Print+ functions.
type Writer struct {
	writer io.Writer
	err    error
}

// Err returns error from the other methods.
func (w *Writer) Err() error {
	return w.err
}

// Print formats using the default formats for its operands and writes to standard output.
func (w *Writer) Print(a ...interface{}) {
	if w.err != nil {
		return
	}

	_, w.err = fmt.Fprint(w.writer, a...)
}

// Printf formats according to a format specifier and writes to standard output.
func (w *Writer) Printf(pattern string, a ...interface{}) {
	if w.err != nil {
		return
	}

	_, w.err = fmt.Fprintf(w.writer, pattern, a...)
}

// Println formats using the default formats for its operands and writes to standard output.
func (w *Writer) Println(a ...interface{}) {
	if w.err != nil {
		return
	}

	_, w.err = fmt.Fprintln(w.writer, a...)
}
