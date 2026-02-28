package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/goscope/internal/parser"
)

// AuthorStats holds repo-wide author statistics.
type AuthorStats struct {
	FilesModified      int
	TotalCommits       int
	FirstCommit        float64
	LastCommit         float64
	MicroserviceCounts map[string]int
}

// Analyzer performs git history analysis.
type Analyzer struct {
	RepoPath    string
	CommitLimit int
}

func NewAnalyzer(repoPath string, commitLimit int) *Analyzer {
	return &Analyzer{RepoPath: repoPath, CommitLimit: commitLimit}
}

func (a *Analyzer) CurrentBranch() string {
	out := a.git(a.RepoPath, "rev-parse", "--abbrev-ref", "HEAD")
	return strings.TrimSpace(out)
}

// GetAuthorStatsMultiRepo collects author stats from multiple git repos.
func GetAuthorStatsMultiRepo(gitRepos []string, commitLimit int) map[string]*AuthorStats {
	stats := make(map[string]*AuthorStats)
	for _, repo := range gitRepos {
		a := &Analyzer{RepoPath: repo, CommitLimit: commitLimit}
		out := a.git(repo, "log", fmt.Sprintf("-%d", commitLimit), "--pretty=format:%an\t%at")
		if out == "" {
			continue
		}
		for _, line := range strings.Split(out, "\n") {
			parts := strings.SplitN(line, "\t", 2)
			if len(parts) < 2 {
				continue
			}
			author := parts[0]
			ts, _ := strconv.ParseFloat(parts[1], 64)
			if ts <= 0 {
				continue
			}
			s, ok := stats[author]
			if !ok {
				s = &AuthorStats{MicroserviceCounts: make(map[string]int)}
				stats[author] = s
			}
			s.TotalCommits++
			if s.FirstCommit == 0 || ts < s.FirstCommit {
				s.FirstCommit = ts
			}
			if ts > s.LastCommit {
				s.LastCommit = ts
			}
		}
	}
	return stats
}

type fileStats struct {
	changeCount     int
	lastModified    float64
	firstCommitDate float64
	authorCounts    map[string]int
	messages        []string
}

// EnrichFilesMultiRepo enriches files using git logs from multiple repos.
func EnrichFilesMultiRepo(gitRepos []string, commitLimit int, files []*parser.ParsedFile, authorStats map[string]*AuthorStats) {
	// Build a merged batch from all repos
	allBatch := make(map[string]*fileStats)

	for _, repo := range gitRepos {
		a := &Analyzer{RepoPath: repo, CommitLimit: commitLimit}
		batch := a.batchCollectFileStats()
		for relPath, fs := range batch {
			// Convert relative path to absolute for matching
			absPath := filepath.Join(repo, relPath)
			allBatch[absPath] = fs
			// Also store relative in case files use different base
			allBatch[relPath] = fs
		}
	}

	fmt.Printf("   Batch git log parsed (%d file entries from %d repos)\n", len(allBatch), len(gitRepos))

	for _, file := range files {
		var fs *fileStats
		// Try absolute path first
		if f, ok := allBatch[file.FilePath]; ok {
			fs = f
		}
		if fs == nil {
			continue
		}

		type ac struct {
			name  string
			count int
		}
		var acs []ac
		for name, cnt := range fs.authorCounts {
			acs = append(acs, ac{name, cnt})
		}
		sort.Slice(acs, func(i, j int) bool { return acs[i].count > acs[j].count })
		var topAuthors []string
		for i, a := range acs {
			if i >= 3 {
				break
			}
			topAuthors = append(topAuthors, a.name)
		}

		file.GitMeta = parser.GitMetadata{
			LastModified:    fs.lastModified,
			ChangeFrequency: fs.changeCount,
			TopAuthors:      topAuthors,
			RecentMessages:  fs.messages,
			FirstCommitDate: fs.firstCommitDate,
		}

		for _, author := range topAuthors {
			if s, ok := authorStats[author]; ok {
				s.FilesModified++
				if file.MicroserviceName != "" {
					s.MicroserviceCounts[file.MicroserviceName]++
				}
			}
		}
	}
}

func (a *Analyzer) batchCollectFileStats() map[string]*fileStats {
	cmd := exec.Command("git", "log",
		fmt.Sprintf("-%d", a.CommitLimit),
		"--pretty=format:__COMMIT__%n%an%n%at%n%s",
		"--name-only",
	)
	cmd.Dir = a.RepoPath
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &bytes.Buffer{}
	if err := cmd.Run(); err != nil {
		return nil
	}

	stats := make(map[string]*fileStats)
	blocks := strings.Split(out.String(), "__COMMIT__\n")
	for _, block := range blocks {
		if block == "" {
			continue
		}
		lines := strings.Split(block, "\n")
		if len(lines) < 3 {
			continue
		}
		author := lines[0]
		ts, _ := strconv.ParseFloat(lines[1], 64)
		message := lines[2]

		for _, fileLine := range lines[3:] {
			trimmed := strings.TrimSpace(fileLine)
			if trimmed == "" {
				continue
			}
			fs, ok := stats[trimmed]
			if !ok {
				fs = &fileStats{authorCounts: make(map[string]int)}
				stats[trimmed] = fs
			}
			fs.changeCount++
			if ts > fs.lastModified {
				fs.lastModified = ts
			}
			if fs.firstCommitDate == 0 || (ts > 0 && ts < fs.firstCommitDate) {
				fs.firstCommitDate = ts
			}
			fs.authorCounts[author]++
			if len(fs.messages) < 5 {
				fs.messages = append(fs.messages, message)
			}
		}
	}
	return stats
}

func (a *Analyzer) git(dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &bytes.Buffer{}
	if err := cmd.Run(); err != nil {
		return ""
	}
	return out.String()
}
