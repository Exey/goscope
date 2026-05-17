package report

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	gitpkg "github.com/goscope/internal/git"
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

func buildBranchManagementHTML(bs gitpkg.BranchStats) string {
	if bs.TotalBranches == 0 {
		return ""
	}

	fmtDays := func(d float64) string {
		if d <= 0 {
			return "—"
		}
		if d < 1 {
			return fmt.Sprintf("%.0fh", d*24)
		}
		return fmt.Sprintf("%.1f days", d)
	}
	fmtHours := func(h float64) string {
		if h <= 0 {
			return "—"
		}
		if h < 1 {
			return fmt.Sprintf("%.0fm", h*60)
		}
		if h < 24 {
			return fmt.Sprintf("%.1fh", h)
		}
		return fmt.Sprintf("%.1f days", h/24)
	}

	depthStr := "—"
	if bs.MaxDepth > 0 {
		depthStr = fmt.Sprintf("%d", bs.MaxDepth)
	}

	rollbackStr := "—"
	if bs.TotalMainCommits > 0 {
		rate := float64(bs.RollbackCount) * 100 / float64(bs.TotalMainCommits)
		rollbackStr = fmt.Sprintf("%.1f%%", rate)
	}

	var b strings.Builder
	b.WriteString(`<div class="bm-grid">`)
	b.WriteString(fmt.Sprintf(`<div class="bm-card"><div class="bm-value">%s</div><div class="bm-label">Avg Branch Lifetime</div></div>`, fmtDays(bs.AvgLifetimeDays)))
	b.WriteString(fmt.Sprintf(`<div class="bm-card"><div class="bm-value">%s</div><div class="bm-label">Avg Time to Merge</div></div>`, fmtDays(bs.AvgTTMDays)))
	b.WriteString(fmt.Sprintf(`<div class="bm-card"><div class="bm-value">%s</div><div class="bm-label">Integration Delay</div></div>`, fmtHours(bs.AvgIntegDelayHours)))
	b.WriteString(fmt.Sprintf(`<div class="bm-card"><div class="bm-value">%s</div><div class="bm-label">Branch Depth</div></div>`, depthStr))
	b.WriteString(fmt.Sprintf(`<div class="bm-card"><div class="bm-value">%s</div><div class="bm-label">Rollback Rate</div></div>`, rollbackStr))
	peakDay := bs.PeakCommitDay
	if peakDay == "" {
		peakDay = "—"
	}
	b.WriteString(fmt.Sprintf(`<div class="bm-card"><div class="bm-value">%s</div><div class="bm-label">Peak Commit Day</div></div>`, esc(peakDay)))
	b.WriteString(`</div>`)

	if bs.RollbackCount > 0 {
		b.WriteString(fmt.Sprintf(
			`<p style="font-size:12px;color:var(--text3);margin:0 0 12px">%d revert/rollback commits out of %s on main.</p>`,
			bs.RollbackCount, fmtNum(bs.TotalMainCommits),
		))
	}

	if len(bs.StaleBranches) > 0 {
		b.WriteString(fmt.Sprintf(
			`<h4 style="margin:16px 0 8px;font-size:14px;color:var(--text2)">Stale Branches <span style="font-weight:400;color:var(--text3);font-size:12px">(no activity &gt;%d days)</span></h4>`,
			bs.StaleThresholdDays,
		))
		b.WriteString(`<div class="table-wrap"><table class="file-table">`)
		b.WriteString(`<thead><tr><th>Branch</th><th>Days Inactive</th><th>Last Activity</th></tr></thead><tbody>`)
		for _, br := range bs.StaleBranches {
			lastAct := "—"
			if br.LastActivity > 0 {
				lastAct = time.Unix(int64(br.LastActivity), 0).Format("2006-01-02")
			}
			nameStyle := ""
			if br.DaysInactive > 90 {
				nameStyle = ` style="color:var(--red)"`
			} else if br.DaysInactive > 60 {
				nameStyle = ` style="color:#e65100"`
			}
			b.WriteString(fmt.Sprintf(
				`<tr><td class="mono"%s>%s</td><td class="mono">%d</td><td class="mono">%s</td></tr>`,
				nameStyle, esc(br.Name), br.DaysInactive, lastAct,
			))
		}
		b.WriteString(`</tbody></table></div>`)
	}

	return b.String()
}
