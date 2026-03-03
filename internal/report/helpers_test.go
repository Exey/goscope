package report

import (
	"testing"

	"github.com/goscope/internal/parser"
)

func TestMatchGoTypeRef(t *testing.T) {
	tests := []struct {
		source   string
		typeName string
		want     bool
	}{
		// Basic match
		{"var x Report", "Report", true},
		{"*Report", "Report", true},
		{"[]Report{}", "Report", true},
		// Should NOT match as substring of longer identifier
		{"ReportService", "Report", false},
		{"MyReport", "Report", false},
		{"var reportData int", "Report", false},
		// Word boundary after
		{"Report.Field", "Report", true},
		{"Report)", "Report", true},
		{"Report\n", "Report", true},
		// Empty source
		{"", "Report", false},
		// Name at start of source
		{"Report is good", "Report", true},
		// Name at end
		{"use Report", "Report", true},
		// Case sensitive
		{"report", "Report", false},
	}

	for _, tt := range tests {
		got := matchGoTypeRef(tt.source, tt.typeName)
		if got != tt.want {
			t.Errorf("matchGoTypeRef(%q, %q) = %v, want %v", tt.source, tt.typeName, got, tt.want)
		}
	}
}

func TestIsIdentChar(t *testing.T) {
	for _, ch := range "azAZ09_" {
		if !isIdentChar(byte(ch)) {
			t.Errorf("isIdentChar(%c) = false, want true", ch)
		}
	}
	for _, ch := range " .*()[]{}:,\n\t" {
		if isIdentChar(byte(ch)) {
			t.Errorf("isIdentChar(%c) = true, want false", ch)
		}
	}
}

func TestFmtNum(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{42, "42"},
		{999, "999"},
		{1000, "1,000"},
		{12345, "12,345"},
		{1234567, "1,234,567"},
	}
	for _, tt := range tests {
		got := fmtNum(tt.n)
		if got != tt.want {
			t.Errorf("fmtNum(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestEsc(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"hello", "hello"},
		{"<script>", "&lt;script&gt;"},
		{`"quotes"`, "&quot;quotes&quot;"},
		{"a & b", "a &amp; b"},
		{"<b>\"hi\"</b>", "&lt;b&gt;&quot;hi&quot;&lt;/b&gt;"},
	}
	for _, tt := range tests {
		got := esc(tt.in)
		if got != tt.want {
			t.Errorf("esc(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSplitDirFile(t *testing.T) {
	tests := []struct {
		path     string
		wantDir  string
		wantFile string
	}{
		{"internal/domain/types.go", "internal/domain/", "types.go"},
		{"main.go", "", "main.go"},
		{"a/b/c/d.go", "a/b/c/", "d.go"},
	}
	for _, tt := range tests {
		dir, file := splitDirFile(tt.path)
		if dir != tt.wantDir || file != tt.wantFile {
			t.Errorf("splitDirFile(%q) = (%q, %q), want (%q, %q)", tt.path, dir, file, tt.wantDir, tt.wantFile)
		}
	}
}

func TestShortRelPath(t *testing.T) {
	tests := []struct {
		fullPath string
		ms       string
		want     string
	}{
		{"/code/bki/internal/domain/types.go", "bki", "internal/domain/types.go"},
		{"/code/api-gw/handlers/auth.go", "api-gw", "handlers/auth.go"},
		{"/very/deep/path/to/file.go", "unknown", "path/to/file.go"}, // fallback: last 3 segments
		{"/simple.go", "x", "simple.go"}, // fallback: basename
	}
	for _, tt := range tests {
		got := shortRelPath(tt.fullPath, tt.ms)
		if got != tt.want {
			t.Errorf("shortRelPath(%q, %q) = %q, want %q", tt.fullPath, tt.ms, got, tt.want)
		}
	}
}

func TestIsAPIGateway(t *testing.T) {
	for _, name := range []string{"api-gw", "gateway", "API-Gateway", "api"} {
		if !isAPIGateway(name) {
			t.Errorf("isAPIGateway(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"user-service", "bki", "proto"} {
		if isAPIGateway(name) {
			t.Errorf("isAPIGateway(%q) = true, want false", name)
		}
	}
}

func TestIsProtoMS(t *testing.T) {
	for _, name := range []string{"proto", "protobuf", "protos", "proto-api"} {
		if !isProtoMS(name) {
			t.Errorf("isProtoMS(%q) = false, want true", name)
		}
	}
	if isProtoMS("user-service") {
		t.Error("isProtoMS(user-service) should be false")
	}
}

func TestKindIcon(t *testing.T) {
	tests := map[parser.DeclKind]string{
		parser.DeclStruct:    "🟢",
		parser.DeclInterface: "🔵",
		parser.DeclFunc:      "🟠",
		parser.DeclService:   "🔴",
		parser.DeclEnum:      "🟡",
		parser.DeclMessage:   "📨",
		parser.DeclRPC:       "🔗",
	}
	for kind, want := range tests {
		got := kindIcon(kind)
		if got != want {
			t.Errorf("kindIcon(%q) = %q, want %q", kind, got, want)
		}
	}
}

func TestNewMS(t *testing.T) {
	files := []*parser.ParsedFile{
		{
			LineCount: 100,
			Declarations: []parser.Declaration{
				{Name: "Foo", Kind: parser.DeclStruct},
				{Name: "Bar", Kind: parser.DeclInterface},
				{Name: "Baz", Kind: parser.DeclFunc},
				{Name: "Qux", Kind: parser.DeclFunc},
			},
		},
		{
			LineCount: 50,
			Declarations: []parser.Declaration{
				{Name: "Msg", Kind: parser.DeclMessage},
				{Name: "Svc", Kind: parser.DeclService},
			},
		},
	}
	ms := newMS("test-svc", files)
	if ms.Name != "test-svc" {
		t.Errorf("Name = %q, want test-svc", ms.Name)
	}
	if ms.TotalLines != 150 {
		t.Errorf("TotalLines = %d, want 150", ms.TotalLines)
	}
	if ms.StructCount != 1 {
		t.Errorf("StructCount = %d, want 1", ms.StructCount)
	}
	if ms.InterfaceCount != 1 {
		t.Errorf("InterfaceCount = %d, want 1", ms.InterfaceCount)
	}
	if ms.FuncCount != 2 {
		t.Errorf("FuncCount = %d, want 2", ms.FuncCount)
	}
	if ms.MessageCount != 1 {
		t.Errorf("MessageCount = %d, want 1", ms.MessageCount)
	}
	if ms.ServiceCount != 1 {
		t.Errorf("ServiceCount = %d, want 1", ms.ServiceCount)
	}
	if len(ms.Declarations) != 6 {
		t.Errorf("Declarations count = %d, want 6", len(ms.Declarations))
	}
}

func TestTopNKeys(t *testing.T) {
	m := map[string]int{"a": 10, "b": 30, "c": 20}
	got := topNKeys(m, 2)
	if len(got) != 2 {
		t.Fatalf("topNKeys returned %d items, want 2", len(got))
	}
	if got[0] != "b" {
		t.Errorf("topNKeys[0] = %q, want b", got[0])
	}
	if got[1] != "c" {
		t.Errorf("topNKeys[1] = %q, want c", got[1])
	}
}

func TestDetectTechFromImport(t *testing.T) {
	tests := []struct {
		imp  string
		want string
	}{
		{"github.com/jackc/pgx/v5", "PostgreSQL"},
		{"google.golang.org/grpc", "gRPC"},
		{"github.com/go-redis/redis/v9", "Redis"},
		{"github.com/gin-gonic/gin", "Gin"},
		{"go.uber.org/zap", "Zap Logger"},
	}
	for _, tt := range tests {
		techSet := make(map[string]bool)
		detectTechFromImport(tt.imp, techSet)
		if !techSet[tt.want] {
			t.Errorf("detectTechFromImport(%q) did not detect %q, got %v", tt.imp, tt.want, techSet)
		}
	}
}
