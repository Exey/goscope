package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/goscope/internal/config"
	gitpkg "github.com/goscope/internal/git"
	"github.com/goscope/internal/graph"
	"github.com/goscope/internal/parser"
	"github.com/goscope/internal/report"
	"github.com/goscope/internal/scanner"
)

const version = "1.0.0"

func main() {
	if len(os.Args) < 2 {
		runAnalyze(".", false, false)
		return
	}

	switch os.Args[1] {
	case "analyze":
		path, verbose, open := parseAnalyzeArgs(os.Args[2:])
		runAnalyze(path, verbose, open)
	case "init":
		runInit()
	case "--version", "-v":
		fmt.Printf("goscope %s\n", version)
	case "--help", "-h":
		printHelp()
	default:
		// Treat everything as: goscope [path] [--open] [--verbose]
		path, verbose, open := parseAnalyzeArgs(os.Args[1:])
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			runAnalyze(path, verbose, open)
		} else {
			fmt.Printf("Unknown command or invalid path: %s\n", os.Args[1])
			printHelp()
			os.Exit(1)
		}
	}
}

// parseAnalyzeArgs extracts path, --verbose, --open from args in any order.
func parseAnalyzeArgs(args []string) (path string, verbose, open bool) {
	path = "."
	for _, arg := range args {
		switch arg {
		case "--open", "-open":
			open = true
		case "--verbose", "-verbose":
			verbose = true
		default:
			if !strings.HasPrefix(arg, "-") {
				path = arg
			}
		}
	}
	return
}

func printHelp() {
	fmt.Println(`🔬 goscope — Go Backend Codebase Intelligence Tool

Usage:
  goscope [path]                  Analyze the given path (default: current directory)
  goscope analyze [path] [flags]  Analyze a Go codebase and generate a report
  goscope init                    Create default .goscope.json config

Path should point to the parent folder where all backend repos are cloned.

Flags:
  --verbose    Enable verbose logging
  --open       Open report in browser after generation
  --version    Show version
  --help       Show this help`)
}

func runInit() {
	if _, err := os.Stat(config.DefaultConfigPath); err == nil {
		fmt.Printf("⚠️  Config already exists at %s\n", config.DefaultConfigPath)
		return
	}
	if err := config.CreateDefault(""); err != nil {
		fmt.Printf("❌ Failed to create config: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("\n📋 Next steps:")
	fmt.Println("   1. Edit .goscope.json to customize settings")
	fmt.Println("   2. Run: goscope analyze /path/to/backend-repos")
}

func runAnalyze(path string, verbose, openBrowser bool) {
	fmt.Printf("🚀 Starting goscope analysis for: %s\n", path)
	startTime := time.Now()

	cfg := config.Load("")

	absPath, err := filepath.Abs(path)
	if err != nil {
		fmt.Printf("❌ Invalid path: %v\n", err)
		os.Exit(1)
	}

	// ── 1. Scan ──
	fmt.Println("📂 Scanning repositories...")
	scanResult, err := scanner.Scan(absPath, cfg)
	if err != nil {
		fmt.Printf("❌ Scan failed: %v\n", err)
		os.Exit(1)
	}
	if len(scanResult.Files) == 0 {
		fmt.Println("❌ No source files found.")
		os.Exit(1)
	}
	if len(scanResult.Files) > cfg.MaxFilesAnalyze {
		fmt.Printf("❌ Too many files (%d). Limit: %d\n", len(scanResult.Files), cfg.MaxFilesAnalyze)
		os.Exit(1)
	}
	fmt.Printf("   Found %d files across %d microservices\n", len(scanResult.Files), len(scanResult.Microservices))
	if len(scanResult.RootSubdirs) > 0 {
		fmt.Printf("   📁 Root subdirs: %s\n", joinStr(scanResult.RootSubdirs))
	}
	if len(scanResult.GitRepos) > 0 {
		fmt.Printf("   🔀 Git repos found: %d\n", len(scanResult.GitRepos))
	}

	// Detect technologies from docker-compose + go.mod + Makefile
	dockerServices, technologies := scanner.ScanDockerCompose(absPath)
	if len(technologies) > 0 {
		fmt.Printf("   🐳 Technologies detected: %s\n", joinStr(technologies))
	}

	// ── 2. Parse ──
	fmt.Printf("📦 Parsing %d files...\n", len(scanResult.Files))

	fileMSLookup := make(map[string]string)
	for ms, msf := range scanResult.Microservices {
		for _, f := range msf {
			fileMSLookup[f] = ms
		}
	}

	var parsedFiles []*parser.ParsedFile
	if cfg.EnableParallel {
		parsedFiles = parseParallel(scanResult, fileMSLookup, verbose)
	} else {
		for _, filePath := range scanResult.Files {
			ms := fileMSLookup[filePath]
			if ms == "" {
				ms = "root"
			}
			pf, err := parser.ParseFile(filePath, ms)
			if err != nil && verbose {
				fmt.Printf("   ⚠️  Failed to parse %s: %v\n", filepath.Base(filePath), err)
			}
			if pf != nil {
				parsedFiles = append(parsedFiles, pf)
			}
		}
	}
	fmt.Printf("   Parsed %d files\n", len(parsedFiles))

	msNames := make(map[string]bool)
	for _, f := range parsedFiles {
		if f.MicroserviceName != "" {
			msNames[f.MicroserviceName] = true
		}
	}
	if len(msNames) > 0 {
		var names []string
		for n := range msNames {
			names = append(names, n)
		}
		sort.Strings(names)
		fmt.Printf("   🔧 Detected microservices: %s\n", joinStr(names))
	}

	// ── 3. Git (multi-repo) ──
	fmt.Println("📜 Analyzing Git history...")
	gitRepos := scanResult.GitRepos
	if len(gitRepos) == 0 {
		fmt.Println("   ⚠️  No .git directories found in subdirectories.")
	}

	branchName := ""
	if len(gitRepos) > 0 {
		a := gitpkg.NewAnalyzer(gitRepos[0], cfg.GitCommitLimit)
		branchName = a.CurrentBranch()
	}
	if branchName == "" {
		branchName = "—"
	}

	authorStats := gitpkg.GetAuthorStatsMultiRepo(gitRepos, cfg.GitCommitLimit)
	if authorStats == nil {
		authorStats = make(map[string]*gitpkg.AuthorStats)
	}
	gitpkg.EnrichFilesMultiRepo(gitRepos, cfg.GitCommitLimit, parsedFiles, authorStats)

	// ── 4. Graph ──
	fmt.Println("🕸️  Building dependency graph...")
	g := graph.New()
	g.Build(parsedFiles)
	g.Analyze()
	fmt.Printf("   Graph: %d nodes, %d edges\n", len(g.Vertices), len(g.Edges))

	hotspots := g.GetTopHotspots(5)
	if len(hotspots) > 0 {
		fmt.Println("\n🗺️  Your Codebase Map")
		fmt.Printf("├─ 🔥 Hot Zones (Top %d):\n", len(hotspots))
		for i, h := range hotspots {
			prefix := "│   ├─"
			if i == len(hotspots)-1 {
				prefix = "│   └─"
			}
			fmt.Printf("%s %s (%.4f)\n", prefix, filepath.Base(h.Path), h.Score)
		}
	}

	// ── 5. Report ──
	fmt.Println("\n📊 Generating report...")
	outputDir := "output"
	os.MkdirAll(outputDir, 0755)
	reportPath := filepath.Join(outputDir, "index.html")

	projectName := filepath.Base(absPath)
	if err := report.Generate(g, reportPath, parsedFiles, branchName, authorStats,
		projectName, technologies, dockerServices, scanResult.RootSubdirs); err != nil {
		fmt.Printf("❌ Report generation failed: %v\n", err)
		os.Exit(1)
	}

	absReport, _ := filepath.Abs(reportPath)
	fmt.Printf("✅ Report: %s\n", absReport)

	elapsed := time.Since(startTime)
	fmt.Printf("\n✨ Complete in %s\n", formatDuration(elapsed))

	if openBrowser {
		openInBrowser(absReport)
	}
}

func parseParallel(scanResult *scanner.ScanResult, fileMSLookup map[string]string, verbose bool) []*parser.ParsedFile {
	numWorkers := runtime.NumCPU()
	if numWorkers > 8 {
		numWorkers = 8
	}

	jobs := make(chan string, len(scanResult.Files))
	results := make(chan *parser.ParsedFile, len(scanResult.Files))

	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for filePath := range jobs {
				ms := fileMSLookup[filePath]
				if ms == "" {
					ms = "root"
				}
				pf, err := parser.ParseFile(filePath, ms)
				if err != nil && verbose {
					fmt.Printf("   ⚠️  %s: %v\n", filepath.Base(filePath), err)
				}
				if pf != nil {
					results <- pf
				}
			}
		}()
	}

	for _, f := range scanResult.Files {
		jobs <- f
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	var parsed []*parser.ParsedFile
	for pf := range results {
		parsed = append(parsed, pf)
	}
	return parsed
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm %ds", m, s)
}

func openInBrowser(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "linux":
		cmd = exec.Command("xdg-open", path)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	}
	if cmd != nil {
		cmd.Start()
	}
}

func joinStr(s []string) string {
	result := ""
	for i, v := range s {
		if i > 0 {
			result += ", "
		}
		result += v
	}
	return result
}
