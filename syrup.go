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

// BaseTemplateData contains the most commonly used template fields.
type BaseTemplateData struct {
	InterfaceName string
	MethodName    string
	TypeParamsUse string
}

// Parameter represents a method parameter with all possible attributes.
type Parameter struct {
	Name      string
	Type      string
	IsContext bool
	Position  int
}

// Result represents a method return value.
type Result struct {
	Name string
	Type string
}

// Method represents a method for template generation.
type Method struct {
	Name       string
	Params     []Parameter
	IsVariadic bool
}

// TypeParamsInfo contains type parameter information for templates.
type TypeParamsInfo struct {
	Declaration string // [T any, U comparable]
	Usage       string // [T, U]
}

// ImportsData contains data for imports template.
type ImportsData struct {
	Name    string
	Imports []string
}

// MockBaseData contains data for mockBase template.
type MockBaseData struct {
	InterfaceName     string
	ConstructorPrefix string
	TypeParamsDecl    string
	TypeParamsUse     string
}

// CombinedCallData contains all data needed for Call template execution.
type CombinedCallData struct {
	BaseTemplateData

	TypeParamsDecl      string
	ReturnParams        []Parameter
	ReturnsFnSignature  string
	TypedRunFnSignature string
	InputParams         []Parameter
	IsVariadic          bool
	CallType            string
	Methods             []Method
	HasReturns          bool
}

// CombinedMockMethodData contains all data needed for MockMethod template execution.
type CombinedMockMethodData struct {
	BaseTemplateData

	Params      []Parameter
	Results     []Result
	CallArgs    []string // For _m.Called() and _rf() calls - parameter names.
	OnCallArgs  []string // For _m.Mock.On() calls - mock.Anything for functions.
	FnSignature string
	IsVariadic  bool
}

// Syrup generates method mocks and mock.Call wrapper.
type Syrup struct {
	PkgPath       string
	InterfaceName string
	Method        *types.Func
	Signature     *types.Signature
	TypeParams    *types.TypeParamList
	Template      *template.Template
}

// Call generates mock.Call wrapper.
func (s Syrup) Call(writer io.Writer, methods []*types.Func) error {
	params := s.Signature.Params()
	results := s.Signature.Results()

	// Generate type parameter declarations and usage
	typeParamsDecl := ""
	typeParamsUse := s.getTypeParamsUse()
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

	// Generate return parameters
	var returnParams []Parameter
	hasReturns := results.Len() > 0
	for i := range results.Len() {
		rName := string(rune(int('a') + i))
		returnParams = append(returnParams, Parameter{
			Name: rName,
			Type: s.getTypeName(results.At(i).Type(), false),
		})
	}

	// Generate input parameters for TypedRun
	var inputParams []Parameter
	var pos int
	for i := range params.Len() {
		param := params.At(i)
		pType := param.Type()

		if pType.String() == contextType {
			continue
		}

		paramName := "_" + getParamName(param, i)
		inputParams = append(inputParams, Parameter{
			Name:     paramName,
			Type:     s.getTypeName(pType, false),
			Position: pos,
		})
		pos++
	}

	// Generate methods data
	var methodData []Method
	for _, method := range methods {
		sign := method.Type().(*types.Signature)
		mParams := sign.Params()

		var paramData []Parameter
		for i := range mParams.Len() {
			param := mParams.At(i)
			isContext := param.Type().String() == contextType

			name := getParamName(param, i)
			paramData = append(paramData, Parameter{
				Name:      name,
				Type:      s.getTypeName(param.Type(), i == mParams.Len()-1),
				IsContext: isContext,
			})
		}

		methodData = append(methodData, Method{
			Name:       method.Name(),
			Params:     paramData,
			IsVariadic: sign.Variadic(),
		})
	}

	callType := fmt.Sprintf("%s%sCall%s", strcase.ToGoCamel(s.InterfaceName), s.Method.Name(), typeParamsUse)

	data := CombinedCallData{
		BaseTemplateData: BaseTemplateData{
			InterfaceName: s.InterfaceName,
			MethodName:    s.Method.Name(),
			TypeParamsUse: typeParamsUse,
		},
		TypeParamsDecl:      typeParamsDecl,
		ReturnParams:        returnParams,
		ReturnsFnSignature:  s.createFuncSignature(params, results),
		TypedRunFnSignature: s.createFuncSignature(params, nil),
		InputParams:         inputParams,
		IsVariadic:          s.Signature.Variadic(),
		CallType:            callType,
		Methods:             methodData,
		HasReturns:          hasReturns,
	}

	return s.Template.ExecuteTemplate(writer, "combinedCall", data)
}

// MockMethod generates method mocks.
func (s Syrup) MockMethod(writer io.Writer) error {
	params := s.Signature.Params()
	results := s.Signature.Results()

	// Generate parameter data (including non-context params for On methods)
	var paramsData []Parameter
	var callArgs []string   // For _m.Called() and _rf() calls - always use parameter names
	var onCallArgs []string // For _m.Mock.On() calls - use mock.Anything for functions
	for i := range params.Len() {
		param := params.At(i)
		isContext := param.Type().String() == contextType

		var name string
		if isContext {
			name = "_"
		} else {
			name = getParamName(param, i)
			callArgs = append(callArgs, name)

			// Function parameters use mock.Anything in On calls, others use the parameter name
			if _, ok := param.Type().(*types.Signature); ok {
				onCallArgs = append(onCallArgs, "mock.Anything")
			} else {
				onCallArgs = append(onCallArgs, name)
			}
		}

		// Add all params to paramsData for template
		paramsData = append(paramsData, Parameter{
			Name:      name,
			Type:      s.getTypeName(param.Type(), i == params.Len()-1),
			IsContext: isContext,
		})
	}

	// Generate result data
	var resultsData []Result
	for i := range results.Len() {
		rType := results.At(i).Type()
		resultsData = append(resultsData, Result{
			Name: getResultName(results.At(i), i),
			Type: s.getTypeName(rType, false),
		})
	}

	data := CombinedMockMethodData{
		BaseTemplateData: BaseTemplateData{
			InterfaceName: s.InterfaceName,
			MethodName:    s.Method.Name(),
			TypeParamsUse: s.getTypeParamsUse(),
		},
		Params:      paramsData,
		Results:     resultsData,
		CallArgs:    callArgs,
		OnCallArgs:  onCallArgs,
		FnSignature: s.createFuncSignature(params, results),
		IsVariadic:  s.Signature.Variadic(),
	}

	return s.Template.ExecuteTemplate(writer, "combinedMockMethod", data)
}

// WriteImports generates package imports using the Syrup's template.
func (s Syrup) WriteImports(writer io.Writer, descPkg PackageDesc) error {
	data := ImportsData{
		Name:    descPkg.Pkg.Name(),
		Imports: quickGoImports(descPkg),
	}
	return s.Template.ExecuteTemplate(writer, "imports", data)
}

// WriteMockBase generates mock base struct and constructor using the Syrup's template.
func (s Syrup) WriteMockBase(writer io.Writer, interfaceDesc InterfaceDesc, exported bool) error {
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

	data := MockBaseData{
		InterfaceName:     interfaceDesc.Name,
		ConstructorPrefix: constructorPrefix,
		TypeParamsDecl:    typeParamsDecl,
		TypeParamsUse:     typeParamsUse,
	}
	return s.Template.ExecuteTemplate(writer, "mockBase", data)
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
	for param := range t.Variables() {
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

func getTemplate(templateFile string) (*template.Template, error) {
	base := template.New("templates").Funcs(template.FuncMap{
		"ToGoCamel":  strcase.ToGoCamel,
		"ToGoPascal": strcase.ToGoPascal,
	})

	if templateFile != "" {
		// Use custom template file
		return base.ParseFiles(templateFile)
	}

	// Use embedded template
	return base.ParseFS(templatesFS, "templates.go.tmpl")
}
