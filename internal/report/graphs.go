package report

import (
	"os"
	"sort"

	"github.com/goscope/internal/parser"
	"github.com/goscope/internal/scanner"
)

type gNode struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"`
	Sublabel string  `json:"sublabel"`
	Kind     string  `json:"kind"`
	Score    float64 `json:"score"`
	Group    string  `json:"group"`
}
type gLink struct {
	Source string `json:"source"`
	Target string `json:"target"`
}
type gData struct {
	Nodes []gNode `json:"nodes"`
	Links []gLink `json:"links"`
}

func newGData() gData {
	return gData{Nodes: make([]gNode, 0), Links: make([]gLink, 0)}
}

func buildArchitectureGraph(microservices []*MicroserviceSummary, techList []string, files []*parser.ParsedFile, foreignServices []scanner.ForeignService) gData {
	msTechs := make(map[string]map[string]bool)
	for _, f := range files {
		if f.MicroserviceName == "" {
			continue
		}
		if msTechs[f.MicroserviceName] == nil {
			msTechs[f.MicroserviceName] = make(map[string]bool)
		}
		for _, imp := range f.Imports {
			ts := make(map[string]bool)
			detectTechFromImport(imp, ts)
			for t := range ts {
				msTechs[f.MicroserviceName][t] = true
			}
		}
		if f.FileType == "proto" {
			msTechs[f.MicroserviceName]["gRPC"] = true
			msTechs[f.MicroserviceName]["Protocol Buffers"] = true
		}
	}

	var nodes []gNode
	usedTechs := make(map[string]bool)

	for _, ms := range microservices {
		score := float64(ms.TotalLines) / 1000.0
		nodes = append(nodes, gNode{
			ID: "ms:" + ms.Name, Label: ms.Name, Sublabel: fmtNum(ms.TotalLines) + " loc",
			Kind: "microservice", Score: score, Group: "ms",
		})
		for t := range msTechs[ms.Name] {
			usedTechs[t] = true
		}
	}
	// Foreign services
	for _, fs := range foreignServices {
		nodes = append(nodes, gNode{
			ID: "ms:" + fs.Name, Label: fs.Name, Sublabel: fs.Language,
			Kind: "foreign", Score: float64(fs.LineCount) / 1000.0, Group: "foreign",
		})
		usedTechs[fs.Language] = true
	}

	for _, t := range techList {
		if usedTechs[t] && t != "Go" {
			nodes = append(nodes, gNode{
				ID: "tech:" + t, Label: t, Sublabel: "technology",
				Kind: "technology", Score: 3, Group: "tech",
			})
		}
	}

	var links []gLink
	for _, ms := range microservices {
		for t := range msTechs[ms.Name] {
			if usedTechs[t] && t != "Go" {
				links = append(links, gLink{Source: "ms:" + ms.Name, Target: "tech:" + t})
			}
		}
	}
	for _, fs := range foreignServices {
		links = append(links, gLink{Source: "ms:" + fs.Name, Target: "tech:" + fs.Language})
	}

	if nodes == nil {
		nodes = make([]gNode, 0)
	}
	if links == nil {
		links = make([]gLink, 0)
	}
	return gData{Nodes: nodes, Links: links}
}

// buildDeclGraph builds the per-microservice declaration graph.
// Nodes: structs, interfaces, messages, services, enums, and functions (all DeclFunc + BigFunctions).
// Edges: cross-file type references + co-location in the same file.
func buildDeclGraph(ms *MicroserviceSummary, scores map[string]float64) gData {
	typeKinds := map[parser.DeclKind]bool{
		parser.DeclStruct: true, parser.DeclInterface: true,
		parser.DeclMessage: true, parser.DeclService: true, parser.DeclEnum: true,
	}
	type di struct {
		name, fp, fn string
		kind         parser.DeclKind
	}

	// Collect all type declarations as nodes
	var ad []di
	funcSet := make(map[string]bool) // track func names to avoid duplication with BigFunctions
	for _, f := range ms.Files {
		for _, d := range f.Declarations {
			if typeKinds[d.Kind] && len(d.Name) >= 3 {
				ad = append(ad, di{d.Name, f.FilePath, f.FileName(), d.Kind})
			}
			if d.Kind == parser.DeclFunc && len(d.Name) >= 3 {
				key := f.FilePath + "::" + d.Name
				if !funcSet[key] {
					ad = append(ad, di{d.Name, f.FilePath, f.FileName(), parser.DeclFunc})
					funcSet[key] = true
				}
			}
		}
		// Also add BigFunctions that might not be in declarations
		for _, bf := range f.BigFunctions {
			key := bf.FilePath + "::" + bf.Name
			if !funcSet[key] {
				ad = append(ad, di{bf.Name, bf.FilePath, f.FileName(), parser.DeclFunc})
				funcSet[key] = true
			}
		}
	}

	// Cap nodes — keep highest-scored files' declarations
	if len(ad) > 80 {
		sort.Slice(ad, func(i, j int) bool { return scores[ad[i].fp] > scores[ad[j].fp] })
		ad = ad[:80]
	}

	nodes := make([]gNode, len(ad))
	nodeScores := make(map[string]float64)
	nodeSet := make(map[string]bool) // valid node IDs
	for i, d := range ad {
		s := scores[d.fp]
		if s < 0.001 {
			s = 0.001
		}
		nid := d.fp + "::" + d.name
		nodes[i] = gNode{ID: nid, Label: d.name, Sublabel: d.fn, Kind: string(d.kind), Score: s, Group: ms.Name}
		nodeScores[nid] = s
		nodeSet[nid] = true
	}

	// Pre-read all files in this microservice
	fileContents := make(map[string]string)
	for _, f := range ms.Files {
		content, err := os.ReadFile(f.FilePath)
		if err == nil {
			fileContents[f.FilePath] = string(content)
		}
	}

	// ── Build edges ──
	type candidate struct {
		target   string
		tgtScore float64
	}
	outgoing := make(map[string][]candidate)
	seen := make(map[string]bool)

	// Group nodes by file for co-location edges
	fileNodes := make(map[string][]di)
	for _, d := range ad {
		fileNodes[d.fp] = append(fileNodes[d.fp], d)
	}

	// Strategy 1: Cross-file references
	// For each file, check which names from OTHER files appear in it
	for fp, cs := range fileContents {
		localNames := make(map[string]bool)
		for _, d := range fileNodes[fp] {
			localNames[d.name] = true
		}
		for _, d := range fileNodes[fp] {
			srcID := d.fp + "::" + d.name
			for _, tgt := range ad {
				if tgt.fp == fp { // skip same file — handled by co-location
					continue
				}
				if tgt.name == d.name || len(tgt.name) <= 4 {
					continue
				}
				ek := srcID + "->" + tgt.fp + "::" + tgt.name
				if seen[ek] {
					continue
				}
				if matchGoTypeRef(cs, tgt.name) {
					tgtID := tgt.fp + "::" + tgt.name
					outgoing[srcID] = append(outgoing[srcID], candidate{tgtID, nodeScores[tgtID]})
					seen[ek] = true
				}
			}
		}
	}

	// Strategy 2: Co-location — declarations in the same file are related
	for _, nodesInFile := range fileNodes {
		if len(nodesInFile) < 2 || len(nodesInFile) > 20 {
			continue
		}
		for i, src := range nodesInFile {
			srcID := src.fp + "::" + src.name
			for j, tgt := range nodesInFile {
				if i == j || tgt.name == src.name {
					continue
				}
				tgtID := tgt.fp + "::" + tgt.name
				ek := srcID + "->" + tgtID
				if !seen[ek] {
					outgoing[srcID] = append(outgoing[srcID], candidate{tgtID, nodeScores[tgtID]})
					seen[ek] = true
				}
			}
		}
	}

	// Strategy 3: Proto service → message edges
	// Connect proto services to request/response messages by name matching
	for _, src := range ad {
		if src.kind != parser.DeclService {
			continue
		}
		cs := fileContents[src.fp]
		if cs == "" {
			continue
		}
		srcID := src.fp + "::" + src.name
		for _, tgt := range ad {
			if tgt.kind != parser.DeclMessage && tgt.kind != parser.DeclRPC {
				continue
			}
			if len(tgt.name) <= 4 {
				continue
			}
			ek := srcID + "->" + tgt.fp + "::" + tgt.name
			if seen[ek] {
				continue
			}
			if matchGoTypeRef(cs, tgt.name) {
				tgtID := tgt.fp + "::" + tgt.name
				outgoing[srcID] = append(outgoing[srcID], candidate{tgtID, nodeScores[tgtID]})
				seen[ek] = true
			}
		}
	}

	// Strategy 4: Function call edges across files
	// If funcA's file mentions funcB's name, create funcA → funcB edge
	for _, src := range ad {
		if src.kind != parser.DeclFunc {
			continue
		}
		cs := fileContents[src.fp]
		if cs == "" {
			continue
		}
		srcID := src.fp + "::" + src.name
		for _, tgt := range ad {
			if tgt.kind != parser.DeclFunc || tgt.fp == src.fp || tgt.name == src.name {
				continue
			}
			if len(tgt.name) <= 4 {
				continue
			}
			ek := srcID + "->" + tgt.fp + "::" + tgt.name
			if seen[ek] {
				continue
			}
			if matchGoTypeRef(cs, tgt.name) {
				tgtID := tgt.fp + "::" + tgt.name
				outgoing[srcID] = append(outgoing[srcID], candidate{tgtID, nodeScores[tgtID]})
				seen[ek] = true
			}
		}
	}

	// Cap outgoing edges per node
	maxOutPerNode := 5
	var links []gLink
	for srcID, cands := range outgoing {
		if !nodeSet[srcID] {
			continue
		}
		sort.Slice(cands, func(i, j int) bool { return cands[i].tgtScore > cands[j].tgtScore })
		limit := maxOutPerNode
		if limit > len(cands) {
			limit = len(cands)
		}
		for _, c := range cands[:limit] {
			if nodeSet[c.target] {
				links = append(links, gLink{Source: srcID, Target: c.target})
			}
		}
	}

	// Global cap
	maxEdges := len(nodes) * 3
	if maxEdges < 10 {
		maxEdges = 10
	}
	if len(links) > maxEdges {
		links = links[:maxEdges]
	}

	// Remove orphan nodes (no edges) — they fly away and break zoomToFit
	linkedIDs := make(map[string]bool)
	for _, l := range links {
		linkedIDs[l.Source] = true
		linkedIDs[l.Target] = true
	}
	var connectedNodes []gNode
	for _, n := range nodes {
		if linkedIDs[n.ID] {
			connectedNodes = append(connectedNodes, n)
		}
	}
	if connectedNodes == nil {
		connectedNodes = make([]gNode, 0)
	}
	if links == nil {
		links = make([]gLink, 0)
	}
	return gData{Nodes: connectedNodes, Links: links}
}
