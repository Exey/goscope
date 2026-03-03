package graph

import (
	"testing"

	"github.com/goscope/internal/parser"
)

func TestGraphBasic(t *testing.T) {
	g := New()
	g.AddVertex("a")
	g.AddVertex("b")
	g.AddEdge("a", "b")

	if len(g.Vertices) != 2 {
		t.Errorf("Vertices = %d, want 2", len(g.Vertices))
	}
	if len(g.Edges) != 1 {
		t.Errorf("Edges = %d, want 1", len(g.Edges))
	}
	if g.OutDegree("a") != 1 {
		t.Errorf("OutDegree(a) = %d, want 1", g.OutDegree("a"))
	}
	if g.InDegree("b") != 1 {
		t.Errorf("InDegree(b) = %d, want 1", g.InDegree("b"))
	}
	if g.InDegree("a") != 0 {
		t.Errorf("InDegree(a) = %d, want 0", g.InDegree("a"))
	}
}

func TestGraphDuplicateEdge(t *testing.T) {
	g := New()
	g.AddVertex("a")
	g.AddVertex("b")
	g.AddEdge("a", "b")
	g.AddEdge("a", "b") // duplicate
	if len(g.Edges) != 1 {
		t.Errorf("Edges = %d, want 1 (no duplicates)", len(g.Edges))
	}
}

func TestPageRank(t *testing.T) {
	g := New()
	// A -> B -> C -> A (cycle) + D -> B
	g.AddVertex("a")
	g.AddVertex("b")
	g.AddVertex("c")
	g.AddVertex("d")
	g.AddEdge("a", "b")
	g.AddEdge("b", "c")
	g.AddEdge("c", "a")
	g.AddEdge("d", "b")
	g.Analyze()

	// B should have highest rank (most incoming: from A and D)
	if g.PageRankScores["b"] <= g.PageRankScores["d"] {
		t.Errorf("PageRank[b]=%f should be > PageRank[d]=%f", g.PageRankScores["b"], g.PageRankScores["d"])
	}
	// All scores should be > 0
	for v, s := range g.PageRankScores {
		if s <= 0 {
			t.Errorf("PageRank[%s] = %f, want > 0", v, s)
		}
	}
}

func TestGetTopHotspots(t *testing.T) {
	g := New()
	g.AddVertex("a")
	g.AddVertex("b")
	g.AddVertex("c")
	g.AddVertex("d")
	g.AddEdge("a", "b")
	g.AddEdge("c", "b")
	g.AddEdge("d", "b")
	g.Analyze()

	hs := g.GetTopHotspots(2)
	if len(hs) != 2 {
		t.Fatalf("GetTopHotspots(2) = %d items, want 2", len(hs))
	}
	if hs[0].Path != "b" {
		t.Errorf("Top hotspot = %q, want b", hs[0].Path)
	}
	if hs[0].Score <= hs[1].Score {
		t.Error("Hotspots not sorted by score descending")
	}
}

func TestBuildFromFiles(t *testing.T) {
	files := []*parser.ParsedFile{
		{
			FilePath:         "/code/svc/a.go",
			MicroserviceName: "svc",
			Imports:          []string{"fmt", "github.com/example/pkg"},
			Declarations: []parser.Declaration{
				{Name: "ServiceA", Kind: parser.DeclStruct},
			},
		},
		{
			FilePath:         "/code/svc/b.go",
			MicroserviceName: "svc",
			Imports:          []string{"fmt"},
			Declarations: []parser.Declaration{
				{Name: "ServiceB", Kind: parser.DeclStruct},
			},
		},
	}

	g := New()
	g.Build(files)

	if len(g.Vertices) < 2 {
		t.Errorf("Vertices = %d, want >= 2", len(g.Vertices))
	}
}
