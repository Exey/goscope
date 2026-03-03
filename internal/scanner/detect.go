package scanner

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

func discoverServiceDirs(rootPath string, excludeSet map[string]bool) []string {
	var dirs []string

	// Level 1: direct children
	l1, _ := os.ReadDir(rootPath)
	for _, e := range l1 {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || excludeSet[e.Name()] {
			continue
		}
		l1Path := filepath.Join(rootPath, e.Name())

		if isServiceDir(l1Path) {
			dirs = append(dirs, l1Path)
			continue
		}

		// Level 2: if l1 is a service container, check children
		if serviceContainerDirs[strings.ToLower(e.Name())] {
			l2, _ := os.ReadDir(l1Path)
			for _, e2 := range l2 {
				if !e2.IsDir() || strings.HasPrefix(e2.Name(), ".") || excludeSet[e2.Name()] {
					continue
				}
				l2Path := filepath.Join(l1Path, e2.Name())
				if isServiceDir(l2Path) {
					dirs = append(dirs, l2Path)
				}
			}
			continue
		}

		// Level 2: check children for service containers
		l2, _ := os.ReadDir(l1Path)
		for _, e2 := range l2 {
			if !e2.IsDir() || strings.HasPrefix(e2.Name(), ".") || excludeSet[e2.Name()] {
				continue
			}
			l2Path := filepath.Join(l1Path, e2.Name())

			if serviceContainerDirs[strings.ToLower(e2.Name())] {
				// Level 3: children of the container
				l3, _ := os.ReadDir(l2Path)
				for _, e3 := range l3 {
					if !e3.IsDir() || strings.HasPrefix(e3.Name(), ".") || excludeSet[e3.Name()] {
						continue
					}
					l3Path := filepath.Join(l2Path, e3.Name())
					if isServiceDir(l3Path) {
						dirs = append(dirs, l3Path)
					}
				}
			}
		}
	}

	return dirs
}

// isServiceDir checks if a directory looks like a microservice.
func isServiceDir(dir string) bool {
	for _, marker := range serviceMarkers {
		// Check both exact file and glob pattern (.csproj)
		if strings.HasPrefix(marker, ".") {
			// Glob for *.csproj etc
			matches, _ := filepath.Glob(filepath.Join(dir, "*"+marker))
			if len(matches) > 0 {
				return true
			}
		} else {
			if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
				return true
			}
		}
	}
	// Check for .git
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return true
	}
	return false
}

// detectServicesRoot finds the common parent directory pattern for services.
func detectServicesRoot(rootPath string, serviceDirs []string) string {
	parents := make(map[string]int)
	for _, sd := range serviceDirs {
		rel, _ := filepath.Rel(rootPath, sd)
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) >= 2 {
			parents[parts[0]]++
		}
	}
	best := ""
	bestCount := 0
	for p, c := range parents {
		if c > bestCount {
			best = p
			bestCount = c
		}
	}
	if bestCount >= 2 {
		return best
	}
	return ""
}

// detectMicroservice infers the microservice name from the file path.
func detectMicroservice(rootPath, filePath string, serviceDirs []string) string {
	// Check if file belongs to a discovered service dir
	for _, sd := range serviceDirs {
		if strings.HasPrefix(filePath, sd+string(filepath.Separator)) || filePath == sd {
			return filepath.Base(sd)
		}
	}

	rel, _ := filepath.Rel(rootPath, filePath)
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 2 {
		return "root"
	}

	// Multi-repo: first-level dir with markers is a repo
	firstDir := filepath.Join(rootPath, parts[0])
	for _, marker := range []string{".git", "go.mod", "Dockerfile", "Makefile", "docker-compose.yml"} {
		if _, err := os.Stat(filepath.Join(firstDir, marker)); err == nil {
			return parts[0]
		}
	}

	for i, p := range parts {
		if p == "cmd" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	knownContainers := map[string]bool{"services": true, "service": true, "apps": true, "microservices": true, "svc": true}
	for i, p := range parts {
		if knownContainers[p] && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	for i, p := range parts {
		if (p == "proto" || p == "api" || p == "pkg") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	for i, p := range parts {
		if p == "internal" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return parts[0]
}

// findServiceDir finds which service directory a file belongs to.
func findServiceDir(rootPath, filePath string, serviceDirs []string) string {
	for _, sd := range serviceDirs {
		if strings.HasPrefix(filePath, sd+string(filepath.Separator)) {
			return sd
		}
	}
	return ""
}

func countFileLines(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	n := 0
	s := bufio.NewScanner(f)
	for s.Scan() {
		n++
	}
	return n
}

