package scanner

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goscope/internal/config"
)

// ScanResult holds the scanned files grouped by microservice.
type ScanResult struct {
	Files         []string            // all file paths
	Microservices map[string][]string // microservice name -> file paths
	RootSubdirs   []string            // first-level subdirectories of the root
	GitRepos      []string            // paths to directories containing .git
}

// Scan walks the directory tree looking for Go/proto files.
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

	// Also check root-level .git
	if _, err := os.Stat(filepath.Join(rootPath, ".git")); err == nil {
		result.GitRepos = append([]string{rootPath}, result.GitRepos...)
	}

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
		if !extSet[ext] {
			return nil
		}
		result.Files = append(result.Files, path)
		ms := detectMicroservice(rootPath, path)
		result.Microservices[ms] = append(result.Microservices[ms], path)
		return nil
	})
	return result, err
}

// detectMicroservice infers the microservice name from the file path.
// For a multi-repo layout the first-level directory IS the microservice.
func detectMicroservice(rootPath, filePath string) string {
	rel, _ := filepath.Rel(rootPath, filePath)
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 2 {
		return "root"
	}

	// Multi-repo: first-level dir with .git / go.mod / Dockerfile is a repo
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
	servicesDirs := map[string]bool{"services": true, "service": true, "apps": true, "microservices": true, "svc": true}
	for i, p := range parts {
		if servicesDirs[p] && i+1 < len(parts) {
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
