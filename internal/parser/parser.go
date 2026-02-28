package parser

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	goImportSingle = regexp.MustCompile(`^\s*import\s+"([^"]+)"`)
	goImportBlock  = regexp.MustCompile(`^\s*"([^"]+)"`)
	goDeclPattern  = regexp.MustCompile(`^(?:type|func|var|const)\s+`)
	goTypeDecl     = regexp.MustCompile(`^type\s+(\w+)\s+(struct|interface)\b`)
	goFuncDecl     = regexp.MustCompile(`^func\s+(?:\(\s*\w+\s+\*?\w+\s*\)\s+)?(\w+)\s*\(`)
	goDocComment   = regexp.MustCompile(`^//\s?(.*)`)
)

// ParseGoFile parses a .go file and extracts imports, declarations, etc.
func ParseGoFile(filePath, microservice string) (*ParsedFile, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var (
		imports      []string
		declarations []Declaration
		lineCount    int
		todoCount    int
		fixmeCount   int
		pkgName      string
		description  string
		docLines     []string
		inImportBlock bool
	)

	// Longest function tracking
	var (
		bestFunc     *FunctionInfo
		bigFuncs     []FunctionInfo
		curFuncName  string
		funcStart    int
		braceDepth   int
		inFunc       bool
	)

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		lineCount++
		trimmed := strings.TrimSpace(line)

		// Package declaration
		if pkgName == "" && strings.HasPrefix(trimmed, "package ") {
			pkgName = strings.TrimPrefix(trimmed, "package ")
			pkgName = strings.TrimSpace(pkgName)
		}

		// Import handling
		if strings.HasPrefix(trimmed, "import (") {
			inImportBlock = true
			continue
		}
		if inImportBlock {
			if trimmed == ")" {
				inImportBlock = false
				continue
			}
			if m := goImportBlock.FindStringSubmatch(trimmed); len(m) > 1 {
				imports = append(imports, m[1])
			}
			continue
		}
		if m := goImportSingle.FindStringSubmatch(trimmed); len(m) > 1 {
			imports = append(imports, m[1])
			continue
		}

		// Type declarations
		if m := goTypeDecl.FindStringSubmatch(trimmed); len(m) > 2 {
			kind := DeclStruct
			if m[2] == "interface" {
				kind = DeclInterface
			}
			declarations = append(declarations, Declaration{Name: m[1], Kind: kind})
			if description == "" && len(docLines) > 0 {
				description = strings.Join(docLines, " ")
			}
			docLines = nil
		}

		// Function declarations
		if m := goFuncDecl.FindStringSubmatch(trimmed); len(m) > 1 {
			declarations = append(declarations, Declaration{Name: m[1], Kind: DeclFunc})

			// Start tracking longest function
			if !inFunc {
				curFuncName = m[1]
				funcStart = lineCount
				braceDepth = 0
				inFunc = true
			}
		}

		// Brace counting for function length
		if inFunc {
			braceDepth += strings.Count(trimmed, "{") - strings.Count(trimmed, "}")
			if braceDepth <= 0 && strings.Contains(trimmed, "}") {
				length := lineCount - funcStart + 1
				if bestFunc == nil || length > bestFunc.LineCount {
					bestFunc = &FunctionInfo{
						Name:      curFuncName,
						LineCount: length,
						FilePath:  filePath,
					}
				}
				if length >= 50 {
					bigFuncs = append(bigFuncs, FunctionInfo{
						Name:      curFuncName,
						LineCount: length,
						FilePath:  filePath,
					})
				}
				inFunc = false
				curFuncName = ""
			}
		}

		// Doc comments (collect before declarations)
		if m := goDocComment.FindStringSubmatch(trimmed); len(m) > 1 {
			docLines = append(docLines, m[1])
		} else if trimmed != "" {
			docLines = nil
		}

		// TODO/FIXME
		if strings.Contains(trimmed, "// TODO") || strings.Contains(trimmed, "//TODO") {
			todoCount++
		}
		if strings.Contains(trimmed, "// FIXME") || strings.Contains(trimmed, "//FIXME") {
			fixmeCount++
		}
	}

	return &ParsedFile{
		FilePath:         filePath,
		ModuleName:       pkgName,
		Imports:          imports,
		Description:      description,
		LineCount:        lineCount,
		Declarations:     declarations,
		PackageName:      pkgName,
		MicroserviceName: microservice,
		TodoCount:        todoCount,
		FixmeCount:       fixmeCount,
		LongestFunction:  bestFunc,
		BigFunctions:     bigFuncs,
		FileType:         "go",
	}, nil
}

// ParseProtoFile parses a .proto file.
func ParseProtoFile(filePath, microservice string) (*ParsedFile, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var (
		imports      []string
		declarations []Declaration
		lineCount    int
		todoCount    int
		fixmeCount   int
		pkgName      string
		description  string
	)

	reImport := regexp.MustCompile(`^\s*import\s+"([^"]+)"`)
	rePackage := regexp.MustCompile(`^\s*package\s+(\S+)\s*;`)
	reMessage := regexp.MustCompile(`^\s*message\s+(\w+)`)
	reService := regexp.MustCompile(`^\s*service\s+(\w+)`)
	reRPC := regexp.MustCompile(`^\s*rpc\s+(\w+)`)
	reEnum := regexp.MustCompile(`^\s*enum\s+(\w+)`)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		lineCount++
		trimmed := strings.TrimSpace(line)

		if m := rePackage.FindStringSubmatch(trimmed); len(m) > 1 {
			pkgName = m[1]
		}
		if m := reImport.FindStringSubmatch(trimmed); len(m) > 1 {
			imports = append(imports, m[1])
		}
		if m := reMessage.FindStringSubmatch(trimmed); len(m) > 1 {
			declarations = append(declarations, Declaration{Name: m[1], Kind: DeclMessage})
		}
		if m := reService.FindStringSubmatch(trimmed); len(m) > 1 {
			declarations = append(declarations, Declaration{Name: m[1], Kind: DeclService})
		}
		if m := reRPC.FindStringSubmatch(trimmed); len(m) > 1 {
			declarations = append(declarations, Declaration{Name: m[1], Kind: DeclRPC})
		}
		if m := reEnum.FindStringSubmatch(trimmed); len(m) > 1 {
			declarations = append(declarations, Declaration{Name: m[1], Kind: DeclEnum})
		}

		if strings.Contains(trimmed, "// TODO") {
			todoCount++
		}
		if strings.Contains(trimmed, "// FIXME") {
			fixmeCount++
		}
	}

	return &ParsedFile{
		FilePath:         filePath,
		ModuleName:       pkgName,
		Imports:          imports,
		Description:      description,
		LineCount:        lineCount,
		Declarations:     declarations,
		PackageName:      pkgName,
		MicroserviceName: microservice,
		TodoCount:        todoCount,
		FixmeCount:       fixmeCount,
		FileType:         "proto",
	}, nil
}

// ParseFile dispatches to the right parser based on extension.
func ParseFile(filePath, microservice string) (*ParsedFile, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".go":
		return ParseGoFile(filePath, microservice)
	case ".proto":
		return ParseProtoFile(filePath, microservice)
	default:
		return nil, nil
	}
}
