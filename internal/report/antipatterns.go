package report

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	gitpkg "github.com/goscope/internal/git"
	"github.com/goscope/internal/parser"
)

const apMaxViolations = 10

// priority values — used for sorting and badge colour.
const (
	apHigh   = "HIGH"
	apMedium = "MEDIUM"
	apLow    = "LOW"
)

type apViolation struct {
	File    string
	Line    int
	Snippet string
	Author  string
}

type apCheck struct {
	Name        string
	Description string
	Priority    string // apHigh / apMedium / apLow
	Detect      func(f *parser.ParsedFile, lines []string) []apViolation
}

type apResult struct {
	Check      apCheck
	Violations []apViolation
}

// ── shared regexes ────────────────────────────────────────────────────────────

var (
	reIgnoredErr      = regexp.MustCompile(`^\s*_\s*=\s*[\w.]+\(`)
	reErrNotWrapped   = regexp.MustCompile(`errors\.New\(\w[\w.]*\.Error\(\)\)`)
	rePanicCall       = regexp.MustCompile(`\bpanic\(`)
	reTypeAssertNook  = regexp.MustCompile(`:=\s*\w[\w.()\[\]]*\.\(([A-Za-z*][\w.*]*)\)\s*(?:$|//)`)
	reForLine         = regexp.MustCompile(`\bfor\b.*\{`)
	reForRangeLine    = regexp.MustCompile(`\bfor\b.+:=\s*range\b`)
	reDeferLine       = regexp.MustCompile(`^\s*defer\s`)
	reDeferClose      = regexp.MustCompile(`^\s*defer\s+\w[\w.]*\.(?:Close|Flush|Sync)\s*\(\s*\)\s*(?:$|//)`)
	reChanBuf         = regexp.MustCompile(`make\(\s*chan\b[^,)]+,\s*(\d+)`)
	reTimeSleep       = regexp.MustCompile(`\btime\.Sleep\b`)
	rePkgUnderscore   = regexp.MustCompile(`^package\s+[a-z]+_[a-z]`)
	reInitFunc        = regexp.MustCompile(`^\s*func\s+init\s*\(\s*\)`)
	reFmtSprintfInt   = regexp.MustCompile(`fmt\.Sprintf\s*\(\s*"%d"\s*,`)
	reHardSecret      = regexp.MustCompile(`(?i)(password|passwd|secret|apikey|api_key|authkey)\s*[:=]+\s*"([^"]{8,})"`)
	reSQLConcat       = regexp.MustCompile(`\.(Query|Exec|QueryRow|QueryContext|ExecContext)\s*\([^)]*\+`)
	reSQLSprintf      = regexp.MustCompile(`\.(Query|Exec|QueryRow|QueryContext|ExecContext)\s*\(\s*fmt\.Sprintf`)
	reMathRandImport  = regexp.MustCompile(`"math/rand"`)
	reBytesInLoop     = regexp.MustCompile(`\[\]byte\(`)
	reRowsErrCheck    = regexp.MustCompile(`rows\.Err\(\)`)
	reRowsClose       = regexp.MustCompile(`rows\.(?:Close|close)\(\)`)
	reRowsNext        = regexp.MustCompile(`\.Next\(\)`)
	reHTTPClientCall  = regexp.MustCompile(`\b(http\.(Get|Post|Put|Delete|Head)|\.Do\(|\.Get\(|\.Post\()`)
	reBodyClose       = regexp.MustCompile(`defer.*\.Body\.Close\(\)`)
	reGoFunc          = regexp.MustCompile(`\bgo\s+func\b`)
	reNakedReturn     = regexp.MustCompile(`^\s*return\s*(?://.*)?$`)
	rePtrToIface      = regexp.MustCompile(`\*interface\{`)
	reMakeNoCapacity  = regexp.MustCompile(`make\(\s*\[\][\w\[\]*]+,\s*0\s*\)`)
	reVarSlice        = regexp.MustCompile(`^\s*var\s+(\w+)\s+\[\]`)
	reAppend          = regexp.MustCompile(`\bappend\(`)
	reErrDecl         = regexp.MustCompile(`,\s*err\s*:=|^\s*err\s*:=`)
	reTypeStructStart = regexp.MustCompile(`^type\s+(\w+)\s+struct\b`)
	reFieldMutex      = regexp.MustCompile(`\bsync\.(RW)?Mutex\b`)
	reTestErrAssign   = regexp.MustCompile(`,\s*err\s*:=|^\s*err\s*:=`)
	reTestErrCheck    = regexp.MustCompile(`err\s*!=\s*nil|require\.NoError|assert\.NoError|t\.Fatal|t\.Error\s*\(\s*err`)
)

// ── helpers ───────────────────────────────────────────────────────────────────

func apSnippet(line string) string {
	s := strings.TrimSpace(line)
	if len(s) > 100 {
		s = s[:100] + "…"
	}
	return s
}

func apDisplayPath(filePath string) string {
	parts := strings.Split(strings.ReplaceAll(filePath, "\\", "/"), "/")
	if len(parts) > 3 {
		return strings.Join(parts[len(parts)-3:], "/")
	}
	return strings.Join(parts, "/")
}

func viol(f *parser.ParsedFile, lineIdx int, lines []string) apViolation {
	return apViolation{
		File:    apDisplayPath(f.FilePath),
		Line:    lineIdx + 1,
		Snippet: apSnippet(lines[lineIdx]),
	}
}

func lineIndent(line string) int {
	n := 0
	for _, ch := range line {
		switch ch {
		case '\t':
			n += 4
		case ' ':
			n++
		default:
			return n
		}
	}
	return n
}

// ── individual checks ─────────────────────────────────────────────────────────

func checkHardcodedSecret(f *parser.ParsedFile, lines []string) []apViolation {
	if strings.HasSuffix(f.FilePath, "_test.go") {
		return nil
	}
	skipWords := []string{"example", "test", "dummy", "fake", "sample", "placeholder", "your_", "xxx", "todo", "changeme"}
	var out []apViolation
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		m := reHardSecret.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		val := strings.ToLower(m[2])
		skip := false
		for _, w := range skipWords {
			if strings.Contains(val, w) {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, viol(f, i, lines))
			if len(out) >= apMaxViolations {
				break
			}
		}
	}
	return out
}

func checkSQLInjection(f *parser.ParsedFile, lines []string) []apViolation {
	var out []apViolation
	for i, line := range lines {
		if reSQLConcat.MatchString(line) || reSQLSprintf.MatchString(line) {
			out = append(out, viol(f, i, lines))
			if len(out) >= apMaxViolations {
				break
			}
		}
	}
	return out
}

func checkMathRand(f *parser.ParsedFile, lines []string) []apViolation {
	if strings.HasSuffix(f.FilePath, "_test.go") {
		return nil
	}
	for i, line := range lines {
		if reMathRandImport.MatchString(line) {
			return []apViolation{viol(f, i, lines)}
		}
	}
	return nil
}

func checkPanic(f *parser.ParsedFile, lines []string) []apViolation {
	if f.FileType == "proto" || strings.HasSuffix(f.FilePath, "_test.go") || strings.HasSuffix(f.FilePath, "main.go") {
		return nil
	}
	var out []apViolation
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		if rePanicCall.MatchString(line) {
			out = append(out, viol(f, i, lines))
			if len(out) >= apMaxViolations {
				break
			}
		}
	}
	return out
}

func checkTypeAssertNook(f *parser.ParsedFile, lines []string) []apViolation {
	var out []apViolation
	for i, line := range lines {
		if strings.Contains(line, ", ok") || strings.Contains(line, ",ok") || strings.Contains(line, ".(type)") {
			continue
		}
		if reTypeAssertNook.MatchString(line) {
			out = append(out, viol(f, i, lines))
			if len(out) >= apMaxViolations {
				break
			}
		}
	}
	return out
}

func checkUnclosedResponseBody(f *parser.ParsedFile, lines []string) []apViolation {
	var out []apViolation
	for i, line := range lines {
		if !reHTTPClientCall.MatchString(line) || !strings.Contains(line, ":=") {
			continue
		}
		hasClose := false
		for j := i; j < len(lines) && j < i+6; j++ {
			if reBodyClose.MatchString(lines[j]) {
				hasClose = true
				break
			}
		}
		if !hasClose {
			out = append(out, viol(f, i, lines))
			if len(out) >= apMaxViolations {
				break
			}
		}
	}
	return out
}

func checkLoopVarGoroutine(f *parser.ParsedFile, lines []string) []apViolation {
	var out []apViolation
	for i, line := range lines {
		if !reForRangeLine.MatchString(line) {
			continue
		}
		// Look for go func() within the next 20 lines
		for j := i + 1; j < len(lines) && j < i+20; j++ {
			jLine := lines[j]
			if !reGoFunc.MatchString(jLine) {
				continue
			}
			// Check that the loop variable isn't re-declared (v := v pattern)
			hasCapture := true
			for k := j; k < len(lines) && k < j+10; k++ {
				// v := v pattern indicates correct capture
				if regexp.MustCompile(`\w+\s*:=\s*\w+`).MatchString(lines[k]) &&
					strings.Count(lines[k], ":=") == 1 {
					parts := strings.SplitN(lines[k], ":=", 2)
					lhs := strings.TrimSpace(parts[0])
					rhs := strings.TrimSpace(parts[1])
					if lhs == rhs {
						hasCapture = false
						break
					}
				}
			}
			if hasCapture {
				out = append(out, viol(f, j, lines))
				if len(out) >= apMaxViolations {
					break
				}
			}
			break
		}
		if len(out) >= apMaxViolations {
			break
		}
	}
	return out
}

func checkCopyMutex(f *parser.ParsedFile, lines []string) []apViolation {
	// Step 1: collect struct type names that embed sync.Mutex/RWMutex
	mutexTypes := make(map[string]bool)
	var curType string
	structDepth := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if m := reTypeStructStart.FindStringSubmatch(trimmed); m != nil {
			curType = m[1]
			structDepth = 1
			continue
		}
		if structDepth > 0 {
			structDepth += strings.Count(line, "{") - strings.Count(line, "}")
			if reFieldMutex.MatchString(line) && curType != "" {
				mutexTypes[curType] = true
			}
			if structDepth <= 0 {
				structDepth = 0
				curType = ""
			}
		}
	}
	if len(mutexTypes) == 0 {
		return nil
	}

	// Step 2: flag function signatures that accept mutex-containing types by value
	var out []apViolation
	for i, line := range lines {
		if !strings.Contains(line, "func ") {
			continue
		}
		for typeName := range mutexTypes {
			reByVal := regexp.MustCompile(`[^*\w]` + regexp.QuoteMeta(typeName) + `[\s,)]`)
			reByPtr := regexp.MustCompile(`\*` + regexp.QuoteMeta(typeName) + `\b`)
			if reByVal.MatchString(line) && !reByPtr.MatchString(line) {
				out = append(out, viol(f, i, lines))
				break
			}
		}
		if len(out) >= apMaxViolations {
			break
		}
	}
	return out
}

func checkIgnoredErrors(f *parser.ParsedFile, lines []string) []apViolation {
	var out []apViolation
	for i, line := range lines {
		cmtIdx := strings.Index(line, "//")
		eqIdx := strings.Index(line, "_ =")
		if cmtIdx >= 0 && eqIdx >= 0 && cmtIdx < eqIdx {
			continue
		}
		if reIgnoredErr.MatchString(line) {
			out = append(out, viol(f, i, lines))
			if len(out) >= apMaxViolations {
				break
			}
		}
	}
	return out
}

func checkErrorNotWrapped(f *parser.ParsedFile, lines []string) []apViolation {
	var out []apViolation
	for i, line := range lines {
		if reErrNotWrapped.MatchString(line) {
			out = append(out, viol(f, i, lines))
			if len(out) >= apMaxViolations {
				break
			}
		}
	}
	return out
}

func checkErrShadowing(f *parser.ParsedFile, lines []string) []apViolation {
	var out []apViolation
	for i, line := range lines {
		if !reErrDecl.MatchString(line) {
			continue
		}
		outerIndent := lineIndent(line)
		for j := i + 1; j < len(lines) && j < i+30; j++ {
			jLine := lines[j]
			jtrim := strings.TrimSpace(jLine)
			if jtrim == "" || strings.HasPrefix(jtrim, "//") {
				continue
			}
			innerIndent := lineIndent(jLine)
			if innerIndent <= outerIndent {
				break
			}
			if reErrDecl.MatchString(jLine) {
				out = append(out, viol(f, j, lines))
				break
			}
		}
		if len(out) >= apMaxViolations {
			break
		}
	}
	return out
}

func checkUncheckedClose(f *parser.ParsedFile, lines []string) []apViolation {
	var out []apViolation
	for i, line := range lines {
		if reDeferClose.MatchString(line) {
			out = append(out, viol(f, i, lines))
			if len(out) >= apMaxViolations {
				break
			}
		}
	}
	return out
}

func checkDeferInLoop(f *parser.ParsedFile, lines []string) []apViolation {
	var out []apViolation
	forDepth := 0
	braceStack := []int{}

	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		opens := strings.Count(line, "{")
		closes := strings.Count(line, "}")
		isFor := reForLine.MatchString(line)
		if isFor && opens > closes {
			braceStack = append(braceStack, 0)
			forDepth++
		}
		if forDepth > 0 && reDeferLine.MatchString(line) {
			out = append(out, viol(f, i, lines))
			if len(out) >= apMaxViolations {
				break
			}
		}
		if closes > opens && forDepth > 0 {
			forDepth--
			if len(braceStack) > 0 {
				braceStack = braceStack[:len(braceStack)-1]
			}
		}
	}
	return out
}

func checkSQLRowsErr(f *parser.ParsedFile, lines []string) []apViolation {
	type funcBlock struct {
		start   int
		hasNext bool
		hasErr  bool
	}
	var blocks []funcBlock
	var cur *funcBlock
	depth := 0

	for i, line := range lines {
		opens := strings.Count(line, "{")
		closes := strings.Count(line, "}")
		if strings.Contains(line, "func ") && opens > closes {
			b := funcBlock{start: i}
			blocks = append(blocks, b)
			cur = &blocks[len(blocks)-1]
			depth = 1
			continue
		}
		if cur != nil {
			if reRowsNext.MatchString(line) {
				cur.hasNext = true
			}
			if reRowsErrCheck.MatchString(line) {
				cur.hasErr = true
			}
			depth += opens - closes
			if depth <= 0 {
				cur = nil
				depth = 0
			}
		}
	}

	var out []apViolation
	for _, b := range blocks {
		if b.hasNext && !b.hasErr {
			out = append(out, apViolation{
				File:    apDisplayPath(f.FilePath),
				Line:    b.start + 1,
				Snippet: apSnippet(lines[b.start]),
			})
			if len(out) >= apMaxViolations {
				break
			}
		}
	}
	return out
}

func checkRowsClose(f *parser.ParsedFile, lines []string) []apViolation {
	type funcBlock struct {
		start    int
		hasNext  bool
		hasClose bool
	}
	var blocks []funcBlock
	var cur *funcBlock
	depth := 0

	for i, line := range lines {
		opens := strings.Count(line, "{")
		closes := strings.Count(line, "}")
		if strings.Contains(line, "func ") && opens > closes {
			b := funcBlock{start: i}
			blocks = append(blocks, b)
			cur = &blocks[len(blocks)-1]
			depth = 1
			continue
		}
		if cur != nil {
			if reRowsNext.MatchString(line) {
				cur.hasNext = true
			}
			if reRowsClose.MatchString(line) {
				cur.hasClose = true
			}
			depth += opens - closes
			if depth <= 0 {
				cur = nil
				depth = 0
			}
		}
	}

	var out []apViolation
	for _, b := range blocks {
		if b.hasNext && !b.hasClose {
			out = append(out, apViolation{
				File:    apDisplayPath(f.FilePath),
				Line:    b.start + 1,
				Snippet: apSnippet(lines[b.start]),
			})
			if len(out) >= apMaxViolations {
				break
			}
		}
	}
	return out
}

func checkTestErrorCheck(f *parser.ParsedFile, lines []string) []apViolation {
	if !strings.HasSuffix(f.FilePath, "_test.go") {
		return nil
	}
	var out []apViolation
	for i, line := range lines {
		if !reTestErrAssign.MatchString(line) {
			continue
		}
		hasCheck := false
		for j := i + 1; j < len(lines) && j < i+4; j++ {
			if reTestErrCheck.MatchString(lines[j]) {
				hasCheck = true
				break
			}
		}
		if !hasCheck {
			out = append(out, viol(f, i, lines))
			if len(out) >= apMaxViolations {
				break
			}
		}
	}
	return out
}

func checkTimeSleepSync(f *parser.ParsedFile, lines []string) []apViolation {
	if strings.HasSuffix(f.FilePath, "_test.go") {
		return nil
	}
	var out []apViolation
	for i, line := range lines {
		if reTimeSleep.MatchString(line) {
			out = append(out, viol(f, i, lines))
			if len(out) >= apMaxViolations {
				break
			}
		}
	}
	return out
}

func checkTimeSleepInTests(f *parser.ParsedFile, lines []string) []apViolation {
	if !strings.HasSuffix(f.FilePath, "_test.go") {
		return nil
	}
	var out []apViolation
	for i, line := range lines {
		if reTimeSleep.MatchString(line) {
			out = append(out, viol(f, i, lines))
			if len(out) >= apMaxViolations {
				break
			}
		}
	}
	return out
}

func checkBigChannelBuffer(f *parser.ParsedFile, lines []string) []apViolation {
	var out []apViolation
	for i, line := range lines {
		m := reChanBuf.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil || n <= 1 {
			continue
		}
		out = append(out, viol(f, i, lines))
		if len(out) >= apMaxViolations {
			break
		}
	}
	return out
}

func checkNakedReturn(f *parser.ParsedFile, lines []string) []apViolation {
	var out []apViolation
	for i, line := range lines {
		if reNakedReturn.MatchString(line) {
			out = append(out, viol(f, i, lines))
			if len(out) >= apMaxViolations {
				break
			}
		}
	}
	return out
}

func checkPtrToInterface(f *parser.ParsedFile, lines []string) []apViolation {
	var out []apViolation
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		if rePtrToIface.MatchString(line) {
			out = append(out, viol(f, i, lines))
			if len(out) >= apMaxViolations {
				break
			}
		}
	}
	return out
}

func checkSlicePrealloc(f *parser.ParsedFile, lines []string) []apViolation {
	var out []apViolation
	for i, line := range lines {
		isNoCapMake := reMakeNoCapacity.MatchString(line)
		isVarSlice := reVarSlice.MatchString(line)
		if !isNoCapMake && !isVarSlice {
			continue
		}
		// Look ahead for a for loop with append
		hasLoop := false
		hasAppend := false
		for j := i + 1; j < len(lines) && j < i+30; j++ {
			if reForLine.MatchString(lines[j]) {
				hasLoop = true
			}
			if hasLoop && reAppend.MatchString(lines[j]) {
				hasAppend = true
				break
			}
		}
		if hasLoop && hasAppend {
			out = append(out, viol(f, i, lines))
			if len(out) >= apMaxViolations {
				break
			}
		}
	}
	return out
}

func checkPackageUnderscore(f *parser.ParsedFile, lines []string) []apViolation {
	for i, line := range lines {
		if rePkgUnderscore.MatchString(line) {
			return []apViolation{viol(f, i, lines)}
		}
	}
	return nil
}

func checkInitFunc(f *parser.ParsedFile, lines []string) []apViolation {
	if strings.HasSuffix(f.FilePath, "_test.go") {
		return nil
	}
	var out []apViolation
	for i, line := range lines {
		if reInitFunc.MatchString(line) {
			out = append(out, viol(f, i, lines))
			if len(out) >= apMaxViolations {
				break
			}
		}
	}
	return out
}

func checkFmtSprintfInt(f *parser.ParsedFile, lines []string) []apViolation {
	var out []apViolation
	for i, line := range lines {
		if reFmtSprintfInt.MatchString(line) {
			out = append(out, viol(f, i, lines))
			if len(out) >= apMaxViolations {
				break
			}
		}
	}
	return out
}

func checkBytesInLoop(f *parser.ParsedFile, lines []string) []apViolation {
	var out []apViolation
	forDepth := 0
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		opens := strings.Count(line, "{")
		closes := strings.Count(line, "}")
		if reForLine.MatchString(line) {
			forDepth++
		}
		if forDepth > 0 && reBytesInLoop.MatchString(line) {
			out = append(out, viol(f, i, lines))
			if len(out) >= apMaxViolations {
				break
			}
		}
		net := closes - opens
		if net > 0 && forDepth > 0 {
			forDepth -= net
			if forDepth < 0 {
				forDepth = 0
			}
		}
	}
	return out
}

// ── check registry ────────────────────────────────────────────────────────────

func goAntipatternChecks() []apCheck {
	return []apCheck{
		// ── HIGH ──────────────────────────────────────────────────────────────
		{
			Name:        "Hardcoded Secrets",
			Priority:    apHigh,
			Description: "Hardcoding passwords, secrets, or API keys embeds credentials into version history permanently. Load secrets from environment variables or a secrets manager at runtime. Any credential found here must be rotated immediately — consider it compromised.",
			Detect:      checkHardcodedSecret,
		},
		{
			Name:        "SQL Injection via String Concatenation",
			Priority:    apHigh,
			Description: "Building SQL queries with `+` or `fmt.Sprintf` allows attackers to inject arbitrary SQL. Always use parameterized queries: `db.Query(\"SELECT ... WHERE id = $1\", id)`. This is one of the most critical security vulnerabilities.",
			Detect:      checkSQLInjection,
		},
		{
			Name:        "math/rand Used (Use crypto/rand for Secrets)",
			Priority:    apHigh,
			Description: "`math/rand` produces predictable pseudo-random numbers. Never use it for security-sensitive values (tokens, keys, nonces, session IDs). Use `crypto/rand` for anything that must be cryptographically unpredictable.",
			Detect:      checkMathRand,
		},
		{
			Name:        "panic() in Business Logic",
			Priority:    apHigh,
			Description: "Reserve `panic` for truly unrecoverable initialization failures. Business logic must return an `error` so callers can handle it gracefully. A `panic` in a goroutine crashes the entire process.",
			Detect:      checkPanic,
		},
		{
			Name:        "Type Assertion Without ok",
			Priority:    apHigh,
			Description: "`x := iface.(MyType)` panics if the interface holds a different concrete type. Always use the two-value form: `x, ok := iface.(MyType)` and check `ok` before using `x`.",
			Detect:      checkTypeAssertNook,
		},
		{
			Name:        "Unclosed HTTP Response Body",
			Priority:    apHigh,
			Description: "Every HTTP response body must be closed to return the underlying connection to the pool. Add `defer resp.Body.Close()` immediately after checking the error from `http.Get` / `client.Do`. Missing this leaks connections and file descriptors.",
			Detect:      checkUnclosedResponseBody,
		},
		{
			Name:        "Loop Variable Captured in Goroutine",
			Priority:    apHigh,
			Description: "A goroutine that closes over a `for … range` loop variable reads the variable's value at runtime, not at launch time. By then the loop may have advanced. Capture it explicitly: `v := v` before the `go func()`. Note: fixed in Go 1.22+ per-iteration semantics.",
			Detect:      checkLoopVarGoroutine,
		},
		{
			Name:        "Copying sync.Mutex",
			Priority:    apHigh,
			Description: "Passing a struct containing `sync.Mutex` (or `sync.RWMutex`) by value copies the lock state and silently breaks locking — the copy and the original have independent lock counts. Always pass such structs by pointer. `go vet` catches this as `copylocks`.",
			Detect:      checkCopyMutex,
		},
		// ── MEDIUM ────────────────────────────────────────────────────────────
		{
			Name:        "Error Not Wrapped With %w",
			Priority:    apMedium,
			Description: "`errors.New(err.Error())` converts the error to a plain string and breaks the error chain, making `errors.Is` / `errors.As` useless for callers. Use `fmt.Errorf(\"context: %w\", err)` to preserve the original error.",
			Detect:      checkErrorNotWrapped,
		},
		{
			Name:        "defer Inside a Loop",
			Priority:    apMedium,
			Description: "A `defer` inside a `for` loop does not execute per iteration — it queues up and runs only when the enclosing function returns. This accumulates file handles, database connections, or locks for the entire loop duration. Call `Close()` explicitly inside the loop body.",
			Detect:      checkDeferInLoop,
		},
		{
			Name:        "SQL Rows — Missing rows.Err() Check",
			Priority:    apMedium,
			Description: "After `for rows.Next() { ... }`, always call `if err := rows.Err(); err != nil { ... }`. Network interruptions or context cancellations during iteration are only surfaced through `rows.Err()` — `rows.Next()` returning false is not sufficient.",
			Detect:      checkSQLRowsErr,
		},
		{
			Name:        "SQL Rows — rows.Close() Not Called",
			Priority:    apMedium,
			Description: "Every `sql.Rows` value must have `rows.Close()` called to release the database connection. Omitting it holds the connection open for the lifetime of the enclosing function. Use `defer rows.Close()` immediately after checking the error from `db.Query`.",
			Detect:      checkRowsClose,
		},
		{
			Name:        "time.Sleep for Goroutine Synchronization",
			Priority:    apMedium,
			Description: "`time.Sleep` is not a reliable synchronization primitive — it creates flaky, timing-dependent code that fails under load or on slow CI machines. Use `sync.WaitGroup`, channels, or `sync/atomic` to coordinate goroutines.",
			Detect:      checkTimeSleepSync,
		},
		// ── LOW ───────────────────────────────────────────────────────────────
		{
			Name:        "Large Channel Buffer",
			Priority:    apLow,
			Description: "A channel buffer larger than 1 usually hides a concurrency design problem. Buffers of 0 (synchronous handoff) or 1 (one-item decoupling) are almost always the right choice. Larger buffers often mask missing backpressure or rate-limiting.",
			Detect:      checkBigChannelBuffer,
		},
		{
			Name:        "time.Sleep in Tests",
			Priority:    apLow,
			Description: "Tests that sleep are slow and flaky. Use `sync.WaitGroup`, channels with `select`/timeout, or `testify/assert.Eventually` to wait for asynchronous conditions deterministically instead of sleeping for an arbitrary duration.",
			Detect:      checkTimeSleepInTests,
		},
		{
			Name:        "Naked Returns",
			Priority:    apLow,
			Description: "Bare `return` in a function with named return values silently returns whatever the named variables hold at that point. In functions longer than a few lines this makes it impossible to tell at the call site what is being returned. Always return values explicitly.",
			Detect:      checkNakedReturn,
		},
		{
			Name:        "Pointer to Interface",
			Priority:    apLow,
			Description: "An interface value already can hold a pointer internally. Taking the address of an interface (`*interface{}`) adds a level of indirection with no benefit, complicates nil checks, and prevents the compiler from using the interface's dynamic dispatch correctly.",
			Detect:      checkPtrToInterface,
		},
		{
			Name:        "Missing Slice Pre-allocation",
			Priority:    apLow,
			Description: "`var s []T` or `make([]T, 0)` without a capacity causes the runtime to reallocate and copy the backing array as the slice grows (typically doubling at 1, 2, 4, 8 … elements). If the final length is known or estimable, use `make([]T, 0, expectedLen)` to allocate once.",
			Detect:      checkSlicePrealloc,
		},
		{
			Name:        "Package Name Contains Underscore",
			Priority:    apLow,
			Description: "Go package names should be short, lowercase, and without underscores. Underscores are a sign the package could be split or renamed. Prefer `httputil` over `http_util`. The Go naming convention is documented in Effective Go.",
			Detect:      checkPackageUnderscore,
		},
		{
			Name:        "init() Function",
			Priority:    apLow,
			Description: "`init()` runs automatically at package load time in an implicit, hard-to-control order. This makes unit testing harder and creates hidden dependencies between packages. Prefer explicit initialization functions called from `main()` or dependency-injection constructors.",
			Detect:      checkInitFunc,
		},
		{
			Name:        "fmt.Sprintf for Integer → String",
			Priority:    apLow,
			Description: "`fmt.Sprintf(\"%d\", n)` involves reflection and allocates more than necessary. Use `strconv.Itoa(n)` (for int) or `strconv.FormatInt(n, 10)` (for int64) — they are faster and allocation-efficient, especially in hot paths.",
			Detect:      checkFmtSprintfInt,
		},
		{
			Name:        "[]byte Conversion in Loop",
			Priority:    apLow,
			Description: "Each `[]byte(str)` call allocates and copies the string's bytes. Inside a loop this generates O(n) garbage. Cache the converted slice before the loop, or restructure the code to use `strings` package functions or a reused `[]byte` buffer.",
			Detect:      checkBytesInLoop,
		},
	}
}

// ── runner ────────────────────────────────────────────────────────────────────

func runAntipatterns(files []*parser.ParsedFile, gitRepos []string) []apResult {
	checks := goAntipatternChecks()
	results := make([]apResult, len(checks))
	for i, ch := range checks {
		results[i].Check = ch
	}
	blameCache := make(map[string]map[int]string)
	for _, f := range files {
		if f.FileType == "proto" || strings.HasSuffix(f.FilePath, ".pb.go") {
			continue
		}
		data, err := os.ReadFile(f.FilePath)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for i := range results {
			vs := results[i].Check.Detect(f, lines)
			if len(vs) > 0 && len(gitRepos) > 0 {
				blame, ok := blameCache[f.FilePath]
				if !ok {
					blame = gitpkg.BlameAuthors(gitRepos, f.FilePath)
					blameCache[f.FilePath] = blame
				}
				for j := range vs {
					if blame != nil {
						vs[j].Author = blame[vs[j].Line]
					}
				}
			}
			results[i].Violations = append(results[i].Violations, vs...)
		}
	}
	for i := range results {
		if len(results[i].Violations) > apMaxViolations {
			results[i].Violations = results[i].Violations[:apMaxViolations]
		}
	}
	return results
}

// ── HTML builder ──────────────────────────────────────────────────────────────

func apPriorityBadge(p string) string {
	switch p {
	case apHigh:
		return `<span class="ap-priority ap-pri-high">HIGH</span>`
	case apMedium:
		return `<span class="ap-priority ap-pri-med">MEDIUM</span>`
	default:
		return `<span class="ap-priority ap-pri-low">LOW</span>`
	}
}

func buildAntipatternHTML(results []apResult) string {
	byPriority := map[string][]apResult{apHigh: nil, apMedium: nil, apLow: nil}
	var passed []apResult
	failedTotal := 0
	for _, r := range results {
		if len(r.Violations) > 0 {
			byPriority[r.Check.Priority] = append(byPriority[r.Check.Priority], r)
			failedTotal++
		} else {
			passed = append(passed, r)
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		`<h2>⚠️ Anti-patterns <span style="color:var(--text3);font-size:14px;font-weight:400">(%d checks)</span></h2>`,
		len(results),
	))
	if len(passed) > 0 {
		sb.WriteString(fmt.Sprintf(`<div class="ap-summary"><span class="ap-pass-badge">✅ %d passed</span></div>`, len(passed)))
		sb.WriteString(`<div class="ap-passed-list" style="display:grid;grid-template-columns:1fr 1fr 1fr;gap:6px;margin-bottom:24px">`)
		for _, r := range passed {
			sb.WriteString(fmt.Sprintf(
				`<div class="ap-passed-item"><span style="color:var(--green)">✅</span><span class="ap-lang-badge ap-lang-badge-go">GO</span>%s<span>%s</span></div>`,
				apPriorityBadge(r.Check.Priority), esc(r.Check.Name),
			))
		}
		sb.WriteString(`</div>`)
	}

	sb.WriteString(fmt.Sprintf(`<div class="ap-summary"><span class="ap-fail-badge">❌ %d failed</span></div>`, failedTotal))

	for _, pri := range []string{apHigh, apMedium, apLow} {
		for _, r := range byPriority[pri] {
			sb.WriteString(`<div class="ap-check">`)
			sb.WriteString(fmt.Sprintf(
				`<div class="ap-check-header">%s<span class="ap-lang-badge ap-lang-badge-go">GO</span><span class="ap-check-title">%s</span><span class="ap-check-count">%d violations</span></div>`,
				apPriorityBadge(r.Check.Priority), esc(r.Check.Name), len(r.Violations),
			))
			sb.WriteString(`<div class="ap-violations">`)
			sb.WriteString(fmt.Sprintf(`<div class="ap-check-desc-text">%s</div>`, esc(r.Check.Description)))
			for _, v := range r.Violations {
				authorBadge := ""
				if v.Author != "" {
					authorBadge = fmt.Sprintf(`<span class="ap-author-badge">%s</span>`, esc(v.Author))
				}
				sb.WriteString(fmt.Sprintf(
					`<div class="ap-violation"><span class="ap-file">%s:%d</span><span class="ap-snippet">%s</span>%s</div>`,
					esc(v.File), v.Line, esc(v.Snippet), authorBadge,
				))
			}
			sb.WriteString(`</div></div>`)
		}
	}

	return sb.String()
}
