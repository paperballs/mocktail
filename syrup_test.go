package main

import (
	"bytes"
	"go/types"
	"testing"
	"text/template"

	"github.com/ettle/strcase"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestSyrup creates a Syrup instance with realistic test data.
func createTestSyrup(t *testing.T, templateContent string) *Syrup {
	t.Helper()

	// Create a realistic method signature for testing
	// func (m *Mock) GetUser(ctx context.Context, id string, active bool) (*User, error)

	// Create parameter types
	contextType := types.NewNamed(
		types.NewTypeName(0, types.NewPackage("context", "context"), "Context", nil),
		types.NewInterfaceType(nil, nil), nil,
	)
	stringType := types.Typ[types.String]
	boolType := types.Typ[types.Bool]

	// Create result types
	userType := types.NewPointer(types.NewNamed(
		types.NewTypeName(0, types.NewPackage("myapp", "myapp"), "User", nil),
		types.NewStruct(nil, nil), nil,
	))
	errorType := types.Universe.Lookup("error").Type()

	// Create parameters
	params := types.NewTuple(
		types.NewParam(0, nil, "ctx", contextType),
		types.NewParam(0, nil, "id", stringType),
		types.NewParam(0, nil, "active", boolType),
	)

	// Create results
	results := types.NewTuple(
		types.NewParam(0, nil, "user", userType),
		types.NewParam(0, nil, "err", errorType),
	)

	// Create signature
	signature := types.NewSignatureType(nil, nil, nil, params, results, false)

	// Create method
	method := types.NewFunc(0, nil, "GetUser", signature)

	base := template.New("templates").Funcs(template.FuncMap{
		"ToGoCamel":  strcase.ToGoCamel,
		"ToGoPascal": strcase.ToGoPascal,
	})

	var tmpl *template.Template
	var err error
	if templateContent == "" {
		// Use embedded template
		tmpl, err = base.ParseFS(templatesFS, "templates.go.tmpl")
	} else {
		tmpl, err = base.Parse(templateContent)
	}
	require.NoError(t, err)

	// Create Syrup instance

	return New("myapp", "UserRepository", method, signature, nil, tmpl)
}

// createSimpleTestMethods creates a slice of test methods for Call() testing.
func createSimpleTestMethods() []*types.Func {
	// Create simple methods for testing chaining
	stringType := types.Typ[types.String]
	intType := types.Typ[types.Int]

	// func FindByName(name string) *User
	findByNameSig := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(types.NewParam(0, nil, "name", stringType)),
		types.NewTuple(types.NewParam(0, nil, "", types.NewPointer(types.Typ[types.String]))),
		false,
	)
	findByName := types.NewFunc(0, nil, "FindByName", findByNameSig)

	// func CountUsers() int
	countSig := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(),
		types.NewTuple(types.NewParam(0, nil, "", intType)),
		false,
	)
	count := types.NewFunc(0, nil, "CountUsers", countSig)

	return []*types.Func{findByName, count}
}

func TestSyrup_TemplateExecution(t *testing.T) {
	t.Parallel()
	templates := map[string]string{
		"default": "", // Use embedded template
		"custom_comment": `{{define "imports"}}// CUSTOM HEADER COMMENT
package {{ .Name }}
{{end}}
{{define "mockBase"}}// Mock for {{ .InterfaceName }}
type {{ .InterfaceName | ToGoCamel }}Mock struct { mock.Mock }
{{end}}
{{define "combinedCall"}}// Call wrapper for {{ .MethodName }}
type {{ .InterfaceName | ToGoCamel }}{{ .MethodName }}Call struct { *mock.Call }
{{end}}
{{define "combinedMockMethod"}}// Mock method {{ .MethodName }}
func (_m *{{ .InterfaceName | ToGoCamel }}Mock) {{ .MethodName }}() { _m.Called() }
{{end}}`,
		"minimal_output": `{{define "imports"}}pkg {{ .Name }}{{end}}
{{define "mockBase"}}mock {{ .InterfaceName }}{{end}}
{{define "combinedCall"}}call {{ .MethodName }}{{end}}
{{define "combinedMockMethod"}}method {{ .MethodName }}{{end}}`,
		"variable_test": `{{define "imports"}}Variables: {{ .Name }} {{ range .Imports }}{{ . }} {{ end }}{{end}}
{{define "mockBase"}}{{ .InterfaceName }}-{{ .ConstructorPrefix }}-{{ .TypeParamsDecl }}-{{ .TypeParamsUse }}{{end}}
{{define "combinedCall"}}{{ .InterfaceName }}.{{ .MethodName }}[{{ .TypeParamsUse }}] {{ .IsVariadic }} {{ .HasReturns }}{{end}}
{{define "combinedMockMethod"}}{{ .InterfaceName }}.{{ .MethodName }}[{{ .TypeParamsUse }}] {{ .IsVariadic }} {{ len .Results }}{{end}}`,
	}

	// Test assertion functions
	tests := []struct {
		name           string
		tmpl           string
		assertCallFunc func(t *testing.T, output string)
		assertMockFunc func(t *testing.T, output string)
		expectError    bool
	}{
		{
			name: "default template",
			tmpl: templates["default"],
			assertCallFunc: func(t *testing.T, output string) {
				t.Helper()

				// Call() output should contain call-related patterns
				assert.Contains(t, output, "userRepositoryGetUserCall")
				assert.Contains(t, output, "TypedReturns")
				assert.Contains(t, output, "TypedRun")
				assert.Contains(t, output, "OnFindByName")
				assert.Contains(t, output, "OnCountUsers")
			},
			assertMockFunc: func(t *testing.T, output string) {
				t.Helper()

				// MockMethod output should contain method patterns
				assert.Contains(t, output, "func (_m *userRepositoryMock) GetUser(")
				assert.Contains(t, output, "_ context.Context")
				assert.Contains(t, output, "id string")
				assert.Contains(t, output, "active bool")
				assert.Contains(t, output, "OnGetUser")
			},
			expectError: false,
		},
		{
			name: "custom comment template",
			tmpl: templates["custom_comment"],
			assertCallFunc: func(t *testing.T, output string) {
				t.Helper()

				// Custom comment template Call should contain call wrapper
				assert.Contains(t, output, "Call wrapper for GetUser")
			},
			assertMockFunc: func(t *testing.T, output string) {
				t.Helper()

				// Custom comment template MockMethod should contain mock method
				assert.Contains(t, output, "Mock method GetUser")
			},
			expectError: false,
		},
		{
			name: "minimal output template",
			tmpl: templates["minimal_output"],
			assertCallFunc: func(t *testing.T, output string) {
				t.Helper()

				// Minimal template Call should contain call string
				assert.Contains(t, output, "call GetUser")
			},
			assertMockFunc: func(t *testing.T, output string) {
				t.Helper()

				// Minimal template MockMethod should contain method string
				assert.Contains(t, output, "method GetUser")
			},
			expectError: false,
		},
		{
			name: "variable testing template",
			tmpl: templates["variable_test"],
			assertCallFunc: func(t *testing.T, output string) {
				t.Helper()

				// Should contain interface and method names
				assert.Contains(t, output, "UserRepository")
				assert.Contains(t, output, "GetUser")
				// Should contain variadic and return info
				assert.Contains(t, output, "false")
				assert.Contains(t, output, "true")
			},
			assertMockFunc: func(t *testing.T, output string) {
				t.Helper()

				// Should contain interface and method names
				assert.Contains(t, output, "UserRepository")
				assert.Contains(t, output, "GetUser")
				// Should contain variadic and return info
				assert.Contains(t, output, "false")
				assert.Contains(t, output, "2")
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			syrup := createTestSyrup(t, tt.tmpl)
			testMethods := createSimpleTestMethods()

			// Test Call method
			t.Run("Call", func(t *testing.T) {
				t.Parallel()
				var buffer bytes.Buffer
				err := syrup.Call(&buffer, testMethods)

				if tt.expectError {
					assert.Error(t, err)
					return
				}

				require.NoError(t, err)
				output := buffer.String()
				assert.NotEmpty(t, output)
				tt.assertCallFunc(t, output)
			})

			// Test MockMethod
			t.Run("MockMethod", func(t *testing.T) {
				t.Parallel()
				var buffer bytes.Buffer
				err := syrup.MockMethod(&buffer)

				if tt.expectError {
					assert.Error(t, err)
					return
				}

				require.NoError(t, err)
				output := buffer.String()
				assert.NotEmpty(t, output)
				tt.assertMockFunc(t, output)
			})
		})
	}
}

func TestSyrup_TemplateErrorHandling(t *testing.T) {
	t.Parallel()
	errorTemplates := map[string]string{
		"syntax_error": `{{define "imports"}}{{ invalid syntax {{end}}`,
		"missing_combinedCall": `{{define "imports"}}package {{ .Name }}{{end}}
{{define "mockBase"}}mock{{end}}
{{define "combinedMockMethod"}}method{{end}}`,
		"missing_combinedMockMethod": `{{define "imports"}}package {{ .Name }}{{end}}
{{define "mockBase"}}mock{{end}}
{{define "combinedCall"}}call{{end}}`,
	}

	tests := []struct {
		name          string
		tmpl          string
		testCall      bool // test Call method
		testMock      bool // test MockMethod
		expectError   bool
		errorContains string
	}{
		{
			name:          "syntax error in template",
			tmpl:          errorTemplates["syntax_error"],
			testCall:      true,
			testMock:      true,
			expectError:   true,
			errorContains: "template",
		},
		{
			name:          "missing combinedCall template",
			tmpl:          errorTemplates["missing_combinedCall"],
			testCall:      true,
			testMock:      false,
			expectError:   true,
			errorContains: "no template",
		},
		{
			name:          "missing combinedMockMethod template",
			tmpl:          errorTemplates["missing_combinedMockMethod"],
			testCall:      false,
			testMock:      true,
			expectError:   true,
			errorContains: "no template",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Create a dummy method and signature for error testing
			stringType := types.Typ[types.String]
			params := types.NewTuple(types.NewParam(0, nil, "test", stringType))
			signature := types.NewSignatureType(nil, nil, nil, params, nil, false)
			method := types.NewFunc(0, nil, "TestMethod", signature)

			// Try to create Syrup - might fail for syntax errors
			base := template.New("templates").Funcs(template.FuncMap{
				"ToGoCamel":  strcase.ToGoCamel,
				"ToGoPascal": strcase.ToGoPascal,
			})

			tmpl, err := base.Parse(tt.tmpl)
			if err != nil {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.errorContains)
				return
			}

			syrup := New("testpkg", "TestInterface", method, signature, nil, tmpl)

			// Test Call method if requested
			if tt.testCall {
				var buffer bytes.Buffer
				err = syrup.Call(&buffer, createSimpleTestMethods())
				if tt.expectError {
					require.Error(t, err)
					require.ErrorContains(t, err, tt.errorContains)
				} else {
					require.NoError(t, err)
				}
			}

			// Test MockMethod if requested
			if tt.testMock {
				var buffer bytes.Buffer
				err = syrup.MockMethod(&buffer)
				if tt.expectError {
					require.Error(t, err)
					require.ErrorContains(t, err, tt.errorContains)
				} else {
					assert.NoError(t, err)
				}
			}
		})
	}
}
