package main

import (
	"bufio"
	"bytes"
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/olekukonko/tablewriter"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

func TestMain(m *testing.M) {
	r, w, err := os.Pipe()
	if err != nil {
		os.Exit(m.Run())
	}
	stdoutOrig := os.Stdout
	buf := bytes.NewBuffer(nil)
	tee := io.MultiWriter(os.Stdout, buf)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		io.Copy(tee, r)
		wg.Done()
	}()
	os.Stdout = w
	code := m.Run()
	os.Stdout = stdoutOrig
	w.Close()
	wg.Wait()
	fmt.Printf("\n%s\n", parseToTable(buf))
	os.Exit(code)
}

func parseToTable(buf *bytes.Buffer) string {
	scnr := bufio.NewScanner(buf)

	scnr.Scan() // drop goos
	scnr.Scan() // drop goarch
	scnr.Scan() // drop pkg
	scnr.Scan() // drop cpu

	testTimes := map[string]map[string]int{}
	for scnr.Scan() {
		line := strings.TrimRight(scnr.Text(), "\n")

		if !strings.HasPrefix(line, "Benchmark") {
			continue
		}

		cols := strings.Fields(line)
		if len(cols) < 3 {
			continue
		}

		nameParts := strings.SplitN(cols[0], "/", 2)
		testName := strings.TrimPrefix(nameParts[0], "Benchmark")
		testName = strings.ReplaceAll(testName, "_", " ")
		pkgName := strings.SplitN(nameParts[1], "-", 2)[0]

		if _, ok := testTimes[testName]; !ok {
			testTimes[testName] = map[string]int{}
		}

		var nsop float64
		for i := 2; i < len(cols); i += 2 {
			switch cols[i+1] {
			case "ns/op":
				nsop, _ = strconv.ParseFloat(cols[i], 64)
			case "batch_size":
				batch_size, _ := strconv.Atoi(cols[i])
				nsop /= float64(batch_size)
			}
		}

		testTimes[testName][pkgName], _ = strconv.Atoi(cols[2])
	}

	pkgNames := []string{}
	for _, times := range testTimes {
		for k, _ := range times {
			pkgNames = append(pkgNames, k)
		}
		break
	}
	sort.Strings(pkgNames)

	opsPerBatch := 10000
	tblBuf := bytes.NewBuffer(nil)
	tbl := tablewriter.NewWriter(tblBuf)
	tbl.SetBorders(tablewriter.Border{Left: true, Top: false, Right: true, Bottom: false})
	tbl.SetCenterSeparator("|")
	tbl.SetAutoFormatHeaders(false)
	tbl.SetHeader(append([]string{"*(OPs/Sec)*"}, pkgNames...))
	var testNames []string
	for testName, _ := range testTimes {
		testNames = append(testNames, testName)
	}
	sort.Strings(testNames)
	p := message.NewPrinter(language.English)
	for _, testName := range testNames {
		times := testTimes[testName]
		row := []string{testName}
		minNsOp := 0
		for _, nsop := range times {
			if minNsOp == 0 || nsop < minNsOp {
				minNsOp = nsop
			}
		}
		opPerSecMax := float64(1e9) / float64(minNsOp) * float64(opsPerBatch)
		for _, pkgName := range pkgNames {
			if _, ok := times[pkgName]; !ok {
				row = append(row, "N/A")
				continue
			}
			nsop := times[pkgName]
			opPerSec := float64(1e9) / float64(nsop) * float64(opsPerBatch)
			diff := opPerSec / opPerSecMax * 100
			row = append(row, p.Sprintf("%.0f (%.1f%%)", opPerSec, diff))
		}
		tbl.Append(row)
	}
	tbl.Render()
	return tblBuf.String()
}

func getFuncDesc(fName string) string {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "benchmark_test.go", nil, parser.ParseComments)
	if err != nil {
		panic(err)
	}

	pkg := &ast.Package{
		Name:  "benchmark",
		Files: map[string]*ast.File{"benchmark_test.go": node},
	}
	pkgDoc := doc.New(pkg, "", doc.AllDecls)
	for _, f := range pkgDoc.Funcs {
		if f.Name == fName {
			return f.Doc
		}
	}

	panic(fmt.Sprintf("%s not found", fName))
}
