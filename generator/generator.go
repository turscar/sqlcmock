package generator

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"github.com/huandu/xstrings"
	"github.com/masterminds/sprig"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"golang.org/x/mod/modfile"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

//go:embed default.tpl
var defaultTpl string

var ErrNoQuerier = errors.New("no Querier found")

type Opts struct {
	InputFile     string
	TemplateFile  string
	OutputFile    string
	OutputPackage string
	Format        bool
}

type Output struct {
	GenPackage string
	ModelPath  string
	Package    string
	Struct     string
	Imports    []Import
	Methods    []Method
}

type Field struct {
	Name string
	Type string
}

type Method struct {
	Name   string
	Input  []Field
	Output []Field
}

type Import struct {
	Path string
	Name string
}

func Parse(opts Opts) (Output, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, opts.InputFile, nil, parser.SkipObjectResolution)
	if err != nil {
		log.Fatal(err)
	}

	var querier *ast.TypeSpec

	ast.Inspect(file, func(n ast.Node) bool {
		tp, ok := n.(*ast.TypeSpec)
		if ok && tp.Name.Name == "Querier" {
			querier = tp
			return false
		}
		return true
	})

	if querier == nil {
		return Output{}, ErrNoQuerier
	}

	var methods []Method
	pkg := file.Name.Name
	var funcName string
	ast.Inspect(querier, func(n ast.Node) bool {
		ident, ok := n.(*ast.Ident)
		if ok {
			funcName = ident.Name
		}
		fn, ok := n.(*ast.FuncType)
		if ok {
			methods = append(methods, Method{
				Name:   funcName,
				Input:  fields(fn.Params, pkg),
				Output: fields(fn.Results, pkg),
			})
		}
		return true
	})

	imports := []Import{}
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return Output{}, err
		}
		var name string
		if imp.Name != nil {
			name = imp.Name.Name
		}
		imports = append(imports, Import{
			Path: path,
			Name: name,
		})
	}

	output := Output{
		Package: pkg,
		Imports: imports,
		Methods: methods,
		Struct:  "Mocker",
	}
	output.setOutputPackage(opts)
	err = output.findOutputModule(opts)
	if err != nil {
		return Output{}, err
	}
	return output, nil
}

var needsPackageRe = regexp.MustCompile(`^([^A-Za-z0-9]*)([A-Z][^.]*)$`)

func fields(list *ast.FieldList, pkg string) []Field {
	ret := []Field{}
	for _, field := range list.List {
		var name string
		if len(field.Names) > 0 && field.Names[0] != nil {
			name = field.Names[0].Name
		}
		tp := exprToString(field.Type)
		matches := needsPackageRe.FindStringSubmatch(tp)
		if matches != nil {
			tp = matches[1] + pkg + "." + matches[2]
		}

		ret = append(ret, Field{
			Name: name,
			Type: tp,
		})
	}
	return ret
}

// This is not general purpose; it's just for our use case.
func exprToString(expr ast.Expr) string {
	switch v := expr.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return v.X.(*ast.Ident).Name + "." + v.Sel.Name
	case *ast.ArrayType:
		return "[]" + exprToString(v.Elt)
	case *ast.StarExpr:
		return "*" + exprToString(v.X)
	default:
		log.Printf("unhandled type %T\n", expr)
		return fmt.Sprintf("<error_unhandled_%T>", expr)
	}
}

func (output *Output) findOutputModule(opts Opts) error {
	abs, err := filepath.Abs(opts.InputFile)
	if err != nil {
		return err
	}
	dir := filepath.Dir(abs)
	root := findModuleRoot(dir)
	modFile := filepath.Join(root, "go.mod")
	modContent, err := os.ReadFile(modFile)
	if err != nil {
		return err
	}
	mod, err := modfile.Parse(modFile, modContent, nil)
	if err != nil {
		return err
	}
	output.ModelPath = mod.Module.Mod.Path + strings.TrimPrefix(dir, root)
	return nil
}

func findModuleRoot(dir string) string {
	for {
		if fi, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !fi.IsDir() {
			return dir
		}
		d := filepath.Dir(dir)
		if d == dir {
			break
		}
		dir = d
	}
	return ""
}

func (output *Output) setOutputPackage(opts Opts) {
	if opts.OutputPackage != "" {
		output.GenPackage = opts.OutputPackage
		return
	}
	//pkg := os.Getenv("GOPACKAGE")
	//if pkg != "" {
	//	output.GenPackage = pkg
	//	return
	//}
	outPath, err := filepath.Abs(opts.OutputFile)
	if err != nil {
		log.Fatal(err)
	}
	output.GenPackage = filepath.Base(filepath.Dir(outPath))
}

func (output *Output) Render(opts Opts) ([]byte, error) {
	templateContent := defaultTpl
	if opts.TemplateFile != "" {
		content, err := os.ReadFile(opts.TemplateFile)
		if err != nil {
			return nil, fmt.Errorf("failed to open template file: %w", err)
		}
		templateContent = string(content)
	}

	var buff bytes.Buffer
	funcs := sprig.HermeticTxtFuncMap()
	funcs["lcfirst"] = xstrings.FirstRuneToLower
	funcs["ucfirst"] = xstrings.FirstRuneToUpper
	tpl, err := template.New("mock").Funcs(funcs).Parse(templateContent)
	if err != nil {
		log.Fatal(err)
	}
	err = tpl.Execute(&buff, output)
	if err != nil {
		log.Fatal(err)
	}

	if !opts.Format {
		return buff.Bytes(), nil
	}

	result, err := format.Source(buff.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to format source: %w", err)
	}

	return result, nil
}
