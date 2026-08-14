package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/wiremesh/wiremesh/internal/control"
)

func main() {
	address := os.Getenv("WIREMESH_ADDR")
	if address == "" {
		address = ":8080"
	}

	masterKey := os.Getenv("WIREMESH_MASTER_KEY")
	if strings.TrimSpace(masterKey) == "" {
		// 兼容 Docker secrets 挂载：从文件读取根密钥
		if raw, readErr := os.ReadFile(strings.TrimSpace(os.Getenv("WIREMESH_MASTER_KEY_FILE"))); readErr == nil {
			masterKey = strings.TrimSpace(string(raw))
		}
	}
	if strings.TrimSpace(masterKey) == "" {
		log.Fatal("WIREMESH_MASTER_KEY is required: set it to a long random secret (used to encrypt private keys and sign session tokens). Generate one with: openssl rand -base64 32")
	}
	databaseDriver := strings.TrimSpace(os.Getenv("WIREMESH_DATABASE_DRIVER"))
	databaseDSN := strings.TrimSpace(os.Getenv("WIREMESH_DATABASE_DSN"))
	var store control.Store
	var database *control.DatabaseManager
	if databaseDriver != "" || databaseDSN != "" {
		if databaseDriver == "" {
			databaseDriver = "sqlite"
		}
		if databaseDSN == "" {
			if databaseDriver == "sqlite" || databaseDriver == "sqlite3" {
				databaseDSN = "file:wiremesh.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
			} else {
				log.Fatal("WIREMESH_DATABASE_DSN is required when WIREMESH_DATABASE_DRIVER is set")
			}
		}
		sqlStore, err := control.OpenSQLStore(databaseDriver, databaseDSN)
		if err != nil {
			// 驱动错误可能回显 DSN 中的凭据，日志前脱敏
			log.Fatalf("open database: %s", control.RedactCredentials(err.Error()))
		}
		defer sqlStore.Close()
		databaseDriver = sqlStore.Driver()
		store = sqlStore
	} else {
		configPath := envOrDefault("WIREMESH_DATABASE_CONFIG", "wiremesh-database.json")
		manager, err := control.NewDatabaseManager(configPath, masterKey)
		if err != nil {
			log.Fatalf("load database setup: %v", err)
		}
		database = manager
		defer database.Close()
		// Preserve existing installations that predate the database setup file.
		if !database.Status().Configured {
			legacyPath := filepath.Join(filepath.Dir(configPath), "wiremesh.db")
			if info, statErr := os.Stat(legacyPath); statErr == nil && !info.IsDir() {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				_, _, configureErr := database.Configure(ctx, control.DatabaseConfig{Driver: "sqlite", SQLitePath: "wiremesh.db"})
				cancel()
				if configureErr != nil {
					log.Fatalf("adopt legacy SQLite database: %v", configureErr)
				}
			}
		}
		store = database.Store()
		databaseDriver = database.Status().Driver
		if databaseDriver == "" {
			databaseDriver = "not configured"
		}
	}

	certFile, keyFile := os.Getenv("WIREMESH_TLS_CERT_FILE"), os.Getenv("WIREMESH_TLS_KEY_FILE")
	// H-4（P0）：初始化口令——未配置 WIREMESH_SETUP_TOKEN 时自动生成随机
	// 口令（写入日志与 wiremesh-setup-token 文件），杜绝全新实例被未认证
	// 抢先初始化接管；管理员从日志/文件获取口令完成首次配置。
	setupToken := strings.TrimSpace(os.Getenv("WIREMESH_SETUP_TOKEN"))
	if setupToken == "" {
		setupToken = control.GenerateSetupToken()
		tokenPath := envOrDefault("WIREMESH_SETUP_TOKEN_FILE", "wiremesh-setup-token")
		if err := os.WriteFile(tokenPath, []byte(setupToken+"\n"), 0600); err != nil {
			log.Printf("warning: could not persist setup token to %s: %v", tokenPath, err)
		}
		log.Printf("first-run setup token generated: %s (also saved to %s) — keep it secret; the setup wizard requires X-Setup-Token", setupToken, tokenPath)
	}
	app, err := control.NewApp(control.Config{
		MasterKey:       masterKey,
		Store:           store,
		Database:        database,
		DatabaseDriver:  databaseDriver,
		AgentBinaryPath: strings.TrimSpace(os.Getenv("WIREMESH_AGENT_BINARY")),
		AgentVersion:    strings.TrimSpace(os.Getenv("WIREMESH_AGENT_VERSION")),
		CAFile:          envOrDefault("WIREMESH_CA_FILE", "wiremesh-ca.json"),
		SetupToken:      setupToken,
		// 直接 TLS 监听时要求 Agent 携带有效客户端证书；仅当流量经过
		// 可信反向代理（由代理注入 X-Agent-ID）时设置 WIREMESH_TRUST_PROXY_AGENT_ID=true
		RequireAgentClientCert: certFile != "" && keyFile != "" && os.Getenv("WIREMESH_TRUST_PROXY_AGENT_ID") != "true",
		TrustProxyAgentID:      os.Getenv("WIREMESH_TRUST_PROXY_AGENT_ID") == "true",
		// 纯 HTTP 模式下 Agent 端点默认拒绝（X-Agent-ID 可伪造窃取私钥）；
		// 仅本地开发显式开启 WIREMESH_AGENT_INSECURE_HTTP=1
		AgentInsecureHTTP: os.Getenv("WIREMESH_AGENT_INSECURE_HTTP") == "1",
		// 可选：更新清单签名私钥（PEM ECDSA P-256），配置后清单携带签名，
		// Agent 端可用 --update-public-key 离线验证
		UpdateSigningKey: os.Getenv("WIREMESH_UPDATE_SIGNING_KEY"),
	})
	if err != nil {
		log.Fatal(err)
	}
	evaluatorCtx, evaluatorCancel := context.WithCancel(context.Background())
	defer evaluatorCancel()
	app.StartAlertEvaluator(evaluatorCtx)
	app.StartHousekeeping(evaluatorCtx)
	log.Printf("WireMesh control plane listening on %s", address)
	log.Printf("database driver: %s", databaseDriver)
	handler := withFrontend(app.Router(), os.Getenv("WIREMESH_WEB_DIR"))
	if certFile != "" && keyFile != "" {
		server := &http.Server{Addr: address, Handler: handler, TLSConfig: app.AgentTLSConfig()}
		log.Printf("agent mTLS verification enabled")
		if err := server.ListenAndServeTLS(certFile, keyFile); err != nil {
			log.Fatal(err)
		}
		return
	}
	log.Printf("WARNING: HTTP mode enables the development X-Agent-ID adapter; configure TLS for production")
	if err := http.ListenAndServe(address, handler); err != nil {
		log.Fatal(err)
	}
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func withFrontend(api http.Handler, directory string) http.Handler {
	if directory == "" {
		return api
	}
	files := http.FileServer(http.Dir(directory))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/metrics" || strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/agent/") {
			api.ServeHTTP(w, r)
			return
		}
		relative := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		// 含反斜杠的路径在 Windows 下可能让 filepath.Join 逃出 web 目录，
		// 直接回退到 index.html（前端路由由哈希模式接管）。
		if strings.Contains(relative, "\\") {
			http.ServeFile(w, r, filepath.Join(directory, "index.html"))
			return
		}
		candidate := filepath.Join(directory, filepath.FromSlash(relative))
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			if strings.HasPrefix(r.URL.Path, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(directory, "index.html"))
	})
}
