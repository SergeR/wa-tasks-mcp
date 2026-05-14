package main

import (
	"bufio"
	"context"
	"crypto/subtle"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/you/wa-tasks-mcp/internal/mcpserver"
	"github.com/you/wa-tasks-mcp/internal/tracker"
)

func main() {
	loadDotEnv()

	// --- конфигурация из переменных окружения ---
	apiBase := os.Getenv("TRACKER_API_BASE")     // напр. https://tracker.example.com/api.php
	apiToken := os.Getenv("TRACKER_API_TOKEN")   // access_token для Webasyst
	mcpSecret := os.Getenv("MCP_BEARER_SECRET")  // общий секрет для аутентификации MCP-клиентов
	listenAddr := os.Getenv("MCP_LISTEN_ADDR")   // напр. 127.0.2.34:7777
	if listenAddr == "" {
		listenAddr = "127.0.0.1:7777"
	}
	if apiBase == "" || apiToken == "" {
		log.Fatal("TRACKER_API_BASE and TRACKER_API_TOKEN are required")
	}
	if mcpSecret == "" {
		log.Fatal("MCP_BEARER_SECRET is required (use any long random string)")
	}

	tc := tracker.New(apiBase, apiToken)
	srv := mcpserver.New(tc)

	// Streamable HTTP handler из SDK. Обрабатывает GET и POST на одном
	// эндпоинте, включая SSE-стримы.
	handler := mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server { return srv },
		nil,
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", authMiddleware(mcpSecret, handler))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	httpSrv := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("wa-tasks-mcp listening on http://%s/mcp", listenAddr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	// graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Println("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
}

// loadDotEnv читает .env из рабочей директории и устанавливает переменные,
// которые ещё не заданы в окружении. Отсутствие файла не является ошибкой.
func loadDotEnv() {
	f, err := os.Open(".env")
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
			val = val[1 : len(val)-1]
		}
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}

// authMiddleware проверяет Bearer-токен. Защищает MCP-сервер от случайных
// обращений от других процессов на той же машине.
func authMiddleware(secret string, next http.Handler) http.Handler {
	expected := "Bearer " + secret
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("Authorization")
		if subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="wa-tasks-mcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
