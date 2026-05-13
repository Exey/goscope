package report

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/goscope/internal/parser"
)

const (
	LayerAPI      = "API / Routes"
	LayerModels   = "Models / Schemas"
	LayerServices = "Services / Domain"
	LayerPersist  = "Persistence / DB"
	LayerAuth     = "Auth / Security"
	LayerTasks    = "Background Tasks"
	LayerConfig   = "Config / Settings"
	LayerCLI      = "CLI / Entry"
	LayerInfra    = "Infrastructure / Utils"
	LayerTests    = "Tests"
	LayerOther    = "Other"
)

var layerIcons = map[string]string{
	LayerAPI:      "🌐",
	LayerModels:   "📦",
	LayerServices: "⚙️",
	LayerPersist:  "🗄️",
	LayerAuth:     "🔐",
	LayerTasks:    "⏱️",
	LayerConfig:   "🔧",
	LayerCLI:      "💻",
	LayerInfra:    "🧰",
	LayerTests:    "🧪",
	LayerOther:    "•",
}

var layerOrder = []string{
	LayerAPI, LayerModels, LayerServices, LayerPersist, LayerAuth,
	LayerTasks, LayerConfig, LayerCLI, LayerInfra, LayerTests, LayerOther,
}

type archComponent struct {
	Name    string
	Summary string
	Icon    string
}

func classifyGoFile(f *parser.ParsedFile) string {
	path := strings.ReplaceAll(f.FilePath, "\\", "/")
	name := strings.ToLower(filepath.Base(path))
	parts := strings.Split(strings.ToLower(path), "/")

	// Tests
	if strings.HasSuffix(name, "_test.go") {
		return LayerTests
	}
	for _, p := range parts {
		if p == "test" || p == "tests" || p == "testdata" {
			return LayerTests
		}
	}

	// Proto files → API (gRPC service definitions)
	if f.FileType == "proto" {
		return LayerAPI
	}

	// CLI / Entry
	if name == "main.go" {
		return LayerCLI
	}
	for _, p := range parts {
		if p == "cmd" || p == "cli" || p == "command" || p == "commands" {
			return LayerCLI
		}
	}
	if name == "cli.go" || name == "cmd.go" {
		return LayerCLI
	}

	// Config
	for _, p := range parts {
		if p == "config" || p == "conf" || p == "configuration" || p == "settings" || p == "constants" {
			return LayerConfig
		}
	}
	if name == "config.go" || name == "settings.go" || name == "constants.go" || name == "conf.go" {
		return LayerConfig
	}

	// Auth / Security
	for _, p := range parts {
		if p == "auth" || p == "authentication" || p == "authorization" || p == "security" || p == "jwt" {
			return LayerAuth
		}
	}
	if name == "auth.go" || name == "jwt.go" || name == "security.go" || name == "oauth.go" {
		return LayerAuth
	}

	// API / Routes
	for _, p := range parts {
		if p == "handler" || p == "handlers" || p == "controller" || p == "controllers" ||
			p == "api" || p == "apis" || p == "route" || p == "routes" || p == "router" ||
			p == "endpoint" || p == "endpoints" || p == "http" || p == "rest" || p == "transport" {
			return LayerAPI
		}
	}
	if name == "handler.go" || name == "handlers.go" || name == "router.go" || name == "routes.go" ||
		name == "controller.go" || name == "controllers.go" || name == "endpoint.go" || name == "endpoints.go" {
		return LayerAPI
	}

	// Models / Schemas
	for _, p := range parts {
		if p == "model" || p == "models" || p == "entity" || p == "entities" ||
			p == "domain" || p == "schema" || p == "schemas" || p == "dto" || p == "dtos" {
			return LayerModels
		}
	}
	if name == "model.go" || name == "models.go" || name == "entity.go" ||
		name == "schema.go" || name == "dto.go" || name == "types.go" {
		return LayerModels
	}

	// Services / Domain
	for _, p := range parts {
		if p == "service" || p == "services" || p == "usecase" || p == "usecases" ||
			p == "use_case" || p == "use_cases" || p == "business" || p == "core" || p == "logic" || p == "app" {
			return LayerServices
		}
	}
	if name == "service.go" || name == "services.go" || name == "usecase.go" || name == "core.go" {
		return LayerServices
	}

	// Persistence / DB
	for _, p := range parts {
		if p == "repository" || p == "repo" || p == "repos" || p == "storage" ||
			p == "db" || p == "database" || p == "dao" || p == "migration" || p == "migrations" || p == "store" {
			return LayerPersist
		}
	}
	if name == "repository.go" || name == "repo.go" || name == "storage.go" ||
		name == "db.go" || name == "database.go" || name == "dao.go" || name == "migration.go" {
		return LayerPersist
	}

	// Background Tasks
	for _, p := range parts {
		if p == "worker" || p == "workers" || p == "task" || p == "tasks" ||
			p == "job" || p == "jobs" || p == "consumer" || p == "consumers" ||
			p == "queue" || p == "queues" || p == "scheduler" || p == "cron" {
			return LayerTasks
		}
	}
	if name == "worker.go" || name == "consumer.go" || name == "scheduler.go" || name == "cron.go" || name == "job.go" {
		return LayerTasks
	}

	// Infrastructure / Utils
	for _, p := range parts {
		if p == "util" || p == "utils" || p == "helper" || p == "helpers" ||
			p == "common" || p == "shared" || p == "lib" || p == "middleware" ||
			p == "infrastructure" || p == "infra" {
			return LayerInfra
		}
	}
	if name == "util.go" || name == "utils.go" || name == "helper.go" || name == "helpers.go" ||
		name == "common.go" || name == "errors.go" || name == "error.go" || name == "middleware.go" {
		return LayerInfra
	}

	return LayerOther
}

func buildArchLayersHTML(files []*parser.ParsedFile) string {
	type layerBucket struct {
		FileCount int
		LineCount int
	}
	buckets := make(map[string]*layerBucket)
	for _, f := range files {
		layer := classifyGoFile(f)
		b := buckets[layer]
		if b == nil {
			b = &layerBucket{}
			buckets[layer] = b
		}
		b.FileCount++
		b.LineCount += f.LineCount
	}

	maxLines := 1
	for _, b := range buckets {
		if b.LineCount > maxLines {
			maxLines = b.LineCount
		}
	}

	var sb strings.Builder
	sb.WriteString(`<div class="arch-layers">`)
	for _, layer := range layerOrder {
		b := buckets[layer]
		if b == nil {
			continue
		}
		icon := layerIcons[layer]
		pct := b.LineCount * 100 / maxLines
		if pct < 4 {
			pct = 4
		}
		sb.WriteString(fmt.Sprintf(
			`<div class="arch-layer"><div class="layer-bar-row"><span class="layer-icon">%s</span><span class="layer-name">%s</span><span class="layer-count">%d files · %s loc</span></div><div class="layer-bar-track"><div class="layer-bar-fill" style="width:%d%%"></div></div></div>`,
			icon, esc(layer), b.FileCount, fmtNum(b.LineCount), pct,
		))
	}
	sb.WriteString(`</div>`)
	return sb.String()
}

func detectGoComponents(files []*parser.ParsedFile, techSet map[string]bool, totalServices, totalRPCs int) []archComponent {
	testFileCount := 0
	for _, f := range files {
		if classifyGoFile(f) == LayerTests {
			testFileCount++
		}
	}

	var out []archComponent
	add := func(name, icon, summary string) {
		out = append(out, archComponent{Name: name, Icon: icon, Summary: summary})
	}

	// HTTP frameworks
	if techSet["Gin"] {
		add("Gin", "🌐", "Gin HTTP server")
	}
	if techSet["Echo"] {
		add("Echo", "🌐", "Echo HTTP server")
	}
	if techSet["Fiber"] {
		add("Fiber", "🌐", "Fiber HTTP server")
	}
	if techSet["Chi"] {
		add("Chi", "🌐", "Chi router")
	}
	if techSet["Gorilla Mux"] {
		add("Gorilla Mux", "🌐", "Gorilla Mux router")
	}

	// gRPC
	if techSet["gRPC"] {
		s := "gRPC"
		if totalServices > 0 {
			s = fmt.Sprintf("gRPC · %d services", totalServices)
		}
		if totalRPCs > 0 {
			s += fmt.Sprintf(" · %d RPCs", totalRPCs)
		}
		add("gRPC", "📡", s)
	}
	if techSet["gRPC Gateway"] {
		add("gRPC Gateway", "🌐", "gRPC Gateway")
	}
	if techSet["gqlgen (GraphQL)"] {
		add("GraphQL", "⬡", "gqlgen GraphQL")
	}
	if techSet["Swagger"] {
		add("Swagger", "📖", "Swagger API docs")
	}

	// ORMs / DB clients
	if techSet["GORM"] {
		add("GORM", "🗄️", "GORM ORM")
	}
	if techSet["sqlx"] {
		add("sqlx", "🗄️", "sqlx")
	}
	if techSet["DB Migrations"] {
		add("Migrations", "🪣", "DB Migrations")
	}
	if techSet["Goose Migrations"] {
		add("Goose", "🪣", "Goose Migrations")
	}

	// Messaging / streaming
	if techSet["Kafka"] {
		add("Kafka", "📨", "Kafka producer/consumer")
	}
	if techSet["RabbitMQ"] {
		add("RabbitMQ", "🐇", "RabbitMQ messaging")
	}
	if techSet["NATS"] {
		add("NATS", "📡", "NATS messaging")
	}

	// Cache / KV
	if techSet["Redis"] {
		add("Redis", "🟥", "Redis client")
	}

	// Databases
	if techSet["PostgreSQL"] {
		add("PostgreSQL", "🐘", "PostgreSQL")
	}
	if techSet["MongoDB"] {
		add("MongoDB", "🍃", "MongoDB")
	}
	if techSet["Elasticsearch"] {
		add("Elasticsearch", "🔍", "Elasticsearch")
	}
	if techSet["ClickHouse"] {
		add("ClickHouse", "📊", "ClickHouse")
	}
	if techSet["MinIO"] {
		add("MinIO", "🪣", "MinIO object storage")
	}

	// Auth
	if techSet["JWT"] {
		add("JWT", "🔐", "JWT authentication")
	}

	// Observability
	if techSet["OpenTelemetry"] {
		add("OpenTelemetry", "📡", "OpenTelemetry")
	}
	if techSet["Prometheus"] {
		add("Prometheus", "📡", "Prometheus metrics")
	}

	// CLI / config
	if techSet["Cobra CLI"] {
		add("Cobra", "💻", "Cobra CLI")
	}
	if techSet["Viper Config"] {
		add("Viper", "🔧", "Viper Config")
	}

	// Cloud / infra
	if techSet["AWS SDK"] {
		add("AWS SDK", "☁️", "AWS SDK")
	}
	if techSet["Google Cloud"] {
		add("Google Cloud", "☁️", "Google Cloud")
	}
	if techSet["Kubernetes Client"] {
		add("Kubernetes", "☸️", "Kubernetes Client")
	}
	if techSet["Consul"] {
		add("Consul", "🔗", "Consul")
	}
	if techSet["HashiCorp Vault"] {
		add("Vault", "🔐", "HashiCorp Vault")
	}
	if techSet["etcd"] {
		add("etcd", "🔗", "etcd")
	}

	// Testing
	if techSet["Testify"] {
		s := "Testify"
		if testFileCount > 0 {
			s = fmt.Sprintf("Testify · %d test files", testFileCount)
		}
		add("Testify", "🧪", s)
	}

	return out
}

func buildArchComponentsHTML(components []archComponent) string {
	if len(components) == 0 {
		return `<p style="color:var(--text3);font-size:13px">No components detected.</p>`
	}
	var sb strings.Builder
	sb.WriteString(`<div class="arch-components">`)
	for _, c := range components {
		sb.WriteString(fmt.Sprintf(
			`<span class="arch-component"><span class="comp-icon">%s</span><span>%s</span></span>`,
			c.Icon, esc(c.Summary),
		))
	}
	sb.WriteString(`</div>`)
	return sb.String()
}
