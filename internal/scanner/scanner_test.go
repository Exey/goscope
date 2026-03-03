package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func setupTestTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// Create multi-repo layout
	dirs := []string{
		"api-gateway",
		"user-service",
		"proto",
		"src/payment-service",
		"src/notification-service",
		"services/auth",
		"not-a-service",
	}
	for _, d := range dirs {
		os.MkdirAll(filepath.Join(root, d), 0755)
	}

	// Add service markers
	markers := map[string]string{
		"api-gateway/go.mod":                "module api-gateway",
		"api-gateway/main.go":               "package main",
		"user-service/Dockerfile":           "FROM golang",
		"proto/user.proto":                  "syntax = \"proto3\";",
		"src/payment-service/go.mod":        "module payment",
		"src/payment-service/main.go":       "package main",
		"src/notification-service/Makefile": "build:",
		"services/auth/Dockerfile":          "FROM golang",
	}
	for path, content := range markers {
		full := filepath.Join(root, path)
		os.WriteFile(full, []byte(content), 0644)
	}

	return root
}

func TestIsServiceDir(t *testing.T) {
	root := setupTestTree(t)

	tests := []struct {
		dir  string
		want bool
	}{
		{filepath.Join(root, "api-gateway"), true},     // has go.mod + main.go
		{filepath.Join(root, "user-service"), true},     // has Dockerfile
		{filepath.Join(root, "not-a-service"), false},   // no markers
		{filepath.Join(root, "src/payment-service"), true}, // has go.mod
		{filepath.Join(root, "services/auth"), true},    // has Dockerfile
	}

	for _, tt := range tests {
		got := isServiceDir(tt.dir)
		name, _ := filepath.Rel(root, tt.dir)
		if got != tt.want {
			t.Errorf("isServiceDir(%q) = %v, want %v", name, got, tt.want)
		}
	}
}

func TestDiscoverServiceDirs(t *testing.T) {
	root := setupTestTree(t)
	dirs := discoverServiceDirs(root, map[string]bool{})

	// Should find: api-gateway, user-service, src/payment-service, src/notification-service, services/auth
	// proto has no standard marker (only .proto file) unless it has go.mod etc
	found := make(map[string]bool)
	for _, d := range dirs {
		name, _ := filepath.Rel(root, d)
		found[name] = true
	}

	expect := []string{"api-gateway", "user-service"}
	for _, e := range expect {
		if !found[e] {
			t.Errorf("discoverServiceDirs missing %q, found: %v", e, found)
		}
	}

	// src/payment-service should be found (src is a container dir)
	if !found[filepath.Join("src", "payment-service")] {
		t.Errorf("discoverServiceDirs missing src/payment-service, found: %v", found)
	}
}

func TestDetectMicroservice(t *testing.T) {
	root := "/code/backend"
	serviceDirs := []string{
		"/code/backend/api-gateway",
		"/code/backend/user-service",
		"/code/backend/src/payment",
	}

	tests := []struct {
		filePath string
		want     string
	}{
		{"/code/backend/api-gateway/main.go", "api-gateway"},
		{"/code/backend/api-gateway/internal/handler.go", "api-gateway"},
		{"/code/backend/user-service/pkg/models.go", "user-service"},
		{"/code/backend/src/payment/service.go", "payment"},
		{"/code/backend/standalone.go", "root"},
	}

	for _, tt := range tests {
		got := detectMicroservice(root, tt.filePath, serviceDirs)
		if got != tt.want {
			t.Errorf("detectMicroservice(%q) = %q, want %q", tt.filePath, got, tt.want)
		}
	}
}

func TestDetectServicesRoot(t *testing.T) {
	root := "/code"
	// 3 services under src/, 1 at root level
	dirs := []string{
		"/code/src/svc-a",
		"/code/src/svc-b",
		"/code/src/svc-c",
		"/code/standalone",
	}
	got := detectServicesRoot(root, dirs)
	if got != "src" {
		t.Errorf("detectServicesRoot = %q, want src", got)
	}
}

func TestDetectServicesRoot_NoPattern(t *testing.T) {
	root := "/code"
	dirs := []string{"/code/a", "/code/b"}
	got := detectServicesRoot(root, dirs)
	if got != "" {
		t.Errorf("detectServicesRoot with flat layout = %q, want empty", got)
	}
}

func TestCountFileLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("line1\nline2\nline3\n"), 0644)

	got := countFileLines(path)
	if got != 3 {
		t.Errorf("countFileLines = %d, want 3", got)
	}
}

func TestCountFileLines_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	os.WriteFile(path, []byte(""), 0644)

	got := countFileLines(path)
	if got != 0 {
		t.Errorf("countFileLines empty = %d, want 0", got)
	}
}

func TestCountFileLines_Missing(t *testing.T) {
	got := countFileLines("/nonexistent/file.txt")
	if got != 0 {
		t.Errorf("countFileLines missing file = %d, want 0", got)
	}
}
