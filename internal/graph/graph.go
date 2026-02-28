package graph

import (
	"sort"
	"strings"

	"github.com/goscope/internal/parser"
)

// DependencyGraph is a directed dependency graph with PageRank scoring.
type DependencyGraph struct {
	Vertices      map[string]bool
	Edges         [][2]string // [source, target]
	adjacency     map[string]map[string]bool
	reverseAdj    map[string]map[string]bool
	PageRankScores map[string]float64
}

func New() *DependencyGraph {
	return &DependencyGraph{
		Vertices:       make(map[string]bool),
		adjacency:      make(map[string]map[string]bool),
		reverseAdj:     make(map[string]map[string]bool),
		PageRankScores: make(map[string]float64),
	}
}

func (g *DependencyGraph) AddVertex(v string) {
	g.Vertices[v] = true
	if g.adjacency[v] == nil {
		g.adjacency[v] = make(map[string]bool)
	}
	if g.reverseAdj[v] == nil {
		g.reverseAdj[v] = make(map[string]bool)
	}
}

func (g *DependencyGraph) AddEdge(source, target string) {
	if source == target || !g.Vertices[source] || !g.Vertices[target] {
		return
	}
	if g.adjacency[source][target] {
		return
	}
	g.Edges = append(g.Edges, [2]string{source, target})
	g.adjacency[source][target] = true
	g.reverseAdj[target][source] = true
}

func (g *DependencyGraph) OutDegree(v string) int {
	return len(g.adjacency[v])
}

func (g *DependencyGraph) InDegree(v string) int {
	return len(g.reverseAdj[v])
}

// Build builds the graph from parsed files using import-based edges.
func (g *DependencyGraph) Build(files []*parser.ParsedFile) {
	nameToPath := make(map[string]string)

	for _, f := range files {
		g.AddVertex(f.FilePath)
		nameToPath[f.FileNameWithoutExt()] = f.FilePath
		if f.ModuleName != "" {
			nameToPath[f.ModuleName] = f.FilePath
		}
	}

	// Import-based edges
	for _, src := range files {
		for _, imp := range src.Imports {
			// Try matching on last path segment
			parts := strings.Split(imp, "/")
			baseName := parts[len(parts)-1]
			if targetPath, ok := nameToPath[baseName]; ok {
				g.AddEdge(src.FilePath, targetPath)
			}
		}
	}

	// Type-reference edges within same microservice
	byMS := make(map[string][]*parser.ParsedFile)
	for _, f := range files {
		ms := f.MicroserviceName
		if ms == "" {
			ms = "__root__"
		}
		byMS[ms] = append(byMS[ms], f)
	}

	for _, msFiles := range byMS {
		g.buildTypeRefEdges(msFiles)
	}
}

func (g *DependencyGraph) buildTypeRefEdges(files []*parser.ParsedFile) {
	type declInfo struct {
		name string
		path string
	}

	var decls []declInfo
	for _, f := range files {
		for _, d := range f.Declarations {
			if d.Kind == parser.DeclStruct || d.Kind == parser.DeclInterface ||
				d.Kind == parser.DeclMessage || d.Kind == parser.DeclService {
				if len(d.Name) >= 3 {
					decls = append(decls, declInfo{name: d.Name, path: f.FilePath})
				}
			}
		}
	}

	if len(decls) > 500 {
		decls = decls[:500]
	}

	// Read file contents and check for type references
	for _, f := range files {
		content, err := readFileContent(f.FilePath)
		if err != nil {
			continue
		}
		for _, d := range decls {
			if d.path == f.FilePath {
				continue
			}
			if containsTypeName(content, d.name) {
				g.AddEdge(f.FilePath, d.path)
			}
		}
	}
}

func containsTypeName(content, typeName string) bool {
	idx := 0
	for {
		pos := strings.Index(content[idx:], typeName)
		if pos < 0 {
			return false
		}
		pos += idx
		// Check word boundaries
		before := pos > 0 && isWordChar(content[pos-1])
		after := pos+len(typeName) < len(content) && isWordChar(content[pos+len(typeName)])
		if !before && !after {
			return true
		}
		idx = pos + len(typeName)
		if idx >= len(content) {
			return false
		}
	}
}

func isWordChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

func readFileContent(path string) (string, error) {
	data, err := ReadFile(path)
	return string(data), err
}

// ReadFile reads a file with size limit.
func ReadFile(path string) ([]byte, error) {
	const maxSize = 512 * 1024 // 512KB
	f, err := openFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	buf := make([]byte, maxSize)
	n, _ := f.Read(buf)
	return buf[:n], nil
}

// Analyze computes PageRank.
func (g *DependencyGraph) Analyze() {
	g.computePageRank(0.85, 100)
}

func (g *DependencyGraph) computePageRank(damping float64, iterations int) {
	n := float64(len(g.Vertices))
	if n == 0 {
		return
	}

	scores := make(map[string]float64)
	for v := range g.Vertices {
		scores[v] = 1.0 / n
	}

	for i := 0; i < iterations; i++ {
		newScores := make(map[string]float64)
		for v := range g.Vertices {
			newScores[v] = (1.0 - damping) / n
		}
		for v := range g.Vertices {
			neighbors := g.adjacency[v]
			if len(neighbors) == 0 {
				continue
			}
			share := scores[v] / float64(len(neighbors))
			for neighbor := range neighbors {
				newScores[neighbor] += damping * share
			}
		}
		scores = newScores
	}
	g.PageRankScores = scores
}

// HotspotEntry holds a path and its PageRank score.
type HotspotEntry struct {
	Path  string
	Score float64
}

// GetTopHotspots returns the top N files by PageRank.
func (g *DependencyGraph) GetTopHotspots(limit int) []HotspotEntry {
	var entries []HotspotEntry
	for path, score := range g.PageRankScores {
		entries = append(entries, HotspotEntry{Path: path, Score: score})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Score > entries[j].Score
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries
}
