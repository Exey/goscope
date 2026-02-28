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

That's it. Go compiles automatically, then opens the HTML report in your browser.

---

### What the Report Contains

1. **📊 Summary** — microservice count, Go files, proto files, lines of code, and declarations by type (structs, interfaces, enums, functions, messages, services, RPCs)

2. **👥 Team Contribution Map** — developer activity with files modified, commit counts, first/last change dates, and **top-3 microservices** per author. Git history is collected from each cloned repo's `.git` independently

3. **📚 Stack** — three subsections:
   - **Technologies** — auto-detected from Go imports (`pgx` → PostgreSQL, `sarama` → Kafka, etc.), `go.mod` dependencies, `docker-compose.yml` images/ports, and `Makefile` hints
   - **Architecture** — interactive force-directed graph showing how major microservices connect to technologies
   - **Microservices** — clickable grid of all detected microservices; top 8 by code size (or all ≥ 8K lines) are highlighted with a border

4. **📋 Microservices Penetration** — package penetration analysis (which Go packages are imported across the most microservices) plus TODO/FIXME density per microservice

5. **📏 Longest Functions** — ranked list of functions by line count, with clickable microservice badges

6. **🔧 Microservices** — detailed breakdown of each microservice (starting with API Gateway, then Proto, then by size):
   - Complete file inventory sorted by lines of code
   - Declaration statistics (structs, interfaces, enums, funcs, proto messages/services/RPCs)
   - Interactive force-directed dependency graph per microservice

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
│   └── main.go              # CLI entry point
├── internal/
│   ├── config/
│   │   └── config.go        # Config models + loader
│   ├── scanner/
│   │   └── scanner.go       # Directory walker, microservice detection, tech scanning
│   ├── parser/
│   │   ├── models.go        # ParsedFile, Declaration, GitMetadata
│   │   └── parser.go        # Go + Proto parsers
│   ├── git/
│   │   └── analyzer.go      # Multi-repo batch git log analysis
│   ├── graph/
│   │   ├── graph.go         # Dependency graph + PageRank
│   │   └── util.go          # File helpers
│   └── report/
│       └── report.go        # HTML report generator
└── README.md
```

---

## Requirements

- **Go 1.22+** (uses standard library only — no external dependencies)
- **git** (for repository history analysis)
