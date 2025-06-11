package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type TestInfo struct {
	Name     string
	File     string
	Line     int
	Subtests []string
}

func main() {
	var (
		showSubtests = flag.Bool("subtests", true, "Show individual subtests")
		showParent   = flag.Bool("parent", true, "Show parent test patterns")
		useFzf       = flag.Bool("fzf", false, "Use fzf for interactive test selection and execution")
		tags         = flag.String("tags", "", "Build tags to pass to go test")
	)
	flag.Parse()

	if len(flag.Args()) < 1 {
		fmt.Println("Usage: testtool [flags] <directory>")
		flag.PrintDefaults()
		os.Exit(1)
	}

	dir := flag.Args()[0]
	tests := findTests(dir)

	if *useFzf {
		runWithFzf(tests, *tags)
		return
	}

	for _, test := range tests {
		if len(test.Subtests) == 0 {
			fmt.Printf("^%s$\n", test.Name)
			continue
		}

		if *showParent {
			fmt.Printf("^%s$\n", test.Name)
		}
		if *showSubtests {
			for _, subtest := range test.Subtests {
				fmt.Printf("^%s/%s$\n", test.Name, subtest)
			}
		}
	}
}

func findTests(dir string) []TestInfo {
	var tests []TestInfo

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fileTests := parseTestFile(path)
		tests = append(tests, fileTests...)
		return nil
	})

	if err != nil {
		log.Fatal(err)
	}

	return tests
}

func parseTestFile(filename string) []TestInfo {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		log.Printf("Error parsing %s: %v", filename, err)
		return nil
	}

	var tests []TestInfo

	ast.Inspect(node, func(n ast.Node) bool {
		x, ok := n.(*ast.FuncDecl)
		if !ok {
			return true
		}

		if !isTestFunction(x) {
			return true
		}

		pos := fset.Position(x.Pos())
		test := TestInfo{
			Name: x.Name.Name,
			File: filename,
			Line: pos.Line,
		}

		test.Subtests = findSubtests(x)
		tests = append(tests, test)
		return true
	})

	return tests
}

func isTestFunction(fn *ast.FuncDecl) bool {
	if !strings.HasPrefix(fn.Name.Name, "Test") {
		return false
	}

	if fn.Type.Params == nil || len(fn.Type.Params.List) == 0 {
		return false
	}

	firstParam := fn.Type.Params.List[0]
	starExpr, ok := firstParam.Type.(*ast.StarExpr)
	if !ok {
		return false
	}

	selExpr, ok := starExpr.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	ident, ok := selExpr.X.(*ast.Ident)
	if !ok {
		return false
	}

	return ident.Name == "testing" &&
		(selExpr.Sel.Name == "T" ||
			selExpr.Sel.Name == "TB" ||
			selExpr.Sel.Name == "B")
}

func findSubtests(fn *ast.FuncDecl) []string {
	var subtests []string

	ast.Inspect(fn, func(n ast.Node) bool {
		callExpr, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		selExpr, ok := callExpr.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		if selExpr.Sel.Name != "Run" {
			return true
		}

		if len(callExpr.Args) < 1 {
			return true
		}

		basicLit, ok := callExpr.Args[0].(*ast.BasicLit)
		if !ok {
			return true
		}

		name := strings.Trim(basicLit.Value, `"`)
		subtests = append(subtests, name)
		return true
	})

	return subtests
}

func runWithFzf(tests []TestInfo, tags string) {
	testPatterns := collectTestPatterns(tests)

	if len(testPatterns) == 0 {
		fmt.Println("No tests found")
		return
	}

	selectedTests, err := fzfSelect(testPatterns)
	if err != nil {
		log.Printf("Error running fzf: %v", err)
		return
	}

	if len(selectedTests) == 0 {
		fmt.Println("No tests selected")
		return
	}

	runPattern := buildRunPattern(selectedTests)
	executeGoTest(runPattern, tags)
}

func collectTestPatterns(tests []TestInfo) []string {
	var patterns []string

	for _, test := range tests {
		if len(test.Subtests) == 0 {
			patterns = append(patterns, test.Name)
			continue
		}

		patterns = append(patterns, test.Name)
		for _, subtest := range test.Subtests {
			patterns = append(patterns, test.Name+"/"+subtest)
		}
	}

	return patterns
}

func fzfSelect(options []string) ([]string, error) {
	cmd := exec.Command("fzf", "--multi", "--prompt=Select tests: ")
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	go func() {
		defer stdin.Close()
		for _, option := range options {
			fmt.Fprintln(stdin, option)
		}
	}()

	var selected []string
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		selected = append(selected, scanner.Text())
	}

	if err := cmd.Wait(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			if exitError.ExitCode() == 130 {
				return nil, nil
			}
		}
		return nil, err
	}

	return selected, scanner.Err()
}

func buildRunPattern(selectedTests []string) string {
	if len(selectedTests) == 0 {
		return ""
	}

	if len(selectedTests) == 1 {
		return "^" + selectedTests[0] + "$"
	}

	var patterns []string
	for _, test := range selectedTests {
		patterns = append(patterns, "^"+test+"$")
	}

	return "(" + strings.Join(patterns, "|") + ")"
}

func executeGoTest(runPattern, tags string) {
	args := []string{"test", "-count=1"}

	if tags != "" {
		args = append(args, "-tags="+tags)
	}

	if runPattern != "" {
		args = append(args, "-run="+runPattern)
	}

	args = append(args, "./...")

	fmt.Printf("Running: go %s\n", strings.Join(args, " "))

	cmd := exec.Command("go", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			os.Exit(exitError.ExitCode())
		}
		log.Fatal(err)
	}
}
