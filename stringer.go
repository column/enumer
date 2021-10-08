// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build go1.5

//Enumer is a tool to generate Go code that adds useful methods to Go enums (constants with a specific type).
//It started as a fork of Rob Pike’s Stringer tool
//
//Please visit http://github.com/alvaroloes/enumer for a comprehensive documentation
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	exact "go/constant"
	"go/format"
	"go/importer"
	"go/token"
	"go/types"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/pascaldekloe/name"
)

type arrayFlags []string

func (af arrayFlags) String() string {
	return strings.Join(af, "")
}

func (af *arrayFlags) Set(value string) error {
	*af = append(*af, value)
	return nil
}

var (
	typeNames       = flag.String("type", "", "comma-separated list of type names; must be set")
	sql             = flag.Bool("sql", false, "if true, the Scanner and Valuer interface will be implemented.")
	json            = flag.Bool("json", false, "if true, json marshaling methods will be generated. Default: false")
	yaml            = flag.Bool("yaml", false, "if true, yaml marshaling methods will be generated. Default: false")
	text            = flag.Bool("text", false, "if true, text marshaling methods will be generated. Default: false")
	output          = flag.String("output", "", "output file name; default srcdir/<type>_enumer.go")
	transformMethod = flag.String("transform", "noop", "enum item name transformation method. Default: noop")
	trimPrefix      = flag.String("trimprefix", "", "transform each item name by removing a prefix. Default: \"\"")
	lineComment     = flag.Bool("linecomment", false, "use line comment text as printed text when present")
	ts              = flag.String("ts", "", "output file name of TypeScript codes; must be set")
	nogo            = flag.Bool("nogo", false, "if true, no Go codes will be generated")
)

var comments arrayFlags

func init() {
	flag.Var(&comments, "comment", "comments to include in generated code, can repeat. Default: \"\"")
}

// Usage is a replacement usage function for the flags package.
func Usage() {
	fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "\tenumer [flags] -type T [directory]\n")
	fmt.Fprintf(os.Stderr, "\tenumer [flags] -type T files... # Must be a single package\n")
	fmt.Fprintf(os.Stderr, "For more information, see:\n")
	fmt.Fprintf(os.Stderr, "\thttps://github.com/column/enumer\n")
	fmt.Fprintf(os.Stderr, "Flags:\n")
	flag.PrintDefaults()
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("enumer: ")
	flag.Usage = Usage
	flag.Parse()
	if len(*typeNames) == 0 {
		flag.Usage()
		os.Exit(2)
	}
	types := strings.Split(*typeNames, ",")

	// We accept either one directory or a list of files. Which do we have?
	args := flag.Args()
	if len(args) == 0 {
		// Default: process whole package in current directory.
		args = []string{"."}
	}

	// Parse the package once.
	var (
		dir string
		g   Generator
	)

	if len(args) == 1 && isDirectory(args[0]) {
		dir = args[0]
	} else {
		dir = filepath.Dir(args[0])
	}

	g.parsePackage(args)

	// Print the header and package clause.
	g.Printf("// Code generated by \"enumer %s\"; DO NOT EDIT.\n", strings.Join(os.Args[1:], " "))
	g.Printf("\n")
	g.Printf("// %s\n", comments.String())
	g.Printf("package %s", g.pkg.name)
	g.Printf("\n")
	g.Printf("import (\n")
	g.Printf("\t\"fmt\"\n")
	if *sql {
		g.Printf("\t\"database/sql/driver\"\n")
	}
	if *json {
		g.Printf("\t\"encoding/json\"\n")
	}
	g.Printf(")\n")

	fmt.Fprintf(&g.tsBuf, "/* Code generated by \"enumer %s\"; DO NOT EDIT. */\n\n", strings.Join(os.Args[1:], " "))

	// Run generate for each type.
	for _, typeName := range types {
		g.generate(typeName, *json, *yaml, *sql, *text, *transformMethod, *trimPrefix, *lineComment)
	}

	if !*nogo {
		// Format the output.
		src := g.format()

		// Figure out filename to write to
		outputName := *output
		if outputName == "" {
			baseName := fmt.Sprintf("%s_enumer.go", types[0])
			outputName = filepath.Join(dir, strings.ToLower(baseName))
		}
		g.dumpData(src, types[0], outputName)
	}

	if *ts != "" {
		folder := filepath.Dir(*ts)
		if e := os.MkdirAll(folder, os.ModePerm); e != nil {
			log.Fatalf("failed to create folder: %s", folder)
		}
		g.dumpData(g.tsBuf.Bytes(), types[0], *ts)
	}
}

func (g *Generator) dumpData(src []byte, typeName string, output string) {
	// Write to tmpfile first
	tmpName := fmt.Sprintf("%s_enumer_", filepath.Base(typeName))
	tmpFile, err := ioutil.TempFile(filepath.Dir(typeName), tmpName)
	if err != nil {
		log.Fatalf("creating temporary file for output: %s", err)
	}
	_, err = tmpFile.Write(src)
	if err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		log.Fatalf("writing output: %s", err)
	}
	tmpFile.Close()

	// Rename tmpfile to output file
	err = os.Rename(tmpFile.Name(), output)
	if err != nil {
		log.Fatalf("moving tempfile to output file: %s", err)
	}
}

// isDirectory reports whether the named file is a directory.
func isDirectory(name string) bool {
	info, err := os.Stat(name)
	if err != nil {
		log.Fatal(err)
	}
	return info.IsDir()
}

// Generator holds the state of the analysis. Primarily used to buffer
// the output for format.Source.
type Generator struct {
	buf   bytes.Buffer // Accumulated output.
	pkg   *Package     // Package we are scanning.
	tsBuf bytes.Buffer // Accumulated output of TypeScript codes.
}

// Printf prints the string to the output
func (g *Generator) Printf(format string, args ...interface{}) {
	fmt.Fprintf(&g.buf, format, args...)
}

// File holds a single parsed file and associated data.
type File struct {
	pkg  *Package  // Package to which this file belongs.
	file *ast.File // Parsed AST.
	// These fields are reset for each type being generated.
	typeName string  // Name of the constant type.
	values   []Value // Accumulator for constant values of that type.
}

// Package holds information about a Go package
type Package struct {
	dir      string
	name     string
	defs     map[*ast.Ident]types.Object
	files    []*File
	typesPkg *types.Package
}

//// parsePackageDir parses the package residing in the directory.
//func (g *Generator) parsePackageDir(directory string) {
//	pkg, err := build.Default.ImportDir(directory, 0)
//	if err != nil {
//		log.Fatalf("cannot process directory %s: %s", directory, err)
//	}
//	var names []string
//	names = append(names, pkg.GoFiles...)
//	names = append(names, pkg.CgoFiles...)
//	// TODO: Need to think about constants in test files. Maybe write type_string_test.go
//	// in a separate pass? For later.
//	// names = append(names, pkg.TestGoFiles...) // These are also in the "foo" package.
//	names = append(names, pkg.SFiles...)
//	names = prefixDirectory(directory, names)
//	g.parsePackage(directory, names, nil)
//}
//
//// parsePackageFiles parses the package occupying the named files.
//func (g *Generator) parsePackageFiles(names []string) {
//	g.parsePackage(".", names, nil)
//}
//
//// prefixDirectory places the directory name on the beginning of each name in the list.
//func prefixDirectory(directory string, names []string) []string {
//	if directory == "." {
//		return names
//	}
//	ret := make([]string, len(names))
//	for i, name := range names {
//		ret[i] = filepath.Join(directory, name)
//	}
//	return ret
//}

//// parsePackage analyzes the single package constructed from the named files.
//// If text is non-nil, it is a string to be used instead of the content of the file,
//// to be used for testing. parsePackage exits if there is an error.
//func (g *Generator) parsePackage(directory string, names []string, text interface{}) {
//	var files []*File
//	var astFiles []*ast.File
//	g.pkg = new(Package)
//	fs := token.NewFileSet()
//	for _, name := range names {
//		if !strings.HasSuffix(name, ".go") {
//			continue
//		}
//		parsedFile, err := parser.ParseFile(fs, name, text, 0)
//		if err != nil {
//			log.Fatalf("parsing package: %s: %s", name, err)
//		}
//		astFiles = append(astFiles, parsedFile)
//		files = append(files, &File{
//			file: parsedFile,
//			pkg:  g.pkg,
//		})
//	}
//	if len(astFiles) == 0 {
//		log.Fatalf("%s: no buildable Go files", directory)
//	}
//	g.pkg.name = astFiles[0].Name.Name
//	g.pkg.files = files
//	g.pkg.dir = directory
//	// Type check the package.
//	g.pkg.check(fs, astFiles)
//}

// parsePackage analyzes the single package constructed from the patterns and tags.
// parsePackage exits if there is an error.
func (g *Generator) parsePackage(patterns []string) {
	cfg := &packages.Config{
		Mode: packages.LoadSyntax,
		// TODO: Need to think about constants in test files. Maybe write type_string_test.go
		// in a separate pass? For later.
		Tests: false,
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		log.Fatal(err)
	}
	if len(pkgs) != 1 {
		log.Fatalf("error: %d packages found", len(pkgs))
	}
	g.addPackage(pkgs[0])
}

// addPackage adds a type checked Package and its syntax files to the generator.
func (g *Generator) addPackage(pkg *packages.Package) {
	g.pkg = &Package{
		name:  pkg.Name,
		defs:  pkg.TypesInfo.Defs,
		files: make([]*File, len(pkg.Syntax)),
	}

	for i, file := range pkg.Syntax {
		g.pkg.files[i] = &File{
			file: file,
			pkg:  g.pkg,
		}
	}
}

// check type-checks the package. The package must be OK to proceed.
func (pkg *Package) check(fs *token.FileSet, astFiles []*ast.File) {
	pkg.defs = make(map[*ast.Ident]types.Object)
	config := types.Config{Importer: importer.Default(), FakeImportC: true}
	info := &types.Info{
		Defs: pkg.defs,
	}
	typesPkg, err := config.Check(pkg.dir, fs, astFiles, info)
	if err != nil {
		log.Fatalf("checking package: %s", err)
	}
	pkg.typesPkg = typesPkg
}

func (g *Generator) transformValueNames(values []Value, transformMethod string) {
	var sep rune
	switch transformMethod {
	case "snake":
		sep = '_'
	case "kebab":
		sep = '-'
	default:
		return
	}

	for i := range values {
		values[i].name = strings.ToLower(name.Delimit(values[i].name, sep))
	}
}

// trimValueNames removes a prefix from each name
func (g *Generator) trimValueNames(values []Value, prefix string) {
	for i := range values {
		values[i].name = strings.TrimPrefix(values[i].name, prefix)
	}
}

func (g *Generator) replaceValuesWithLineComment(values []Value) {
	for i, val := range values {
		if val.comment != "" {
			values[i].name = val.comment
		}
	}
}

// generate produces the String method for the named type.
func (g *Generator) generate(
	typeName string,
	includeJSON,
	includeYAML,
	includeSQL,
	includeText bool,
	transformMethod string,
	trimPrefix string,
	lineComment bool,
) {
	values := make([]Value, 0, 100)
	for _, file := range g.pkg.files {
		// Set the state for this run of the walker.
		file.typeName = typeName
		file.values = nil
		if file.file != nil {
			ast.Inspect(file.file, file.genDecl)
			values = append(values, file.values...)
		}
	}

	if len(values) == 0 {
		log.Fatalf("no values defined for type %s", typeName)
	}

	g.trimValueNames(values, trimPrefix)

	g.transformValueNames(values, transformMethod)

	g.genTypeScriptCodes(typeName, values, lineComment)

	if lineComment {
		g.replaceValuesWithLineComment(values)
	}

	runs := splitIntoRuns(values)
	// The decision of which pattern to use depends on the number of
	// runs in the numbers. If there's only one, it's easy. For more than
	// one, there's a tradeoff between complexity and size of the data
	// and code vs. the simplicity of a map. A map takes more space,
	// but so does the code. The decision here (crossover at 10) is
	// arbitrary, but considers that for large numbers of runs the cost
	// of the linear scan in the switch might become important, and
	// rather than use yet another algorithm such as binary search,
	// we punt and use a map. In any case, the likelihood of a map
	// being necessary for any realistic example other than bitmasks
	// is very low. And bitmasks probably deserve their own analysis,
	// to be done some other day.
	const runsThreshold = 10
	switch {
	case len(runs) == 1:
		g.buildOneRun(runs, typeName)
	case len(runs) <= runsThreshold:
		g.buildMultipleRuns(runs, typeName)
	default:
		g.buildMap(runs, typeName)
	}

	g.buildBasicExtras(runs, typeName, runsThreshold)
	if includeJSON {
		g.buildJSONMethods(runs, typeName, runsThreshold)
	}
	if includeText {
		g.buildTextMethods(runs, typeName, runsThreshold)
	}
	if includeYAML {
		g.buildYAMLMethods(runs, typeName, runsThreshold)
	}
	if includeSQL {
		g.addValueAndScanMethod(typeName)
	}
}

// splitIntoRuns breaks the values into runs of contiguous sequences.
// For example, given 1,2,3,5,6,7 it returns {1,2,3},{5,6,7}.
// The input slice is known to be non-empty.
func splitIntoRuns(values []Value) [][]Value {
	// We use stable sort so the lexically first name is chosen for equal elements.
	sort.Stable(byValue(values))
	// Remove duplicates. Stable sort has put the one we want to print first,
	// so use that one. The String method won't care about which named constant
	// was the argument, so the first name for the given value is the only one to keep.
	// We need to do this because identical values would cause the switch or map
	// to fail to compile.
	j := 1
	for i := 1; i < len(values); i++ {
		if values[i].value != values[i-1].value {
			values[j] = values[i]
			j++
		}
	}
	values = values[:j]
	runs := make([][]Value, 0, 10)
	for len(values) > 0 {
		// One contiguous sequence per outer loop.
		i := 1
		for i < len(values) && values[i].value == values[i-1].value+1 {
			i++
		}
		runs = append(runs, values[:i])
		values = values[i:]
	}
	return runs
}

// format returns the gofmt-ed contents of the Generator's buffer.
func (g *Generator) format() []byte {
	src, err := format.Source(g.buf.Bytes())
	if err != nil {
		// Should never happen, but can arise when developing this code.
		// The user can compile the output to see the error.
		log.Printf("warning: internal error: invalid Go generated: %s", err)
		log.Printf("warning: compile the package to analyze the error")
		return g.buf.Bytes()
	}
	return src
}

// Value represents a declared constant.
type Value struct {
	name string // The name of the constant after transformation (i.e. camel case => snake case)
	// The value is stored as a bit pattern alone. The boolean tells us
	// whether to interpret it as an int64 or a uint64; the only place
	// this matters is when sorting.
	// Much of the time the str field is all we need; it is printed
	// by Value.String.
	value   uint64 // Will be converted to int64 when needed.
	signed  bool   // Whether the constant is a signed type.
	str     string // The string representation given by the "go/exact" package.
	comment string // The comment on the right of the constant
}

func (v *Value) String() string {
	return v.str
}

// byValue lets us sort the constants into increasing order.
// We take care in the Less method to sort in signed or unsigned order,
// as appropriate.
type byValue []Value

func (b byValue) Len() int      { return len(b) }
func (b byValue) Swap(i, j int) { b[i], b[j] = b[j], b[i] }
func (b byValue) Less(i, j int) bool {
	if b[i].signed {
		return int64(b[i].value) < int64(b[j].value)
	}
	return b[i].value < b[j].value
}

// genDecl processes one declaration clause.
func (f *File) genDecl(node ast.Node) bool {
	decl, ok := node.(*ast.GenDecl)
	if !ok || decl.Tok != token.CONST {
		// We only care about const declarations.
		return true
	}
	// The name of the type of the constants we are declaring.
	// Can change if this is a multi-element declaration.
	typ := ""
	// Loop over the elements of the declaration. Each element is a ValueSpec:
	// a list of names possibly followed by a type, possibly followed by values.
	// If the type and value are both missing, we carry down the type (and value,
	// but the "go/types" package takes care of that).
	for _, spec := range decl.Specs {
		vspec := spec.(*ast.ValueSpec) // Guaranteed to succeed as this is CONST.
		if vspec.Type == nil && len(vspec.Values) > 0 {
			// "X = 1". With no type but a value, the constant is untyped.
			// Skip this vspec and reset the remembered type.
			typ = ""
			continue
		}
		if vspec.Type != nil {
			// "X T". We have a type. Remember it.
			ident, ok := vspec.Type.(*ast.Ident)
			if !ok {
				continue
			}
			typ = ident.Name
		}
		if typ != f.typeName {
			// This is not the type we're looking for.
			continue
		}
		// We now have a list of names (from one line of source code) all being
		// declared with the desired type.
		// Grab their names and actual values and store them in f.values.
		for _, name := range vspec.Names {
			if name.Name == "_" {
				continue
			}
			// This dance lets the type checker find the values for us. It's a
			// bit tricky: look up the object declared by the name, find its
			// types.Const, and extract its value.
			obj, ok := f.pkg.defs[name]
			if !ok {
				log.Fatalf("no value for constant %s", name)
			}
			info := obj.Type().Underlying().(*types.Basic).Info()
			if info&types.IsInteger == 0 {
				log.Fatalf("can't handle non-integer constant type %s", typ)
			}
			value := obj.(*types.Const).Val() // Guaranteed to succeed as this is CONST.
			if value.Kind() != exact.Int {
				log.Fatalf("can't happen: constant is not an integer %s", name)
			}
			i64, isInt := exact.Int64Val(value)
			u64, isUint := exact.Uint64Val(value)
			if !isInt && !isUint {
				log.Fatalf("internal error: value of %s is not an integer: %s", name, value.String())
			}
			if !isInt {
				u64 = uint64(i64)
			}
			comment := ""
			if c := vspec.Comment; c != nil && len(c.List) == 1 {
				comment = strings.TrimSpace(c.Text())
			}

			v := Value{
				name:    name.Name,
				value:   u64,
				signed:  info&types.IsUnsigned == 0,
				str:     value.String(),
				comment: comment,
			}
			f.values = append(f.values, v)
		}
	}
	return false
}

// Helpers

// usize returns the number of bits of the smallest unsigned integer
// type that will hold n. Used to create the smallest possible slice of
// integers to use as indexes into the concatenated strings.
func usize(n int) int {
	switch {
	case n < 1<<8:
		return 8
	case n < 1<<16:
		return 16
	default:
		// 2^32 is enough constants for anyone.
		return 32
	}
}

// declareIndexAndNameVars declares the index slices and concatenated names
// strings representing the runs of values.
func (g *Generator) declareIndexAndNameVars(runs [][]Value, typeName string) {
	var indexes, names []string
	for i, run := range runs {
		index, name := g.createIndexAndNameDecl(run, typeName, fmt.Sprintf("_%d", i))
		indexes = append(indexes, index)
		names = append(names, name)
	}
	g.Printf("const (\n")
	for _, name := range names {
		g.Printf("\t%s\n", name)
	}
	g.Printf(")\n\n")
	g.Printf("var (")
	for _, index := range indexes {
		g.Printf("\t%s\n", index)
	}
	g.Printf(")\n\n")
}

// declareIndexAndNameVar is the single-run version of declareIndexAndNameVars
func (g *Generator) declareIndexAndNameVar(run []Value, typeName string) {
	index, name := g.createIndexAndNameDecl(run, typeName, "")
	g.Printf("const %s\n", name)
	g.Printf("var %s\n", index)
}

// createIndexAndNameDecl returns the pair of declarations for the run. The caller will add "const" and "var".
func (g *Generator) createIndexAndNameDecl(run []Value, typeName string, suffix string) (string, string) {
	b := new(bytes.Buffer)
	indexes := make([]int, len(run))
	for i := range run {
		b.WriteString(run[i].name)
		indexes[i] = b.Len()
	}
	nameConst := fmt.Sprintf("_%sName%s = %q", typeName, suffix, b.String())
	nameLen := b.Len()
	b.Reset()
	fmt.Fprintf(b, "_%sIndex%s = [...]uint%d{0, ", typeName, suffix, usize(nameLen))
	for i, v := range indexes {
		if i > 0 {
			fmt.Fprintf(b, ", ")
		}
		fmt.Fprintf(b, "%d", v)
	}
	fmt.Fprintf(b, "}")
	return b.String(), nameConst
}

// declareNameVars declares the concatenated names string representing all the values in the runs.
func (g *Generator) declareNameVars(runs [][]Value, typeName string, suffix string) {
	g.Printf("const _%sName%s = \"", typeName, suffix)
	for _, run := range runs {
		for i := range run {
			g.Printf("%s", run[i].name)
		}
	}
	g.Printf("\"\n")
}

// buildOneRun generates the variables and String method for a single run of contiguous values.
func (g *Generator) buildOneRun(runs [][]Value, typeName string) {
	values := runs[0]
	g.Printf("\n")
	g.declareIndexAndNameVar(values, typeName)
	// The generated code is simple enough to write as a Printf format.
	lessThanZero := ""
	if values[0].signed {
		lessThanZero = "i < 0 || "
	}
	if values[0].value == 0 { // Signed or unsigned, 0 is still 0.
		g.Printf(stringOneRun, typeName, usize(len(values)), lessThanZero)
	} else {
		g.Printf(stringOneRunWithOffset, typeName, values[0].String(), usize(len(values)), lessThanZero)
	}
}

// Arguments to format are:
//	[1]: type name
//	[2]: size of index element (8 for uint8 etc.)
//	[3]: less than zero check (for signed types)
const stringOneRun = `func (i %[1]s) String() string {
	if %[3]si >= %[1]s(len(_%[1]sIndex)-1) {
		return fmt.Sprintf("%[1]s(%%d)", i)
	}
	return _%[1]sName[_%[1]sIndex[i]:_%[1]sIndex[i+1]]
}
`

// Arguments to format are:
//	[1]: type name
//	[2]: lowest defined value for type, as a string
//	[3]: size of index element (8 for uint8 etc.)
//	[4]: less than zero check (for signed types)
/*
 */
const stringOneRunWithOffset = `func (i %[1]s) String() string {
	i -= %[2]s
	if %[4]si >= %[1]s(len(_%[1]sIndex)-1) {
		return fmt.Sprintf("%[1]s(%%d)", i + %[2]s)
	}
	return _%[1]sName[_%[1]sIndex[i] : _%[1]sIndex[i+1]]
}
`

// buildMultipleRuns generates the variables and String method for multiple runs of contiguous values.
// For this pattern, a single Printf format won't do.
func (g *Generator) buildMultipleRuns(runs [][]Value, typeName string) {
	g.Printf("\n")
	g.declareIndexAndNameVars(runs, typeName)
	g.Printf("func (i %s) String() string {\n", typeName)
	g.Printf("\tswitch {\n")
	for i, values := range runs {
		if len(values) == 1 {
			g.Printf("\tcase i == %s:\n", &values[0])
			g.Printf("\t\treturn _%sName_%d\n", typeName, i)
			continue
		}
		g.Printf("\tcase %s <= i && i <= %s:\n", &values[0], &values[len(values)-1])
		if values[0].value != 0 {
			g.Printf("\t\ti -= %s\n", &values[0])
		}
		g.Printf("\t\treturn _%sName_%d[_%sIndex_%d[i]:_%sIndex_%d[i+1]]\n",
			typeName, i, typeName, i, typeName, i)
	}
	g.Printf("\tdefault:\n")
	g.Printf("\t\treturn fmt.Sprintf(\"%s(%%d)\", i)\n", typeName)
	g.Printf("\t}\n")
	g.Printf("}\n")
}

// buildMap handles the case where the space is so sparse a map is a reasonable fallback.
// It's a rare situation but has simple code.
func (g *Generator) buildMap(runs [][]Value, typeName string) {
	g.Printf("\n")
	g.declareNameVars(runs, typeName, "")
	g.Printf("\nvar _%sMap = map[%s]string{\n", typeName, typeName)
	n := 0
	for _, values := range runs {
		for _, value := range values {
			g.Printf("\t%s: _%sName[%d:%d],\n", &value, typeName, n, n+len(value.name))
			n += len(value.name)
		}
	}
	g.Printf("}\n\n")
	g.Printf(stringMap, typeName)
}

// Argument to format is the type name.
const stringMap = `func (i %[1]s) String() string {
	if str, ok := _%[1]sMap[i]; ok {
		return str
	}
	return fmt.Sprintf("%[1]s(%%d)", i)
}
`
