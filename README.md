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

```
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

2. **👥 Team Contribution Map** — developer activity with files modified, commit counts, first/last change dates, and **top-3 microservices** per author. Git history is collected from each cloned repo's `.git` independently

3. **📚 Tech Stack** — three subsections:
   - **Technologies** — auto-detected from Go imports (`pgx` → PostgreSQL, `sarama` → Kafka, etc.), `go.mod` dependencies, `docker-compose.yml` images/ports, and `Makefile` hints. Non-Go languages shown with orange badges
   - **Microservices** — clickable grid of all detected microservices, including non-Go services with language badges
   - **Architecture** — interactive force-directed graph showing how microservices connect to technologies

4. **🔗 Microservices Penetration** — which microservice is imported by the most other microservices, plus TODO/FIXME density per microservice

5. **🔥 Hot Zones** — top 10 most interconnected files by PageRank dependency score, with clickable microservice badges

6. **📏 Longest Functions** — ranked list of functions by line count, with clickable microservice badges

7. **🔧 Microservices** — detailed breakdown of each microservice (starting with API Gateway, then Proto, then by size):
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

```
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
