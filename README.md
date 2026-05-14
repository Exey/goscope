# 🔬 goscope

**CLI tool for Go backend codebase intelligence** — analyze microservices, generate dependency graphs, detect tech stack, and produce an interactive HTML report.

Built in pure Go with zero external dependencies.

---

## ⚡ Generate Report in 10 Seconds

```bash
cd goscope
go run ./cmd/goscope ~/backend --open
```

Where `~/backend` is the **parent folder** where all your backend service repositories are cloned:

```text
~/backend/
├── api-gateway/      ← cloned repo (.git inside)
├── user-service/     ← cloned repo
├── payment-service/  ← cloned repo
├── proto/            ← shared proto definitions
├── auth-service/     ← cloned repo
└── docker-compose.yml
```

Services are auto-detected up to 3 levels deep, so layouts like `src/<service>/` or `services/<service>/` also work. Non-Go services (Python, Java, PHP, etc.) are detected and shown in the report with language badges.

![Result](https://i.postimg.cc/TTfttRWk/goscope.png)

---

### What the Report Contains

1. **📊 Summary** — microservice count, Go files, lines of code, declarations by type (structs, interfaces, enums, functions), proto files, gRPC services. Non-Go services detected in the repo tree get line count cards per language (Python, Java, etc.)

2. **🐙 Git Analysis** — three sub-sections pulled from each cloned repo's `.git` independently:
   - **👥 Team Contribution Map** — per-developer: files modified, commit count, LOC per commit, first/last change date, and top-3 microservices worked on
   - **🔥 Code Churn** — most frequently modified files across all repos, with change count and top authors
   - **📐 Semantic Standards** — semver tag adoption rate (with latest tag), conventional commit coverage with a type breakdown (`feat`, `fix`, `chore`, `refactor`, `docs`, `test`, …), and samples of non-standard commit messages

3. **🏛️ Architecture** — four-column layout plus an interactive graph:
   - **Layers** — detected architectural layers (API, service, repository, etc.) with file counts and a proportional bar
   - **Components** — identified Go components (HTTP server, gRPC server, message queue consumer, etc.)
   - **Technologies** — auto-detected from Go imports (`pgx` → PostgreSQL, `sarama` → Kafka, etc.), `go.mod`, `docker-compose.yml`, and `Makefile`. Non-Go languages shown with orange badges
   - **Microservices** — clickable grid of all services including non-Go ones with language/LOC badges
   - **Architecture Graph** — interactive force-directed graph connecting microservices to their technologies

4. **🔗 Microservices Penetration** — which microservice is imported by the most other microservices, plus TODO/FIXME density per microservice

5. **🔥 Hot Zones** — top 10 most interconnected files by PageRank dependency score, with clickable microservice badges

6. **📏 Longest Functions** — ranked list of functions by line count, with clickable microservice badges

7. **⚠️ Anti-patterns** — static analysis across the codebase with 22 Go-specific checks grouped by severity. Passed checks shown in a compact 3-column grid; failed checks listed with file locations, code snippets, and git-blame author attribution. Protobuf-generated files (`.pb.go`) are excluded automatically. Checks include:
   - **HIGH** — hardcoded secrets, SQL injection via string concatenation, `math/rand` for security, `panic()` in business logic, unsafe type assertions, unclosed HTTP response bodies, loop variable capture in goroutines, copying `sync.Mutex`
   - **MEDIUM** — error not wrapped with `%w`, defer inside loops, missing `rows.Err()` / `rows.Close()`, `time.Sleep` for goroutine sync
   - **LOW** — large channel buffers, naked returns, pointer-to-interface, missing slice pre-allocation, package underscore naming, `init()` functions, `fmt.Sprintf` for integer conversion, `[]byte` conversion in loops

8. **🔧 Microservices** — detailed breakdown of each microservice (starting with API Gateway, then Proto, then by size):
   - Complete file inventory sorted by lines of code
   - Declaration statistics (structs, interfaces, enums, funcs, gRPC services/RPCs)
   - Interactive force-directed dependency graph per microservice (includes big functions ≥50 lines)

---

## 🚀 Quick Start

```bash
cd goscope

# Build
go build -o goscope ./cmd/goscope

# Analyze a Go backend (point to the parent folder with all repos)
./goscope ~/backend

# See help
./goscope --help
```

---

## 🏗️ Build & Install

### Option 1: Go Run (Recommended for first try)

```bash
go run ./cmd/goscope ~/backend --open
```

### Option 2: Build Binary

```bash
go build -o goscope ./cmd/goscope
./goscope ~/backend --open
```

### Option 3: Install System-Wide

```bash
go build -o goscope ./cmd/goscope
sudo mv goscope /usr/local/bin/
goscope ~/backend --open
```

---

## ⚙️ Configuration

Create `.goscope.json` in your project root (or run `goscope init`):

```json
{
  "excludePaths": [".git", "node_modules", "vendor", "dist", "build", ".idea"],
  "maxFilesAnalyze": 50000,
  "gitCommitLimit": 1000,
  "enableCache": false,
  "enableParallel": true,
  "hotspotCount": 15,
  "fileExtensions": ["go", "proto"]
}
```

---

## 📁 Project Structure

```text
goscope/
├── go.mod
├── cmd/goscope/
│   └── main.go                  # CLI entry point
├── internal/
│   ├── config/
│   │   └── config.go            # Config models + loader
│   ├── scanner/
│   │   ├── scanner.go           # Directory walker, scan orchestration
│   │   ├── detect.go            # Service detection, microservice inference
│   │   ├── techdetect.go        # Technology detection (docker-compose, go.mod, Makefile)
│   │   └── scanner_test.go
│   ├── parser/
│   │   ├── models.go            # ParsedFile, Declaration, GitMetadata
│   │   ├── parser.go            # Go + Proto file parsers
│   │   └── parser_test.go
│   ├── git/
│   │   └── analyzer.go          # Multi-repo batch git log analysis
│   ├── graph/
│   │   ├── graph.go             # Dependency graph + PageRank
│   │   ├── util.go              # File helpers
│   │   └── graph_test.go
│   └── report/
│       ├── report.go            # HTML report generator (Generate)
│       ├── antipatterns.go      # 22 Go anti-pattern checks + HTML builder
│       ├── graphs.go            # Architecture + declaration graph builders
│       ├── helpers.go           # Formatting, escaping, tech detection
│       └── helpers_test.go
└── README.md
```

---

## 🧪 Testing

```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run specific package tests
go test -v ./internal/parser/
go test -v ./internal/scanner/
go test -v ./internal/graph/
go test -v ./internal/report/

# Run a specific test
go test -v -run TestMatchGoTypeRef ./internal/report/
```

---

## Requirements

- **Go 1.22+** (uses standard library only — no external dependencies)
- **git** (for repository history analysis)
