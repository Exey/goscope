package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/goscope/internal/parser"
)

// AuthorStats holds repo-wide author statistics.
type AuthorStats struct {
	FilesModified      int
	TotalCommits       int
	FirstCommit        float64
	LastCommit         float64
	MicroserviceCounts map[string]int
	TotalLOCAdded      int
}

// FileChurnStat holds churn data for a single file.
type FileChurnStat struct {
	RelPath     string
	ChangeCount int
	TopAuthors  []string
}

// TagStats holds semver tag analysis.
type TagStats struct {
	TotalTags    int
	SemverTags   int
	LatestSemver string
	SemverList   []string
}

// CommitStats holds conventional-commit analysis.
type CommitStats struct {
	Total      int
	Typed      int
	TypeCounts map[string]int
	Samples    []string // sample non-conventional messages
}

// BranchInfo holds data for a single local branch.
type BranchInfo struct {
	Name         string
	LastActivity float64 // unix timestamp
	DaysInactive int
}

// BranchStats holds branch management metrics.
type BranchStats struct {
	TotalBranches      int
	StaleBranches      []BranchInfo
	StaleThresholdDays int
	AvgLifetimeDays    float64 // avg time from first to last commit in merged branches
	AvgTTMDays         float64 // avg time from first branch commit to merge
	AvgIntegDelayHours float64 // avg time from last branch commit to merge (review delay)
	MaxDepth           int     // max nesting depth inferred from branch names
	RollbackCount      int
	TotalMainCommits   int
	PeakCommitDay      string // day of week with the most commits
}

// GitSummary bundles all git data produced for a report.
type GitSummary struct {
	AuthorStats map[string]*AuthorStats
	Churn       []FileChurnStat
	Tags        TagStats
	Commits     CommitStats
	Repos       []string
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

// EnrichAuthorLOC populates TotalLOCAdded in existing AuthorStats entries via --numstat.
func EnrichAuthorLOC(gitRepos []string, commitLimit int, authorStats map[string]*AuthorStats) {
	for _, repo := range gitRepos {
		a := &Analyzer{RepoPath: repo, CommitLimit: commitLimit}
		out := a.git(repo, "log",
			fmt.Sprintf("-%d", commitLimit),
			"--pretty=format:__AUTHOR__%n%an",
			"--numstat",
		)
		if out == "" {
			continue
		}
		currentAuthor := ""
		for _, line := range strings.Split(out, "\n") {
			if line == "__AUTHOR__" {
				currentAuthor = ""
				continue
			}
			if currentAuthor == "" {
				currentAuthor = strings.TrimSpace(line)
				continue
			}
			parts := strings.Fields(line)
			if len(parts) < 2 {
				continue
			}
			added, err := strconv.Atoi(parts[0])
			if err != nil {
				continue
			}
			if s, ok := authorStats[currentAuthor]; ok {
				s.TotalLOCAdded += added
			}
		}
	}
}

// GetChurnStats returns the top N most-changed files across all repos.
func GetChurnStats(gitRepos []string, commitLimit, topN int) []FileChurnStat {
	type entry struct {
		changeCount  int
		authorCounts map[string]int
	}
	all := make(map[string]*entry)

	for _, repo := range gitRepos {
		a := &Analyzer{RepoPath: repo, CommitLimit: commitLimit}
		cmd := exec.Command("git", "log",
			fmt.Sprintf("-%d", commitLimit),
			"--pretty=format:__COMMIT__%n%an",
			"--name-only",
		)
		cmd.Dir = repo
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &bytes.Buffer{}
		if err := cmd.Run(); err != nil {
			continue
		}
		_ = a
		currentAuthor := ""
		for _, line := range strings.Split(buf.String(), "\n") {
			if line == "__COMMIT__" {
				currentAuthor = ""
				continue
			}
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			if currentAuthor == "" {
				currentAuthor = trimmed
				continue
			}
			absPath := filepath.Join(repo, trimmed)
			e, ok := all[absPath]
			if !ok {
				e = &entry{authorCounts: make(map[string]int)}
				all[absPath] = e
			}
			e.changeCount++
			if currentAuthor != "" {
				e.authorCounts[currentAuthor]++
			}
		}
	}

	type kv struct {
		path  string
		count int
	}
	var sorted []kv
	for path, e := range all {
		sorted = append(sorted, kv{path, e.changeCount})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].count > sorted[j].count })

	var result []FileChurnStat
	for i, kv := range sorted {
		if i >= topN {
			break
		}
		e := all[kv.path]
		type ac struct {
			name  string
			count int
		}
		var acs []ac
		for name, cnt := range e.authorCounts {
			acs = append(acs, ac{name, cnt})
		}
		sort.Slice(acs, func(i, j int) bool { return acs[i].count > acs[j].count })
		var top []string
		for j, a := range acs {
			if j >= 3 {
				break
			}
			top = append(top, a.name)
		}
		result = append(result, FileChurnStat{
			RelPath:     kv.path,
			ChangeCount: kv.count,
			TopAuthors:  top,
		})
	}
	return result
}

var semverRe = regexp.MustCompile(`^v?\d+\.\d+\.\d+`)

// GetTagStats analyzes git tags for semver compliance across all repos.
func GetTagStats(gitRepos []string) TagStats {
	seen := make(map[string]bool)
	var ts TagStats

	for _, repo := range gitRepos {
		a := &Analyzer{RepoPath: repo, CommitLimit: 0}
		out := a.git(repo, "tag")
		if out == "" {
			continue
		}
		for _, tag := range strings.Split(strings.TrimSpace(out), "\n") {
			tag = strings.TrimSpace(tag)
			if tag == "" || seen[tag] {
				continue
			}
			seen[tag] = true
			ts.TotalTags++
			if semverRe.MatchString(tag) {
				ts.SemverTags++
				ts.SemverList = append(ts.SemverList, tag)
			}
		}
	}

	// Find latest semver by sorting
	if len(ts.SemverList) > 0 {
		sort.Slice(ts.SemverList, func(i, j int) bool {
			return compareSemver(ts.SemverList[i], ts.SemverList[j]) > 0
		})
		ts.LatestSemver = ts.SemverList[0]
	}
	return ts
}

func compareSemver(a, b string) int {
	parse := func(s string) [3]int {
		s = strings.TrimPrefix(s, "v")
		parts := strings.SplitN(s, ".", 3)
		var nums [3]int
		for i, p := range parts {
			if i >= 3 {
				break
			}
			// strip pre-release suffix
			p = strings.FieldsFunc(p, func(r rune) bool { return r == '-' || r == '+' })[0]
			nums[i], _ = strconv.Atoi(p)
		}
		return nums
	}
	av, bv := parse(a), parse(b)
	for i := 0; i < 3; i++ {
		if av[i] != bv[i] {
			if av[i] > bv[i] {
				return 1
			}
			return -1
		}
	}
	return 0
}

var conventionalRe = regexp.MustCompile(`^(feat|fix|chore|refactor|docs|style|test|perf|ci|build|revert)(\(.+\))?!?:`)
var ticketRe = regexp.MustCompile(`(#\d+|[A-Z]+-\d+|GH-\d+)`)

// GetCommitMessageStats analyzes commit messages for conventional commit compliance.
func GetCommitMessageStats(gitRepos []string, commitLimit int) CommitStats {
	var cs CommitStats
	cs.TypeCounts = make(map[string]int)
	seen := make(map[string]bool)

	for _, repo := range gitRepos {
		a := &Analyzer{RepoPath: repo, CommitLimit: commitLimit}
		out := a.git(repo, "log",
			fmt.Sprintf("-%d", commitLimit),
			"--pretty=format:%H\t%s",
		)
		if out == "" {
			continue
		}
		for _, line := range strings.Split(out, "\n") {
			parts := strings.SplitN(line, "\t", 2)
			if len(parts) < 2 {
				continue
			}
			hash, msg := parts[0], parts[1]
			if seen[hash] {
				continue
			}
			seen[hash] = true
			cs.Total++

			if m := conventionalRe.FindStringSubmatch(msg); m != nil {
				cs.Typed++
				cs.TypeCounts[m[1]]++
			} else if ticketRe.MatchString(msg) {
				cs.Typed++
				cs.TypeCounts["ticket"]++
			} else {
				if len(cs.Samples) < 5 {
					cs.Samples = append(cs.Samples, msg)
				}
			}
		}
	}
	return cs
}

// BlameAuthors returns a map of line number (1-based) → author name for a file.
func BlameAuthors(gitRepos []string, absFilePath string) map[int]string {
	for _, repo := range gitRepos {
		if !strings.HasPrefix(absFilePath, repo) {
			continue
		}
		cmd := exec.Command("git", "blame", "-p", absFilePath)
		cmd.Dir = repo
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &bytes.Buffer{}
		if err := cmd.Run(); err != nil {
			continue
		}
		result := parseGitBlame(buf.String())
		if len(result) > 0 {
			return result
		}
	}
	return nil
}

func parseGitBlame(output string) map[int]string {
	result := make(map[int]string)
	lines := strings.Split(output, "\n")
	commitAuthors := make(map[string]string)
	currentCommit := ""
	currentLine := 0

	for _, line := range lines {
		if line == "" {
			continue
		}
		// Header line: <40-hex> <orig-line> <final-line> [<num-lines>]
		fields := strings.Fields(line)
		if len(fields) >= 3 && len(fields[0]) == 40 && isHexChars(fields[0]) {
			currentCommit = fields[0]
			n, err := strconv.Atoi(fields[2])
			if err == nil {
				currentLine = n
			}
			continue
		}
		if strings.HasPrefix(line, "author ") {
			author := strings.TrimPrefix(line, "author ")
			commitAuthors[currentCommit] = author
			if currentLine > 0 {
				result[currentLine] = author
			}
			continue
		}
		// Content line starts with tab
		if strings.HasPrefix(line, "\t") {
			if currentLine > 0 {
				if author, ok := commitAuthors[currentCommit]; ok {
					result[currentLine] = author
				}
			}
		}
	}
	return result
}

func isHexChars(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func (a *Analyzer) defaultMainBranch() string {
	for _, name := range []string{"main", "master"} {
		out := a.git(a.RepoPath, "rev-parse", "--verify", name)
		if strings.TrimSpace(out) != "" {
			return name
		}
	}
	return "HEAD"
}

// GetBranchStats collects branch management metrics across all repos.
func GetBranchStats(gitRepos []string, staleDays int) BranchStats {
	var bs BranchStats
	bs.StaleThresholdDays = staleDays
	if bs.StaleThresholdDays <= 0 {
		bs.StaleThresholdDays = 30
	}

	now := float64(time.Now().Unix())
	threshold := now - float64(bs.StaleThresholdDays)*86400
	seenBranch := make(map[string]bool)
	skipNames := map[string]bool{"main": true, "master": true, "develop": true, "HEAD": true}

	var lifetimes, ttms, integDelays []float64
	dayCounts := make(map[time.Weekday]int)

	for _, repo := range gitRepos {
		a := &Analyzer{RepoPath: repo}
		mainBranch := a.defaultMainBranch()

		// 1. Branch inventory: stale detection + depth from naming
		branchOut := a.git(repo, "for-each-ref",
			"--format=%(refname:short)\t%(committerdate:unix)",
			"refs/heads/")
		for _, line := range strings.Split(strings.TrimSpace(branchOut), "\n") {
			parts := strings.SplitN(line, "\t", 2)
			if len(parts) < 2 {
				continue
			}
			name := strings.TrimSpace(parts[0])
			ts, _ := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
			if name == "" || seenBranch[name] {
				continue
			}
			seenBranch[name] = true
			bs.TotalBranches++

			// Depth from slash-delimited name parts (e.g. feature/team/x → 3)
			if depth := strings.Count(name, "/") + 1; depth > bs.MaxDepth {
				bs.MaxDepth = depth
			}

			if !skipNames[name] && ts > 0 && ts < threshold {
				bs.StaleBranches = append(bs.StaleBranches, BranchInfo{
					Name:         name,
					LastActivity: ts,
					DaysInactive: int((now - ts) / 86400),
				})
			}
		}

		// 2. Merge analysis: TTM, Lifetime, Integration Delay
		// Format: "<merge-ts> <first-parent> <second-parent>"
		mergeOut := a.git(repo, "log", mainBranch, "--merges",
			"-50", "--pretty=format:%at %P")
		for _, line := range strings.Split(strings.TrimSpace(mergeOut), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 3 {
				continue
			}
			mergeTs, err := strconv.ParseFloat(fields[0], 64)
			if err != nil || mergeTs <= 0 {
				continue
			}
			mainParent := fields[1]
			featureTip := fields[2]

			// Timestamps of all commits on the feature branch not reachable from main
			featureLog := a.git(repo, "log",
				fmt.Sprintf("%s..%s", mainParent, featureTip),
				"--pretty=format:%at")
			if featureLog == "" {
				continue
			}
			var timestamps []float64
			for _, tsStr := range strings.Split(strings.TrimSpace(featureLog), "\n") {
				ts, err := strconv.ParseFloat(strings.TrimSpace(tsStr), 64)
				if err == nil && ts > 0 {
					timestamps = append(timestamps, ts)
				}
			}
			if len(timestamps) == 0 {
				continue
			}

			minTs, maxTs := timestamps[0], timestamps[0]
			for _, ts := range timestamps[1:] {
				if ts < minTs {
					minTs = ts
				}
				if ts > maxTs {
					maxTs = ts
				}
			}

			if d := (maxTs - minTs) / 86400; d >= 0 && d < 365 {
				lifetimes = append(lifetimes, d)
			}
			if d := (mergeTs - minTs) / 86400; d >= 0 && d < 730 {
				ttms = append(ttms, d)
			}
			if h := (mergeTs - maxTs) / 3600; h >= 0 && h < 8760 {
				integDelays = append(integDelays, h)
			}
		}

		// 3. Rollback rate on main
		rollbackOut := a.git(repo, "log", mainBranch,
			"--pretty=format:%H", "--grep=revert", "--grep=rollback", "-i")
		for _, h := range strings.Split(strings.TrimSpace(rollbackOut), "\n") {
			if strings.TrimSpace(h) != "" {
				bs.RollbackCount++
			}
		}

		// 4. Peak commit day: tally all commit timestamps across all branches
		dayOut := a.git(repo, "log", "--all",
			fmt.Sprintf("-%d", 2000), "--pretty=format:%at")
		for _, line := range strings.Split(strings.TrimSpace(dayOut), "\n") {
			ts, err := strconv.ParseFloat(strings.TrimSpace(line), 64)
			if err != nil || ts <= 0 {
				continue
			}
			weekday := time.Unix(int64(ts), 0).UTC().Weekday()
			dayCounts[weekday]++
		}

		countOut := a.git(repo, "rev-list", "--count", mainBranch)
		n, _ := strconv.Atoi(strings.TrimSpace(countOut))
		bs.TotalMainCommits += n
	}

	avg := func(vals []float64) float64 {
		if len(vals) == 0 {
			return 0
		}
		sum := 0.0
		for _, v := range vals {
			sum += v
		}
		return sum / float64(len(vals))
	}
	bs.AvgLifetimeDays = avg(lifetimes)
	bs.AvgTTMDays = avg(ttms)
	bs.AvgIntegDelayHours = avg(integDelays)

	peakDay, peakCount := time.Sunday, 0
	for day, count := range dayCounts {
		if count > peakCount {
			peakCount = count
			peakDay = day
		}
	}
	if peakCount > 0 {
		bs.PeakCommitDay = peakDay.String()
	}

	sort.Slice(bs.StaleBranches, func(i, j int) bool {
		return bs.StaleBranches[i].DaysInactive > bs.StaleBranches[j].DaysInactive
	})
	if len(bs.StaleBranches) > 20 {
		bs.StaleBranches = bs.StaleBranches[:20]
	}
	return bs
}
