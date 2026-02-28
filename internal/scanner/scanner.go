package scanner

import (
	"bufio"
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

// ScanDockerCompose reads docker-compose.yml from root and all subdirs.
func ScanDockerCompose(rootPath string) (services []string, technologies []string) {
	techSet := make(map[string]bool)
	svcSet := make(map[string]bool)

	searchDirs := []string{rootPath}
	entries, _ := os.ReadDir(rootPath)
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			searchDirs = append(searchDirs, filepath.Join(rootPath, e.Name()))
		}
	}

	candidates := []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"}

	for _, dir := range searchDirs {
		for _, name := range candidates {
			svcs, techs := parseDockerCompose(filepath.Join(dir, name))
			for _, s := range svcs {
				svcSet[s] = true
			}
			for _, t := range techs {
				techSet[t] = true
			}
		}
		for _, t := range scanGoMod(filepath.Join(dir, "go.mod")) {
			techSet[t] = true
		}
		for _, t := range scanMakefile(filepath.Join(dir, "Makefile")) {
			techSet[t] = true
		}
	}

	for s := range svcSet {
		services = append(services, s)
	}
	sort.Strings(services)
	for t := range techSet {
		technologies = append(technologies, t)
	}
	sort.Strings(technologies)
	return
}

func parseDockerCompose(path string) (services []string, technologies []string) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	lines := strings.Split(string(content), "\n")
	inServices := false
	indent := 0
	techSet := make(map[string]bool)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "services:" {
			inServices = true
			indent = countLeadingSpaces(line)
			continue
		}
		if inServices && len(trimmed) > 0 && !strings.HasPrefix(trimmed, "#") {
			lineIndent := countLeadingSpaces(line)
			if lineIndent == indent+2 && strings.HasSuffix(trimmed, ":") {
				services = append(services, strings.TrimSuffix(trimmed, ":"))
			}
			if strings.Contains(trimmed, "image:") {
				img := strings.TrimSpace(strings.TrimPrefix(trimmed, "image:"))
				img = strings.Trim(img, "\"'")
				if tech := extractTechFromImage(img); tech != "" {
					techSet[tech] = true
				}
			}
			upper := strings.ToUpper(trimmed)
			if strings.Contains(upper, "POSTGRES") || strings.Contains(upper, "PGHOST") || strings.Contains(trimmed, "5432") {
				techSet["PostgreSQL"] = true
			}
			if strings.Contains(upper, "REDIS") || strings.Contains(trimmed, "6379") {
				techSet["Redis"] = true
			}
			if strings.Contains(upper, "MONGO") || strings.Contains(trimmed, "27017") {
				techSet["MongoDB"] = true
			}
			if strings.Contains(upper, "KAFKA") || strings.Contains(trimmed, "9092") {
				techSet["Kafka"] = true
			}
			if strings.Contains(upper, "RABBIT") || strings.Contains(trimmed, "5672") {
				techSet["RabbitMQ"] = true
			}
		}
	}
	for t := range techSet {
		technologies = append(technologies, t)
	}
	return
}

func scanGoMod(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	techMap := map[string]string{
		"github.com/jackc/pgx":                      "PostgreSQL",
		"github.com/lib/pq":                         "PostgreSQL",
		"gorm.io/driver/postgres":                   "PostgreSQL",
		"github.com/go-redis/redis":                 "Redis",
		"github.com/redis/go-redis":                 "Redis",
		"go.mongodb.org/mongo-driver":               "MongoDB",
		"github.com/segmentio/kafka-go":             "Kafka",
		"github.com/IBM/sarama":                     "Kafka",
		"github.com/Shopify/sarama":                 "Kafka",
		"github.com/nats-io/nats.go":                "NATS",
		"github.com/streadway/amqp":                 "RabbitMQ",
		"github.com/rabbitmq/amqp091-go":            "RabbitMQ",
		"google.golang.org/grpc":                    "gRPC",
		"google.golang.org/protobuf":                "Protocol Buffers",
		"github.com/gin-gonic/gin":                  "Gin",
		"github.com/labstack/echo":                  "Echo",
		"github.com/gofiber/fiber":                  "Fiber",
		"github.com/go-chi/chi":                     "Chi",
		"github.com/gorilla/mux":                    "Gorilla Mux",
		"gorm.io/gorm":                              "GORM",
		"github.com/jmoiron/sqlx":                   "sqlx",
		"go.uber.org/zap":                           "Zap Logger",
		"github.com/sirupsen/logrus":                "Logrus",
		"go.opentelemetry.io/otel":                  "OpenTelemetry",
		"github.com/prometheus/client_golang":       "Prometheus",
		"github.com/elastic/go-elasticsearch":       "Elasticsearch",
		"github.com/ClickHouse/clickhouse-go":       "ClickHouse",
		"github.com/minio/minio-go":                 "MinIO",
		"github.com/aws/aws-sdk-go":                 "AWS SDK",
		"github.com/aws/aws-sdk-go-v2":              "AWS SDK",
		"cloud.google.com/go":                       "Google Cloud",
		"k8s.io/client-go":                          "Kubernetes Client",
		"github.com/hashicorp/consul":               "Consul",
		"github.com/hashicorp/vault":                "HashiCorp Vault",
		"go.etcd.io/etcd":                           "etcd",
		"github.com/golang-jwt/jwt":                 "JWT",
		"github.com/spf13/cobra":                    "Cobra CLI",
		"github.com/spf13/viper":                    "Viper Config",
		"github.com/grpc-ecosystem/grpc-gateway":    "gRPC Gateway",
		"github.com/99designs/gqlgen":               "gqlgen (GraphQL)",
		"github.com/golang-migrate/migrate":         "DB Migrations",
		"github.com/pressly/goose":                  "Goose Migrations",
		"github.com/swaggo/swag":                    "Swagger",
		"github.com/stretchr/testify":               "Testify",
		"github.com/docker/docker":                  "Docker SDK",
	}

	seen := make(map[string]bool)
	var techs []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		for prefix, tech := range techMap {
			if strings.HasPrefix(line, prefix) && !seen[tech] {
				techs = append(techs, tech)
				seen[tech] = true
			}
		}
	}
	return techs
}

func scanMakefile(path string) []string {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	s := strings.ToLower(string(content))
	hints := map[string]string{
		"protoc": "Protocol Buffers", "grpc": "gRPC", "postgres": "PostgreSQL",
		"psql": "PostgreSQL", "redis-cli": "Redis", "mongo": "MongoDB",
		"kafka": "Kafka", "rabbitmq": "RabbitMQ", "nats": "NATS",
		"docker": "Docker", "kubectl": "Kubernetes", "helm": "Helm",
		"swagger": "Swagger", "migrate": "DB Migrations",
	}
	seen := make(map[string]bool)
	var techs []string
	for kw, tech := range hints {
		if strings.Contains(s, kw) && !seen[tech] {
			techs = append(techs, tech)
			seen[tech] = true
		}
	}
	return techs
}

func countLeadingSpaces(s string) int {
	n := 0
	for _, ch := range s {
		if ch == ' ' {
			n++
		} else if ch == '\t' {
			n += 2
		} else {
			break
		}
	}
	return n
}

func extractTechFromImage(image string) string {
	imgLower := strings.ToLower(image)
	techMap := map[string]string{
		"postgres": "PostgreSQL", "postgresql": "PostgreSQL",
		"mysql": "MySQL", "mariadb": "MariaDB",
		"mongo": "MongoDB", "redis": "Redis", "memcached": "Memcached",
		"rabbitmq": "RabbitMQ", "kafka": "Kafka", "zookeeper": "Zookeeper",
		"elasticsearch": "Elasticsearch", "opensearch": "OpenSearch",
		"kibana": "Kibana", "grafana": "Grafana", "prometheus": "Prometheus",
		"jaeger": "Jaeger", "nginx": "NGINX", "envoy": "Envoy",
		"consul": "Consul", "vault": "HashiCorp Vault", "nats": "NATS",
		"etcd": "etcd", "minio": "MinIO", "clickhouse": "ClickHouse",
		"influxdb": "InfluxDB", "temporal": "Temporal", "keycloak": "Keycloak",
		"traefik": "Traefik", "caddy": "Caddy", "localstack": "LocalStack",
		"cassandra": "Cassandra",
	}
	for key, tech := range techMap {
		if strings.Contains(imgLower, key) {
			return tech
		}
	}
	return ""
}
