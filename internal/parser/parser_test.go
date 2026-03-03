package parser

import (
	"os"
	"path/filepath"
	"testing"
)

func tmpFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseGoFile_Basic(t *testing.T) {
	src := `package myservice

import (
	"fmt"
	"github.com/jackc/pgx/v5"
)

// UserService handles user operations.
type UserService struct {
	db *pgx.Pool
}

type UserRepository interface {
	GetByID(id string) error
}

func NewUserService() *UserService {
	return &UserService{}
}

func (s *UserService) GetUser() {
	fmt.Println("hello")
}
`
	path := tmpFile(t, "service.go", src)
	pf, err := ParseGoFile(path, "user-svc")
	if err != nil {
		t.Fatal(err)
	}

	if pf.PackageName != "myservice" {
		t.Errorf("PackageName = %q, want myservice", pf.PackageName)
	}
	if pf.MicroserviceName != "user-svc" {
		t.Errorf("MicroserviceName = %q, want user-svc", pf.MicroserviceName)
	}
	if pf.FileType != "go" {
		t.Errorf("FileType = %q, want go", pf.FileType)
	}
	if len(pf.Imports) != 2 {
		t.Errorf("Imports count = %d, want 2", len(pf.Imports))
	}

	kinds := map[DeclKind]int{}
	for _, d := range pf.Declarations {
		kinds[d.Kind]++
	}
	if kinds[DeclStruct] != 1 {
		t.Errorf("structs = %d, want 1", kinds[DeclStruct])
	}
	if kinds[DeclInterface] != 1 {
		t.Errorf("interfaces = %d, want 1", kinds[DeclInterface])
	}
	if kinds[DeclFunc] < 2 {
		t.Errorf("funcs = %d, want >= 2", kinds[DeclFunc])
	}
	if pf.Description != "UserService handles user operations." {
		t.Errorf("Description = %q", pf.Description)
	}
}

func TestParseGoFile_BigFunctions(t *testing.T) {
	// Create a file with a 30-line function
	lines := "package pkg\n\nfunc BigFunc() {\n"
	for i := 0; i < 28; i++ {
		lines += "\t_ = 1\n"
	}
	lines += "}\n\nfunc SmallFunc() {\n\t_ = 1\n}\n"

	path := tmpFile(t, "big.go", lines)
	pf, err := ParseGoFile(path, "test")
	if err != nil {
		t.Fatal(err)
	}
	if pf.LongestFunction == nil {
		t.Fatal("LongestFunction is nil")
	}
	if pf.LongestFunction.Name != "BigFunc" {
		t.Errorf("LongestFunction.Name = %q, want BigFunc", pf.LongestFunction.Name)
	}
	if pf.LongestFunction.LineCount < 25 {
		t.Errorf("LongestFunction.LineCount = %d, want >= 25", pf.LongestFunction.LineCount)
	}
	if len(pf.BigFunctions) == 0 {
		t.Error("BigFunctions is empty, want at least 1")
	}
	found := false
	for _, bf := range pf.BigFunctions {
		if bf.Name == "BigFunc" {
			found = true
		}
	}
	if !found {
		t.Error("BigFunc not found in BigFunctions")
	}
}

func TestParseGoFile_TodoFixme(t *testing.T) {
	src := "package x\n// TODO fix this\n// FIXME broken\n// TODO another\n"
	path := tmpFile(t, "todo.go", src)
	pf, err := ParseGoFile(path, "test")
	if err != nil {
		t.Fatal(err)
	}
	if pf.TodoCount != 2 {
		t.Errorf("TodoCount = %d, want 2", pf.TodoCount)
	}
	if pf.FixmeCount != 1 {
		t.Errorf("FixmeCount = %d, want 1", pf.FixmeCount)
	}
}

func TestParseProtoFile(t *testing.T) {
	src := `syntax = "proto3";
package myapi;

import "google/protobuf/timestamp.proto";

message GetUserRequest {
  string user_id = 1;
}

message GetUserResponse {
  string name = 1;
}

service UserService {
  rpc GetUser(GetUserRequest) returns (GetUserResponse);
  rpc DeleteUser(GetUserRequest) returns (GetUserResponse);
}

enum UserRole {
  ADMIN = 0;
  USER = 1;
}
`
	path := tmpFile(t, "user.proto", src)
	pf, err := ParseProtoFile(path, "proto")
	if err != nil {
		t.Fatal(err)
	}
	if pf.FileType != "proto" {
		t.Errorf("FileType = %q, want proto", pf.FileType)
	}

	kinds := map[DeclKind]int{}
	for _, d := range pf.Declarations {
		kinds[d.Kind]++
	}
	if kinds[DeclMessage] != 2 {
		t.Errorf("messages = %d, want 2", kinds[DeclMessage])
	}
	if kinds[DeclService] != 1 {
		t.Errorf("services = %d, want 1", kinds[DeclService])
	}
	if kinds[DeclRPC] != 2 {
		t.Errorf("rpcs = %d, want 2", kinds[DeclRPC])
	}
	if kinds[DeclEnum] != 1 {
		t.Errorf("enums = %d, want 1", kinds[DeclEnum])
	}
}

func TestParseFile_DispatchesByExtension(t *testing.T) {
	goPath := tmpFile(t, "x.go", "package x\ntype Foo struct{}\n")
	pf, err := ParseFile(goPath, "ms")
	if err != nil {
		t.Fatal(err)
	}
	if pf.FileType != "go" {
		t.Errorf("ParseFile(.go) FileType = %q, want go", pf.FileType)
	}

	protoPath := tmpFile(t, "x.proto", "syntax = \"proto3\";\nmessage Foo {}\n")
	pf, err = ParseFile(protoPath, "ms")
	if err != nil {
		t.Fatal(err)
	}
	if pf.FileType != "proto" {
		t.Errorf("ParseFile(.proto) FileType = %q, want proto", pf.FileType)
	}
}
