package scanner

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goscope/internal/config"
)

// ForeignService represents a non-Go microservice detected in the repo tree.
type ForeignService struct {
	Name      string
	Language  string // "Python", "Java", "C#", etc.
	Path      string
	LineCount int
	FileCount int
}

// ScanResult holds the scanned files grouped by microservice.
type ScanResult struct {
	Files           []string            // all Go/proto file paths
	Microservices   map[string][]string // microservice name -> Go/proto file paths
	RootSubdirs     []string            // first-level subdirectories of the root
	GitRepos        []string            // paths to directories containing .git
	ForeignServices []ForeignService    // non-Go services detected
	ServicesRoot    string              // detected services root dir (e.g. "src", "services", or "")
}

// serviceContainerDirs are directory names that typically hold microservices inside them.
var serviceContainerDirs = map[string]bool{
	"src": true, "services": true, "service": true, "apps": true,
	"microservices": true, "svc": true, "cmd": true, "modules": true,
	"components": true, "backend": true, "packages": true, "deploy": false,
	"projects": true, "server": true, "servers": true,
}

// serviceMarkers are files/dirs that signal a directory is a microservice.
var serviceMarkers = []string{
	"Dockerfile", "go.mod", "main.go", "package.json", "requirements.txt",
	"setup.py", "pyproject.toml", "pom.xml", "build.gradle", "build.gradle.kts",
	"Cargo.toml", "composer.json", "Gemfile", "mix.exs", "CMakeLists.txt",
	"Makefile", ".csproj", "Program.cs",
}

// langExtensions maps file extensions to programming languages.
var langExtensions = map[string]string{
	".py":    "Python",
	".java":  "Java",
	".kt":    "Kotlin",
	".scala": "Scala",
	".php":   "PHP",
	".rb":    "Ruby",
	".rs":    "Rust",
	".cs":    "C#",
	".ts":    "TypeScript",
	".js":    "JavaScript",
	".c":     "C",
	".cpp":   "C++",
	".cc":    "C++",
	".h":     "C/C++ Header",
	".hpp":   "C++",
	".ex":    "Elixir",
	".exs":   "Elixir",
	".swift": "Swift",
	".dart":  "Dart",
}

// Scan walks the directory tree looking for Go/proto files and foreign services.
func Scan(rootPath string, cfg config.Config) (*ScanResult, error) {
	rootPath, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(rootPath)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, err
	}

	excludeSet := make(map[string]bool)
	for _, p := range cfg.ExcludePaths {
		excludeSet[p] = true
	}
	extSet := make(map[string]bool)
	for _, ext := range cfg.FileExtensions {
		extSet["."+ext] = true
	}

	result := &ScanResult{Microservices: make(map[string][]string)}

	// ── Phase 1: Discover service directories (up to 3 levels deep) ──
	serviceDirs := discoverServiceDirs(rootPath, excludeSet)

	// Determine services root for CLI output
	if len(serviceDirs) > 0 {
		result.ServicesRoot = detectServicesRoot(rootPath, serviceDirs)
	}

	// Collect root-level subdirs and find .git repos
	entries, _ := os.ReadDir(rootPath)
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && !excludeSet[e.Name()] {
			result.RootSubdirs = append(result.RootSubdirs, e.Name())
			gitDir := filepath.Join(rootPath, e.Name(), ".git")
			if _, err := os.Stat(gitDir); err == nil {
				result.GitRepos = append(result.GitRepos, filepath.Join(rootPath, e.Name()))
			}
		}
	}
	sort.Strings(result.RootSubdirs)

	// Also check for .git in discovered service dirs (e.g. src/<service>/.git)
	for _, sd := range serviceDirs {
		gitDir := filepath.Join(sd, ".git")
		if _, err := os.Stat(gitDir); err == nil {
			alreadyHave := false
			for _, r := range result.GitRepos {
				if r == sd {
					alreadyHave = true
					break
				}
			}
			if !alreadyHave {
				result.GitRepos = append(result.GitRepos, sd)
			}
		}
	}

	// Also check root-level .git
	if _, err := os.Stat(filepath.Join(rootPath, ".git")); err == nil {
		hasRoot := false
		for _, r := range result.GitRepos {
			if r == rootPath {
				hasRoot = true
				break
			}
		}
		if !hasRoot {
			result.GitRepos = append([]string{rootPath}, result.GitRepos...)
		}
	}

	// ── Phase 2: Walk and collect Go/proto files + detect foreign services ──
	foreignStats := make(map[string]*ForeignService) // service dir path -> stats

	err = filepath.WalkDir(rootPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() && strings.HasPrefix(name, ".") {
			return filepath.SkipDir
		}
		if d.IsDir() && excludeSet[name] {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(name))

		// Go/proto files
		if extSet[ext] {
			result.Files = append(result.Files, path)
			ms := detectMicroservice(rootPath, path, serviceDirs)
			result.Microservices[ms] = append(result.Microservices[ms], path)
			return nil
		}

		// Foreign language files — count lines per service dir
		if lang, ok := langExtensions[ext]; ok {
			svcDir := findServiceDir(rootPath, path, serviceDirs)
			if svcDir == "" {
				return nil
			}
			svcName := filepath.Base(svcDir)
			key := svcDir
			fs, exists := foreignStats[key]
			if !exists {
				fs = &ForeignService{Name: svcName, Language: lang, Path: svcDir}
				foreignStats[key] = fs
			}
			fs.FileCount++
			lc := countFileLines(path)
			fs.LineCount += lc
		}

		return nil
	})

	// Filter foreign services: only keep dirs that have NO Go files (pure foreign)
	for key, fs := range foreignStats {
		if _, hasGo := result.Microservices[fs.Name]; hasGo {
			continue // This service has Go files, skip as foreign
		}
		// Also skip if very few files (probably not a real service)
		if fs.FileCount < 2 {
			continue
		}
		_ = key
		result.ForeignServices = append(result.ForeignServices, *fs)
	}
	sort.Slice(result.ForeignServices, func(i, j int) bool {
		return result.ForeignServices[i].LineCount > result.ForeignServices[j].LineCount
	})

	// ── Phase 3: Filter out microservices with no real content ──
	for ms, files := range result.Microservices {
		if ms == "root" && len(files) <= 1 {
			// Keep root only if it has real files
			continue
		}
		// Keep all detected microservices that have files
	}

	return result, err
}

// discoverServiceDirs finds directories that look like microservices.
// Searches up to 3 levels deep from root.
