package parser

// GitMetadata holds git history info for a file.
type GitMetadata struct {
	LastModified    float64  `json:"lastModified"`
	ChangeFrequency int      `json:"changeFrequency"`
	TopAuthors      []string `json:"topAuthors"`
	RecentMessages  []string `json:"recentMessages"`
	FirstCommitDate float64  `json:"firstCommitDate"`
}

// DeclKind represents the type of a Go declaration.
type DeclKind string

const (
	DeclStruct    DeclKind = "struct"
	DeclInterface DeclKind = "interface"
	DeclFunc      DeclKind = "func"
	DeclType      DeclKind = "type"
	DeclConst     DeclKind = "const"
	DeclVar       DeclKind = "var"
	// proto-specific
	DeclMessage DeclKind = "message"
	DeclService DeclKind = "service"
	DeclRPC     DeclKind = "rpc"
	DeclEnum    DeclKind = "enum"
)

// Declaration represents a named declaration in source code.
type Declaration struct {
	Name string   `json:"name"`
	Kind DeclKind `json:"kind"`
}

// FunctionInfo holds info about a function's size.
type FunctionInfo struct {
	Name     string `json:"name"`
	LineCount int   `json:"lineCount"`
	FilePath string `json:"filePath"`
}

// ParsedFile holds the parsed result for a single source file.
type ParsedFile struct {
	FilePath        string       `json:"filePath"`
	ModuleName      string       `json:"moduleName"` // microservice name
	Imports         []string     `json:"imports"`
	GitMeta         GitMetadata  `json:"gitMetadata"`
	Description     string       `json:"description"`
	LineCount       int          `json:"lineCount"`
	Declarations    []Declaration `json:"declarations"`
	PackageName     string       `json:"packageName"` // Go package name
	MicroserviceName string      `json:"microserviceName"`
	TodoCount       int          `json:"todoCount"`
	FixmeCount      int          `json:"fixmeCount"`
	LongestFunction *FunctionInfo `json:"longestFunction,omitempty"`
	FileType        string       `json:"fileType"` // "go" or "proto"
}

// FileName returns just the file name from the path.
func (p *ParsedFile) FileName() string {
	for i := len(p.FilePath) - 1; i >= 0; i-- {
		if p.FilePath[i] == '/' {
			return p.FilePath[i+1:]
		}
	}
	return p.FilePath
}

// FileNameWithoutExt returns the file name without extension.
func (p *ParsedFile) FileNameWithoutExt() string {
	name := p.FileName()
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '.' {
			return name[:i]
		}
	}
	return name
}
