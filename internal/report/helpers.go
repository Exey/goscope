package report

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goscope/internal/parser"
)

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
