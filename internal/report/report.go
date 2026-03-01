package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	gitpkg "github.com/goscope/internal/git"
	"github.com/goscope/internal/graph"
	"github.com/goscope/internal/parser"
	"github.com/goscope/internal/scanner"
)

type MicroserviceSummary struct {
	Name           string
	Files          []*parser.ParsedFile
	TotalLines     int
	Declarations   []parser.Declaration
	StructCount    int
	InterfaceCount int
	FuncCount      int
	MessageCount   int
	ServiceCount   int
	RPCCount       int
	EnumCount      int
}

func newMS(name string, files []*parser.ParsedFile) *MicroserviceSummary {
	ms := &MicroserviceSummary{Name: name, Files: files}
	for _, f := range files {
		ms.TotalLines += f.LineCount
		for _, d := range f.Declarations {
			ms.Declarations = append(ms.Declarations, d)
			switch d.Kind {
			case parser.DeclStruct:
				ms.StructCount++
			case parser.DeclInterface:
				ms.InterfaceCount++
			case parser.DeclFunc:
				ms.FuncCount++
			case parser.DeclMessage:
				ms.MessageCount++
			case parser.DeclService:
				ms.ServiceCount++
			case parser.DeclRPC:
				ms.RPCCount++
			case parser.DeclEnum:
				ms.EnumCount++
			}
		}
	}
	return ms
}

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

func Generate(
	g *graph.DependencyGraph,
	outputPath string,
	files []*parser.ParsedFile,
	branchName string,
	authorStats map[string]*gitpkg.AuthorStats,
	projectName string,
	technologies []string,
	dockerServices []string,
	rootSubdirs []string,
	foreignServices []scanner.ForeignService,
) error {
	fmt.Println("   Generating HTML sections...")

	fileMap := make(map[string]*parser.ParsedFile)
	for _, f := range files {
		fileMap[f.FilePath] = f
	}
	dateFmt := "2006-01-02"

	// ─── Stats ───
	var totalLines, totalGoFiles, totalProtoFiles int
	var totalStructs, totalInterfaces, totalFuncs int
	var totalMessages, totalServices, totalRPCs, totalEnums int
	var totalTodos, totalFixmes int

	for _, f := range files {
		totalLines += f.LineCount
		if f.FileType == "go" {
			totalGoFiles++
		} else if f.FileType == "proto" {
			totalProtoFiles++
		}
		totalTodos += f.TodoCount
		totalFixmes += f.FixmeCount
		for _, d := range f.Declarations {
			switch d.Kind {
			case parser.DeclStruct:
				totalStructs++
			case parser.DeclInterface:
				totalInterfaces++
			case parser.DeclFunc:
				totalFuncs++
			case parser.DeclMessage:
				totalMessages++
			case parser.DeclService:
				totalServices++
			case parser.DeclRPC:
				totalRPCs++
			case parser.DeclEnum:
				totalEnums++
			}
		}
	}

	// Foreign language stats
	foreignLangLines := make(map[string]int) // language -> total lines
	for _, fs := range foreignServices {
		foreignLangLines[fs.Language] += fs.LineCount
	}
	var foreignLangCards string
	type langStat struct {
		Lang  string
		Lines int
	}
	var ls []langStat
	for l, n := range foreignLangLines {
		ls = append(ls, langStat{l, n})
	}
	sort.Slice(ls, func(i, j int) bool { return ls[i].Lines > ls[j].Lines })
	for _, l := range ls {
		foreignLangCards += fmt.Sprintf(`<div class="summary-card"><div class="num">%s</div><div class="label">%s lines</div></div>`, fmtNum(l.Lines), esc(l.Lang))
	}

	// ─── Microservices ───
	msFiles := make(map[string][]*parser.ParsedFile)
	for _, f := range files {
		ms := f.MicroserviceName
		if ms == "" {
			ms = "root"
		}
		msFiles[ms] = append(msFiles[ms], f)
	}
	var microservices []*MicroserviceSummary
	for name, mf := range msFiles {
		microservices = append(microservices, newMS(name, mf))
	}
	sort.Slice(microservices, func(i, j int) bool {
		iGW := isAPIGateway(microservices[i].Name)
		jGW := isAPIGateway(microservices[j].Name)
		if iGW != jGW {
			return iGW
		}
		iP := isProtoMS(microservices[i].Name)
		jP := isProtoMS(microservices[j].Name)
		if iP != jP {
			return iP
		}
		return microservices[i].TotalLines > microservices[j].TotalLines
	})

	totalMSCount := len(microservices) + len(foreignServices)

	// ─── 1. Team ───
	type authorEntry struct {
		Name  string
		Stats *gitpkg.AuthorStats
	}
	var teamEntries []authorEntry
	for name, stats := range authorStats {
		teamEntries = append(teamEntries, authorEntry{name, stats})
	}
	sort.Slice(teamEntries, func(i, j int) bool {
		return teamEntries[i].Stats.FilesModified > teamEntries[j].Stats.FilesModified
	})
	if len(teamEntries) > 30 {
		teamEntries = teamEntries[:30]
	}

	var teamRows strings.Builder
	for _, ae := range teamEntries {
		first := "—"
		if ae.Stats.FirstCommit > 0 {
			first = time.Unix(int64(ae.Stats.FirstCommit), 0).Format(dateFmt)
		}
		last := "—"
		if ae.Stats.LastCommit > 0 {
			last = time.Unix(int64(ae.Stats.LastCommit), 0).Format(dateFmt)
		}
		top3ms := topNKeys(ae.Stats.MicroserviceCounts, 3)
		var top3html string
		for _, ms := range top3ms {
			anchor := strings.ReplaceAll(ms, " ", "-")
			top3html += fmt.Sprintf("<a href='#ms-%s' class='tag tag-local pkg-link-inline' style='font-size:11px'>%s</a> ", anchor, esc(ms))
		}
		teamRows.WriteString(fmt.Sprintf(
			"<tr><td>%s</td><td>%d</td><td>%d</td><td>%s</td><td>%s</td><td>%s</td></tr>\n",
			esc(ae.Name), ae.Stats.FilesModified, ae.Stats.TotalCommits, first, last, top3html,
		))
	}

	// ─── 2. Tech Stack: Technologies ───
	techSet := make(map[string]bool)
	for _, t := range technologies {
		techSet[t] = true
	}
	techSet["Go"] = true
	for _, f := range files {
		for _, imp := range f.Imports {
			detectTechFromImport(imp, techSet)
		}
	}
	if totalProtoFiles > 0 {
		techSet["Protocol Buffers"] = true
		techSet["gRPC"] = true
	}
	// Add foreign languages as technologies
	for _, fs := range foreignServices {
		techSet[fs.Language] = true
	}
	var techList []string
	for t := range techSet {
		techList = append(techList, t)
	}
	sort.Strings(techList)

	// Tech tags with language badge for non-infra techs
	foreignLangs := make(map[string]bool)
	for _, fs := range foreignServices {
		foreignLangs[fs.Language] = true
	}
	var techTags string
	for _, t := range techList {
		cls := "tag-tech"
		if foreignLangs[t] {
			cls = "tag-foreign"
		}
		techTags += fmt.Sprintf("<span class='tag %s'>%s</span> ", cls, esc(t))
	}

	// ─── 2b. Architecture graph ───
	archGraph := buildArchitectureGraph(microservices, techList, files, foreignServices)
	archGraphJSON, _ := json.Marshal(archGraph)

	// ─── 2c. Microservices grid ───
	var msGridHTML strings.Builder
	for _, ms := range microservices {
		anchor := strings.ReplaceAll(ms.Name, " ", "-")
		badge := fmt.Sprintf("%s loc", fmtNum(ms.TotalLines))
		icon := "🔧"
		if isAPIGateway(ms.Name) {
			icon = "🌐"
		} else if isProtoMS(ms.Name) {
			icon = "📡"
		}
		msGridHTML.WriteString(fmt.Sprintf(
			"<a href='#ms-%s' class='tag tag-local pkg-link'><span class='pkg-name'>%s %s</span><span class='bs-badge-right'>%s</span></a>\n",
			anchor, icon, esc(ms.Name), badge,
		))
	}
	// Foreign services in the grid
	for _, fs := range foreignServices {
		badge := fmt.Sprintf("%s loc · %s", fmtNum(fs.LineCount), esc(fs.Language))
		msGridHTML.WriteString(fmt.Sprintf(
			"<span class='tag tag-foreign pkg-link'><span class='pkg-name'>🌐 %s</span><span class='bs-badge-right'>%s</span></span>\n",
			esc(fs.Name), badge,
		))
	}

	// ─── 3. Microservices penetration ───
	allMSNames := make(map[string]bool)
	for _, ms := range microservices {
		allMSNames[ms.Name] = true
	}
	msImportedBy := make(map[string]map[string]bool)
	for _, f := range files {
		srcMS := f.MicroserviceName
		if srcMS == "" {
			continue
		}
		for _, imp := range f.Imports {
			impLower := strings.ToLower(imp)
			for targetMS := range allMSNames {
				if targetMS == srcMS {
					continue
				}
				if strings.Contains(impLower, strings.ToLower(targetMS)) {
					if msImportedBy[targetMS] == nil {
						msImportedBy[targetMS] = make(map[string]bool)
					}
					msImportedBy[targetMS][srcMS] = true
				}
			}
		}
	}
	type penEntry struct {
		Name       string
		Count      int
		Dependents []string
	}
	var penList []penEntry
	for ms, deps := range msImportedBy {
		if len(deps) >= 1 {
			var dl []string
			for d := range deps {
				dl = append(dl, d)
			}
			sort.Strings(dl)
			penList = append(penList, penEntry{ms, len(deps), dl})
		}
	}
	sort.Slice(penList, func(i, j int) bool { return penList[i].Count > penList[j].Count })
	if len(penList) > 20 {
		penList = penList[:20]
	}

	msTodos := make(map[string]int)
	msFixmes := make(map[string]int)
	for _, f := range files {
		ms := f.MicroserviceName
		if ms == "" {
			ms = "root"
		}
		msTodos[ms] += f.TodoCount
		msFixmes[ms] += f.FixmeCount
	}
	type todoEntry struct {
		Name   string
		Todos  int
		Fixmes int
	}
	var todoList []todoEntry
	for ms, todos := range msTodos {
		fixmes := msFixmes[ms]
		if todos+fixmes > 0 {
			todoList = append(todoList, todoEntry{ms, todos, fixmes})
		}
	}
	sort.Slice(todoList, func(i, j int) bool {
		return todoList[i].Todos+todoList[i].Fixmes > todoList[j].Todos+todoList[j].Fixmes
	})

	// ─── 4. Hot Zones (top 10 by PageRank) ───
	hotspots := g.GetTopHotspots(30) // fetch more, then filter
	var hotspotRows strings.Builder
	hotspotCount := 0
	for _, h := range hotspots {
		if hotspotCount >= 10 {
			break
		}
		fname := filepath.Base(h.Path)
		// Skip module.go
		if fname == "module.go" {
			continue
		}
		ms := "root"
		var lineCount, declCount int
		if f, ok := fileMap[h.Path]; ok {
			if f.MicroserviceName != "" {
				ms = f.MicroserviceName
			}
			lineCount = f.LineCount
			declCount = len(f.Declarations)
		}
		// Build a short relative path: microservice/...last dirs/filename
		displayPath := shortRelPath(h.Path, ms)
		displayDir, displayFile := splitDirFile(displayPath)

		anchor := strings.ReplaceAll(ms, " ", "-")
		hotspotRows.WriteString(fmt.Sprintf(
			"<tr><td><span style='color:var(--text3)'>%s</span><strong>%s</strong></td><td class='mono'>%.4f</td><td class='mono'>%d</td><td class='mono'>%d</td><td><a href='#ms-%s' class='tag tag-local pkg-link-inline' style='font-size:11px'>%s</a></td></tr>\n",
			esc(displayDir), esc(displayFile), h.Score, lineCount, declCount, anchor, esc(ms),
		))
		hotspotCount++
	}

	// ─── 5. Longest Functions ───
	var allFuncs []*parser.FunctionInfo
	for _, f := range files {
		if f.LongestFunction != nil {
			allFuncs = append(allFuncs, f.LongestFunction)
		}
	}
	sort.Slice(allFuncs, func(i, j int) bool { return allFuncs[i].LineCount > allFuncs[j].LineCount })
	if len(allFuncs) > 20 {
		allFuncs = allFuncs[:20]
	}

	// ─── 5. Microservice sections ───
	var msSections, msGraphScripts strings.Builder
	graphCounter := 0

	for _, ms := range microservices {
		sf := make([]*parser.ParsedFile, len(ms.Files))
		copy(sf, ms.Files)
		sort.Slice(sf, func(i, j int) bool { return sf[i].LineCount > sf[j].LineCount })

		anchor := strings.ReplaceAll(ms.Name, " ", "-")
		icon := "🔧"
		if isAPIGateway(ms.Name) {
			icon = "🌐"
		} else if isProtoMS(ms.Name) {
			icon = "📡"
		}

		var statsParts []string
		if ms.StructCount > 0 {
			statsParts = append(statsParts, fmt.Sprintf("🟢 %d structs", ms.StructCount))
		}
		if ms.InterfaceCount > 0 {
			statsParts = append(statsParts, fmt.Sprintf("🔵 %d interfaces", ms.InterfaceCount))
		}
		if ms.EnumCount > 0 {
			statsParts = append(statsParts, fmt.Sprintf("🟡 %d enums", ms.EnumCount))
		}
		if ms.FuncCount > 0 {
			statsParts = append(statsParts, fmt.Sprintf("🟠 %d funcs", ms.FuncCount))
		}
		if ms.MessageCount > 0 {
			statsParts = append(statsParts, fmt.Sprintf("📨 %d messages", ms.MessageCount))
		}
		if ms.ServiceCount > 0 {
			statsParts = append(statsParts, fmt.Sprintf("🔴 %d gRPC services", ms.ServiceCount))
		}
		if ms.RPCCount > 0 {
			statsParts = append(statsParts, fmt.Sprintf("🔗 %d RPCs", ms.RPCCount))
		}

		var fileRows strings.Builder
		for _, f := range sf {
			// Skip module.go files from visual listing (DI wiring boilerplate)
			if f.FileName() == "module.go" {
				continue
			}
			var dp []string
			for _, d := range f.Declarations {
				dp = append(dp, fmt.Sprintf("%s&thinsp;%s", kindIcon(d.Kind), esc(d.Name)))
			}
			declStr := "—"
			if len(dp) > 0 {
				declStr = strings.Join(dp, "&ensp;")
			}
			desc := ""
			if f.Description != "" {
				short := f.Description
				if len(short) > 120 {
					short = short[:120]
				}
				desc = fmt.Sprintf("<div class='file-desc'>💡 %s</div>", esc(short))
			}
			typeTag := ""
			if f.FileType == "proto" {
				typeTag = " <span class='bs-badge'>proto</span>"
			}
			fileRows.WriteString(fmt.Sprintf(
				"<tr><td><strong>%s</strong>%s%s</td><td class='mono'>%d</td><td>%d</td><td class='decl-tags'>%s</td></tr>\n",
				esc(f.FileName()), typeTag, desc, f.LineCount, len(f.Declarations), declStr,
			))
		}

		gID := fmt.Sprintf("ms-graph-%d", graphCounter)
		graphCounter++
		gd := buildDeclGraph(ms, g.PageRankScores)
		gdJ, _ := json.Marshal(gd)
		showG := len(gd.Nodes) >= 2

		graphDiv := ""
		if showG {
			graphDiv = fmt.Sprintf("<div id='%s' class='pkg-graph-container'></div>", gID)
		}

		msSections.WriteString(fmt.Sprintf(`<div class="package-section" id="ms-%s">
<h3>%s %s <span class="pkg-stats">%d files · %s lines · %d declarations</span></h3>
<p class="stats-detail">%s</p>
%s
<div class="table-wrap"><table class="file-table">
<thead><tr><th>File</th><th>Lines</th><th>Decl</th><th>Declarations</th></tr></thead>
<tbody>%s</tbody>
</table></div>
</div>
`, anchor, icon, esc(ms.Name), len(sf), fmtNum(ms.TotalLines), len(ms.Declarations),
			strings.Join(statsParts, " · "), graphDiv, fileRows.String()))

		if showG {
			msGraphScripts.WriteString(fmt.Sprintf(`{
const d=%s;const el=document.getElementById('%s');
if(d.nodes.length>0&&el){const kc={'struct':'#34c759','interface':'#007aff','message':'#ff9500','service':'#ff3b30','enum':'#af52de','func':'#ff9500'};
const g=ForceGraph()(el).graphData(d).nodeLabel(n=>n.label+' ('+n.sublabel+')\n'+n.kind).nodeVal(n=>Math.max(n.score*3000,5)).nodeColor(n=>kc[n.kind]||'#999')
.nodeCanvasObject((node,ctx,gs)=>{const r=Math.max(Math.sqrt(Math.max(node.score*3000,5))*0.8,3);ctx.beginPath();ctx.arc(node.x,node.y,r,0,2*Math.PI);ctx.fillStyle=kc[node.kind]||'#999';ctx.fill();if(gs>0.5){ctx.font=(Math.max(10/gs,3))+'px -apple-system,sans-serif';ctx.textAlign='center';ctx.fillStyle='#333';ctx.fillText(node.label,node.x,node.y+r+10/gs);}})
.linkDirectionalArrowLength(8).linkDirectionalArrowRelPos(1).linkColor(()=>'rgba(0,0,0,0.12)').width(el.offsetWidth).height(420)
.onEngineStop(()=>g.zoomToFit(400,40));
g.d3Force('charge').strength(-150);g.d3Force('link').distance(60);}}
`, string(gdJ), gID))
		}
	}

	// ─── Penetration rows ───
	var penRows strings.Builder
	for _, pe := range penList {
		ds := strings.Join(pe.Dependents, ", ")
		if len(ds) > 80 {
			ds = ds[:80] + "…"
		}
		anchor := strings.ReplaceAll(pe.Name, " ", "-")
		penRows.WriteString(fmt.Sprintf("<tr><td><a href='#ms-%s' class='pkg-link-inline'>%s</a></td><td class='mono'>%d</td><td style='color:var(--text3);font-size:12px'>%s</td></tr>\n", anchor, esc(pe.Name), pe.Count, esc(ds)))
	}
	var todoRows strings.Builder
	for _, te := range todoList {
		a := strings.ReplaceAll(te.Name, " ", "-")
		todoRows.WriteString(fmt.Sprintf("<tr><td><a href='#ms-%s' class='pkg-link-inline'>%s</a></td><td>%d</td><td>%d</td><td><strong>%d</strong></td></tr>\n", a, esc(te.Name), te.Todos, te.Fixmes, te.Todos+te.Fixmes))
	}
	var funcRows strings.Builder
	for _, fn := range allFuncs {
		fname := filepath.Base(fn.FilePath)
		ms := "root"
		if f, ok := fileMap[fn.FilePath]; ok && f.MicroserviceName != "" {
			ms = f.MicroserviceName
		}
		a := strings.ReplaceAll(ms, " ", "-")
		funcRows.WriteString(fmt.Sprintf("<tr><td><code>%s()</code></td><td class='mono'>%d</td><td>%s</td><td><a href='#ms-%s' class='pkg-link-inline'>%s</a></td></tr>\n", esc(fn.Name), fn.LineCount, esc(fname), a, esc(ms)))
	}

	subdirDisplay := ""
	if len(rootSubdirs) > 0 {
		subdirDisplay = " · <span style='color:var(--text3);font-size:12px'>" + esc(strings.Join(rootSubdirs, " / ")) + "</span>"
	}

	projDisplay := projectName
	if projDisplay == "" {
		projDisplay = "Project"
	}

	protoCard := ""
	if totalProtoFiles > 0 {
		protoCard = fmt.Sprintf(`<div class="summary-card"><div class="num">%d</div><div class="label">📡 Proto Files</div></div>`, totalProtoFiles)
	}
	grpcCard := ""
	if totalServices > 0 {
		grpcCard = fmt.Sprintf(`<div class="summary-card"><div class="num">%d</div><div class="label">🔴 gRPC Services</div></div>`, totalServices)
	}
	todoCard := ""
	if totalTodos+totalFixmes > 0 {
		todoCard = fmt.Sprintf(`<div class="summary-card"><div class="num">%d</div><div class="label">TODO/FIXME</div></div>`, totalTodos+totalFixmes)
	}

	// ─── HTML ───
	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1.0">
<link rel="icon" href="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'><text y='.9em' font-size='90'>🔬</text></svg>">
<title>🔬 goscope — %s</title>
<style>
:root{--bg:#f5f5f7;--card:#fff;--border:#e5e5ea;--text:#1d1d1f;--text2:#424245;--text3:#86868b;--accent:#0071e3;--red:#ff3b30;}
*{box-sizing:border-box;}
body{font-family:-apple-system,BlinkMacSystemFont,'SF Pro Display','Helvetica Neue',sans-serif;margin:0;padding:20px;background:var(--bg);color:var(--text);line-height:1.5;}
.container{max-width:1280px;margin:0 auto;}
.card{background:var(--card);padding:28px;border-radius:16px;box-shadow:0 1px 12px rgba(0,0,0,0.06);margin-bottom:20px;}
h1{font-size:28px;font-weight:700;margin:0 0 4px 0;}
h2{color:var(--text2);font-size:20px;border-bottom:2px solid var(--border);padding-bottom:10px;margin:28px 0 16px 0;}
h3{color:var(--text2);font-size:16px;margin:20px 0 8px 0;}
.subtitle{color:var(--text3);font-size:14px;margin-bottom:20px;}
.branch-badge{display:inline-block;background:#e3f2fd;color:#1565c0;padding:2px 10px;border-radius:8px;font-size:13px;font-weight:500;font-family:'SF Mono',Menlo,monospace;}
.summary-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(130px,1fr));gap:10px;margin-bottom:24px;}
.summary-card{background:var(--bg);border-radius:12px;padding:14px 8px;text-align:center;}
.summary-card .num{font-size:26px;font-weight:700;color:var(--accent);}
.summary-card .label{font-size:11px;color:var(--text3);text-transform:uppercase;letter-spacing:0.04em;margin-top:2px;}
.team-table,.file-table{width:100%%;border-collapse:collapse;font-size:14px;}
.team-table th,.file-table th{color:var(--text3);font-weight:500;text-transform:uppercase;font-size:11px;letter-spacing:0.05em;text-align:left;padding:8px 10px;border-bottom:2px solid var(--border);}
.team-table td,.file-table td{padding:8px 10px;border-bottom:1px solid var(--border);vertical-align:top;}
.mono{font-family:'SF Mono',Menlo,monospace;font-size:13px;}
.tag{display:inline-block;padding:2px 8px;border-radius:6px;font-size:12px;font-weight:500;margin:2px;}
.tag-tech{background:#e8f5e9;color:#2e7d32;}
.tag-foreign{background:#fff3e0;color:#e65100;}
.tag-local{background:#e3f2fd;color:#1565c0;}
.tag-cloud{line-height:2.2;}
.pkg-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(240px,1fr));gap:4px 8px;}
.pkg-link{display:flex;align-items:center;justify-content:space-between;text-decoration:none;cursor:pointer;transition:background 0.15s;}
.pkg-link:hover{background:#bbdefb;}
.pkg-link-inline{text-decoration:none;cursor:pointer;}
.pkg-name{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;}
.bs-badge-right{background:rgba(0,0,0,0.07);color:var(--text3);font-size:9px;padding:1px 5px;border-radius:4px;margin-left:auto;padding-left:6px;flex-shrink:0;font-weight:400;}
.bs-badge{background:rgba(0,0,0,0.08);color:var(--text3);font-size:10px;padding:1px 5px;border-radius:4px;margin-left:2px;font-weight:400;}
.count{font-weight:400;color:var(--text3);}
.package-section{margin-bottom:32px;padding-bottom:24px;border-bottom:1px solid var(--border);}
.package-section:last-child{border-bottom:none;}
.pkg-stats{font-weight:400;color:var(--text3);font-size:13px;margin-left:8px;}
.stats-detail{color:var(--text3);font-size:13px;margin:4px 0 12px 0;}
.file-desc{color:var(--text3);font-size:12px;font-style:italic;margin-top:2px;}
.decl-tags{font-size:12px;line-height:1.8;}
.pkg-graph-container{width:100%%;height:420px;border:1px solid var(--border);border-radius:10px;margin-bottom:16px;overflow:hidden;background:#fafafa;}
.arch-graph-container{width:100%%;height:500px;border:1px solid var(--border);border-radius:10px;margin-bottom:16px;overflow:hidden;background:#fafafa;}
.table-wrap{width:100%%;overflow-x:auto;-webkit-overflow-scrolling:touch;}
@media(max-width:768px){body{padding:8px;}.card{padding:14px;border-radius:12px;}.summary-grid{grid-template-columns:repeat(3,1fr);gap:6px;}.summary-card{padding:10px 4px;}.summary-card .num{font-size:18px;}.summary-card .label{font-size:9px;}h1{font-size:20px;}h2{font-size:17px;}.team-table,.file-table{font-size:12px;min-width:500px;}.pkg-grid{grid-template-columns:repeat(auto-fill,minmax(160px,1fr));}.pkg-graph-container,.arch-graph-container{height:300px;}}
</style>
<script src="https://unpkg.com/force-graph"></script>
</head>
<body>
<div class="container">

<div class="card">
<h1>🔬 goscope report — %s</h1>
<p class="subtitle">Generated %s · <span class="branch-badge">%s</span>%s</p>
<div class="summary-grid">
    <div class="summary-card"><div class="num">%d</div><div class="label">Microservices</div></div>
    <div class="summary-card"><div class="num">%d</div><div class="label">Go Files</div></div>
    <div class="summary-card"><div class="num">%s</div><div class="label">Lines of Code</div></div>
    <div class="summary-card"><div class="num">%d</div><div class="label">Declarations</div></div>
    %s
    <div class="summary-card"><div class="num">%d</div><div class="label">🟢 Structs</div></div>
    <div class="summary-card"><div class="num">%d</div><div class="label">🔵 Interfaces</div></div>
    <div class="summary-card"><div class="num">%d</div><div class="label">🟡 Enums</div></div>
    <div class="summary-card"><div class="num">%d</div><div class="label">🟠 Functions</div></div>
    %s
    %s
    %s
</div>
</div>

%s

<div class="card">
<h2>📚 Tech Stack</h2>
<h3>Technologies</h3>
<div class="tag-cloud">%s</div>
<h3 style="margin-top:20px">Microservices <span class="count">(%d)</span></h3>
<div class="pkg-grid">%s</div>
<h3 style="margin-top:24px">Architecture</h3>
<div id="arch-graph" class="arch-graph-container"></div>
</div>

<div class="card">
<h2>🔗 Microservices Penetration</h2>
%s
<h3 style="margin-top:24px">📝 TODO / FIXME</h3>
%s
</div>

%s

%s

<div class="card">
<h2>🔧 Microservices</h2>
<p class="subtitle">Graphs: cross-file references &amp; co-location. <span style="color:#34c759">●</span> struct <span style="color:#007aff">●</span> interface <span style="color:#ff9500">●</span> func/message <span style="color:#ff3b30">●</span> service <span style="color:#af52de">●</span> enum</p>
%s
</div>

<footer style="text-align:center;padding:20px 0 10px;color:var(--text3);font-size:12px;">Generator: <strong>goscope</strong> · MIT License</footer>
</div>

<script>
// Architecture graph
{
const d=%s;
const el=document.getElementById('arch-graph');
if(d.nodes.length>0&&el){
const kc={'microservice':'#007aff','technology':'#34c759','foreign':'#ff9500'};
const g=ForceGraph()(el).graphData(d)
.nodeLabel(n=>n.label+'\n'+n.kind)
.nodeVal(n=>n.kind==='microservice'||n.kind==='foreign'?10:5)
.nodeColor(n=>kc[n.kind]||'#999')
.nodeCanvasObject((node,ctx,gs)=>{
const r=node.kind==='technology'?5:7;
ctx.beginPath();ctx.arc(node.x,node.y,r,0,2*Math.PI);
ctx.fillStyle=kc[node.kind]||'#999';ctx.fill();
if(gs>0.3){
ctx.font=(Math.max(10/gs,3))+'px -apple-system,sans-serif';
ctx.textAlign='center';ctx.fillStyle=node.kind==='technology'?'#666':'#1d1d1f';
ctx.fillText(node.label,node.x,node.y+r+12/gs);}})
.linkColor(()=>'rgba(0,0,0,0.08)')
.linkWidth(1.5)
.width(el.offsetWidth).height(500)
.onEngineStop(()=>g.zoomToFit(400,40));
g.d3Force('charge').strength(-250);g.d3Force('link').distance(100);
}}
// MS graphs
%s
</script>
</body>
</html>`,
		esc(projDisplay),
		esc(projDisplay),
		time.Now().Format("2006-01-02 15:04:05"),
		esc(branchName),
		subdirDisplay,
		// Summary cards
		totalMSCount,
		totalGoFiles,
		fmtNum(totalLines),
		totalStructs+totalInterfaces+totalFuncs+totalMessages+totalServices+totalRPCs+totalEnums,
		todoCard,
		totalStructs, totalInterfaces, totalEnums, totalFuncs,
		protoCard,
		grpcCard,
		foreignLangCards,
		// Team (only if git data exists)
		func() string {
			if len(teamEntries) == 0 {
				return ""
			}
			return fmt.Sprintf(`<div class="card">
<h2>👥 Team Contribution Map</h2>
<div class="table-wrap"><table class="team-table">
<thead><tr><th>Developer</th><th>Files Modified</th><th>Commits</th><th>First Change</th><th>Last Change</th><th>Top-3 Microservices</th></tr></thead>
<tbody>%s</tbody>
</table></div>
</div>`, teamRows.String())
		}(),
		// Tech Stack
		techTags,
		totalMSCount,
		msGridHTML.String(),
		// Penetration
		func() string {
			if len(penList) == 0 {
				return ""
			}
			return fmt.Sprintf(`<div class="table-wrap"><table class="file-table">
<thead><tr><th>Microservice</th><th>Used by</th><th>Dependent Microservices</th></tr></thead>
<tbody>%s</tbody>
</table></div>`, penRows.String())
		}(),
		func() string {
			if len(todoList) == 0 {
				return `<p style="color:var(--text3)">No TODO or FIXME comments found.</p>`
			}
			return fmt.Sprintf(`<div class="table-wrap"><table class="file-table">
<thead><tr><th>Microservice</th><th>TODO</th><th>FIXME</th><th>Total</th></tr></thead>
<tbody>%s</tbody>
</table></div>`, todoRows.String())
		}(),
		// Hot Zones
		func() string {
			if hotspotCount == 0 {
				return ""
			}
			return fmt.Sprintf(`<div class="card">
<h2>🔥 Hot Zones</h2>
<p class="subtitle">Files with the highest dependency score (PageRank). These are the most interconnected files in the codebase.</p>
<div class="table-wrap"><table class="file-table">
<thead><tr><th>File</th><th>Score</th><th>Lines</th><th>Decl</th><th>Microservice</th></tr></thead>
<tbody>%s</tbody>
</table></div>
</div>`, hotspotRows.String())
		}(),
		// Longest functions
		func() string {
			if len(allFuncs) == 0 {
				return ""
			}
			return fmt.Sprintf(`<div class="card">
<h2>📏 Longest Functions</h2>
<div class="table-wrap"><table class="file-table">
<thead><tr><th>Function</th><th>Lines</th><th>File</th><th>Microservice</th></tr></thead>
<tbody>%s</tbody>
</table></div>
</div>`, funcRows.String())
		}(),
		// Microservice sections
		msSections.String(),
		// Architecture graph JSON
		string(archGraphJSON),
		// MS graph scripts
		msGraphScripts.String(),
	)

	dir := filepath.Dir(outputPath)
	os.MkdirAll(dir, 0755)
	if err := os.WriteFile(outputPath, []byte(html), 0644); err != nil {
		return err
	}
	fmt.Printf("   HTML written (%dKB)\n", len(html)/1024)
	return nil
}

// buildArchitectureGraph builds a graph with all microservices + technologies.
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

	if nodes == nil {
		nodes = make([]gNode, 0)
	}
	if links == nil {
		links = make([]gLink, 0)
	}
	return gData{Nodes: nodes, Links: links}
}

func matchGoTypeRef(source, typeName string) bool {
	tl := len(typeName)
	sl := len(source)
	for i := 0; i <= sl-tl; i++ {
		if source[i:i+tl] != typeName {
			continue
		}
		if i > 0 && isIdentChar(source[i-1]) {
			continue
		}
		after := i + tl
		if after < sl && isIdentChar(source[after]) {
			continue
		}
		return true
	}
	return false
}

func isIdentChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

func isAPIGateway(n string) bool {
	l := strings.ToLower(n)
	return strings.Contains(l, "gateway") || strings.Contains(l, "api-gw") || l == "api"
}
func isProtoMS(n string) bool {
	l := strings.ToLower(n)
	return l == "proto" || l == "protobuf" || l == "protos" || strings.HasPrefix(l, "proto-") || strings.HasSuffix(l, "-proto")
}

func detectTechFromImport(imp string, techSet map[string]bool) {
	m := map[string]string{
		"google.golang.org/grpc": "gRPC", "google.golang.org/protobuf": "Protocol Buffers",
		"github.com/gin-gonic/gin": "Gin", "github.com/labstack/echo": "Echo",
		"github.com/gofiber/fiber": "Fiber", "github.com/gorilla/mux": "Gorilla Mux",
		"github.com/go-chi/chi": "Chi",
		"gorm.io/gorm": "GORM", "gorm.io/driver/postgres": "PostgreSQL",
		"github.com/jmoiron/sqlx": "sqlx",
		"github.com/jackc/pgx": "PostgreSQL", "github.com/lib/pq": "PostgreSQL",
		"github.com/go-redis/redis": "Redis", "github.com/redis/go-redis": "Redis",
		"go.mongodb.org/mongo-driver": "MongoDB",
		"github.com/segmentio/kafka-go": "Kafka", "github.com/IBM/sarama": "Kafka",
		"github.com/nats-io/nats.go": "NATS",
		"github.com/streadway/amqp": "RabbitMQ", "github.com/rabbitmq/amqp091-go": "RabbitMQ",
		"go.uber.org/zap": "Zap Logger", "github.com/sirupsen/logrus": "Logrus", "log/slog": "slog",
		"go.opentelemetry.io/otel": "OpenTelemetry",
		"github.com/prometheus/client_golang": "Prometheus",
		"github.com/elastic/go-elasticsearch": "Elasticsearch",
		"github.com/ClickHouse/clickhouse-go": "ClickHouse",
		"github.com/minio/minio-go": "MinIO",
		"github.com/aws/aws-sdk-go": "AWS SDK", "github.com/aws/aws-sdk-go-v2": "AWS SDK",
		"cloud.google.com/go": "Google Cloud",
		"k8s.io/client-go": "Kubernetes Client",
		"github.com/hashicorp/consul": "Consul", "github.com/hashicorp/vault": "HashiCorp Vault",
		"go.etcd.io/etcd": "etcd",
		"github.com/golang-jwt/jwt": "JWT", "github.com/spf13/cobra": "Cobra CLI",
		"github.com/spf13/viper": "Viper Config",
		"github.com/grpc-ecosystem/grpc-gateway": "gRPC Gateway",
		"github.com/99designs/gqlgen": "gqlgen (GraphQL)",
		"github.com/golang-migrate/migrate": "DB Migrations", "github.com/pressly/goose": "Goose Migrations",
		"github.com/swaggo/swag": "Swagger", "github.com/stretchr/testify": "Testify",
	}
	for prefix, tech := range m {
		if strings.HasPrefix(imp, prefix) {
			techSet[tech] = true
			return
		}
	}
}

func topNKeys(m map[string]int, n int) []string {
	type kv struct {
		K string
		V int
	}
	var s []kv
	for k, v := range m {
		s = append(s, kv{k, v})
	}
	sort.Slice(s, func(i, j int) bool { return s[i].V > s[j].V })
	var r []string
	for i, x := range s {
		if i >= n {
			break
		}
		r = append(r, x.K)
	}
	return r
}

func kindIcon(k parser.DeclKind) string {
	switch k {
	case parser.DeclStruct:
		return "🟢"
	case parser.DeclInterface:
		return "🔵"
	case parser.DeclFunc:
		return "🟠"
	case parser.DeclMessage:
		return "📨"
	case parser.DeclService:
		return "🔴"
	case parser.DeclRPC:
		return "🔗"
	case parser.DeclEnum:
		return "🟡"
	default:
		return "⚪"
	}
}

// splitDirFile splits "internal/domain/personal_data.go" into ("internal/domain/", "personal_data.go").
func splitDirFile(path string) (dir, file string) {
	// Normalize to forward slashes for display
	path = strings.ReplaceAll(path, "\\", "/")
	idx := strings.LastIndex(path, "/")
	if idx >= 0 {
		return path[:idx+1], path[idx+1:]
	}
	return "", path
}

// shortRelPath builds a display path like "internal/domain/resp/types.go"
// by finding the microservice name in the path and showing what comes after it.
func shortRelPath(fullPath, msName string) string {
	sep := string(filepath.Separator)
	// Find microservice name in path
	idx := strings.Index(fullPath, sep+msName+sep)
	if idx >= 0 {
		rel := fullPath[idx+len(sep)+len(msName)+len(sep):]
		return rel
	}
	// Fallback: show last 3 path segments
	parts := strings.Split(fullPath, sep)
	if len(parts) > 3 {
		return strings.Join(parts[len(parts)-3:], "/")
	}
	return filepath.Base(fullPath)
}

func esc(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

func fmtNum(n int) string {
	s := fmt.Sprintf("%d", n)
	if n < 1000 {
		return s
	}
	var r []byte
	for i, ch := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			r = append(r, ',')
		}
		r = append(r, byte(ch))
	}
	return string(r)
}
