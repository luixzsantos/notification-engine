package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"notification-engine/internal/config"
	"notification-engine/internal/db"
	"notification-engine/internal/domain"
	"notification-engine/internal/handler"
	"notification-engine/internal/queue"
	"notification-engine/internal/service"
)

func main() {
	cfg := config.Load()

	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	ctxPing, cancelPing := context.WithTimeout(context.Background(), 5*time.Second)
	if err := redisClient.Ping(ctxPing).Err(); err != nil {
		cancelPing()
		log.Fatalf("[api] falha ao conectar no Redis (%s): %v", cfg.RedisAddr, err)
	}
	cancelPing()
	log.Printf("[api] conectado ao Redis em %s", cfg.RedisAddr)

	dsn := db.BuildDSN(cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBSSLMode)
	conn, err := db.Connect(dsn, cfg.DBMaxOpenConns)
	if err != nil {
		log.Fatalf("[api] falha ao conectar no PostgreSQL (%s:%s): %v", cfg.DBHost, cfg.DBPort, err)
	}
	defer conn.Close()
	if err := db.EnsureSchema(conn); err != nil {
		log.Fatalf("[api] falha ao aplicar schema do banco: %v", err)
	}
	log.Printf("[api] conectado ao PostgreSQL em %s:%s", cfg.DBHost, cfg.DBPort)

	repo := db.NewNotificationRepository(conn)
	producer := queue.NewRedisProducer(redisClient, cfg.RedisStreamName)

	defaultTargets := map[domain.ChannelType]string{}
	if cfg.DefaultDiscordTarget != "" {
		defaultTargets[domain.ChannelDiscord] = cfg.DefaultDiscordTarget
	}
	if cfg.DefaultTelegramTarget != "" {
		defaultTargets[domain.ChannelTelegram] = cfg.DefaultTelegramTarget
	}
	if cfg.DefaultEmailTarget != "" {
		defaultTargets[domain.ChannelEmail] = cfg.DefaultEmailTarget
	}
	if cfg.DefaultWhatsAppTarget != "" {
		defaultTargets[domain.ChannelWhatsApp] = cfg.DefaultWhatsAppTarget
	}
	if cfg.DefaultOutlookTarget != "" {
		defaultTargets[domain.ChannelOutlook] = cfg.DefaultOutlookTarget
	}

	notificationService := service.NewNotificationService(
		producer, repo, defaultTargets,
		cfg.RetryMaxAttempts, cfg.RetryMaxBackoffSeconds, cfg.AllowPrivateNetworkTargets,
	)
	notificationHandler := handler.NewNotificationHandler(notificationService)

	if cfg.APIKey == "" {
		log.Println("[api] AVISO: API_KEY não configurada — endpoints de negócio estão sem autenticação (adequado só para uso local/dev)")
	}
	if len(cfg.CORSAllowedOrigins) == 1 && cfg.CORSAllowedOrigins[0] == "*" {
		log.Println("[api] AVISO: CORS_ALLOWED_ORIGINS não configurado — liberando qualquer origem (\"*\")")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", notificationHandler.HealthCheck) // alias legado, equivalente à liveness
	mux.HandleFunc("GET /health/live", notificationHandler.HealthCheck)
	mux.HandleFunc("GET /health/ready", readinessHandler(conn, redisClient))

	protected := requireAPIKey(cfg.APIKey)
	mux.Handle("POST /api/v1/notifications", protected(http.HandlerFunc(notificationHandler.Create)))
	mux.Handle("POST /api/v1/notifications/bulk", protected(http.HandlerFunc(notificationHandler.Bulk)))
	mux.Handle("GET /api/v1/notifications", protected(http.HandlerFunc(notificationHandler.List)))
	mux.Handle("GET /api/v1/notifications/{id}", protected(http.HandlerFunc(notificationHandler.Get)))
	mux.Handle("POST /api/v1/notifications/{id}/retry", protected(http.HandlerFunc(notificationHandler.Retry)))
	mux.Handle("GET /api/v1/stats", protected(http.HandlerFunc(notificationHandler.Stats)))

	if cfg.MetricsEnabled {
		mux.Handle("GET /metrics", promhttp.Handler())
	}

	server := &http.Server{
		Addr:         ":" + cfg.APIPort,
		Handler:      corsMiddleware(cfg.CORSAllowedOrigins)(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("[api] servidor HTTP escutando na porta %s", cfg.APIPort)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[api] erro fatal no servidor HTTP: %v", err)
		}
	}()

	// Graceful shutdown: aguarda SIGINT/SIGTERM antes de encerrar o processo.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("[api] sinal de encerramento recebido, finalizando com graceful shutdown...")

	ctxShutdown, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()

	if err := server.Shutdown(ctxShutdown); err != nil {
		log.Printf("[api] erro durante shutdown: %v", err)
	}
	if err := redisClient.Close(); err != nil {
		log.Printf("[api] erro ao fechar conexão com Redis: %v", err)
	}

	log.Println("[api] encerrado com sucesso")
}

// corsMiddleware aplica CORS restrito às origens configuradas em
// CORS_ALLOWED_ORIGINS. Se a lista for exatamente ["*"], qualquer origem é
// liberada (default, adequado para uso local/desenvolvimento).
func corsMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	allowAll := len(allowedOrigins) == 1 && allowedOrigins[0] == "*"
	originSet := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		originSet[o] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			switch {
			case allowAll:
				w.Header().Set("Access-Control-Allow-Origin", "*")
			case origin != "" && originSet[origin]:
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key, Idempotency-Key")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// requireAPIKey protege um handler exigindo "Authorization: Bearer <chave>"
// ou "X-API-Key: <chave>". Se apiKey estiver vazio (não configurado), a
// checagem é pulada por completo — modo aberto, só recomendado localmente.
func requireAPIKey(apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if apiKey == "" {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			provided := r.Header.Get("X-API-Key")
			if provided == "" {
				if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
					provided = strings.TrimPrefix(auth, "Bearer ")
				}
			}

			if provided == "" || provided != apiKey {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "não autorizado: informe uma API key válida via 'Authorization: Bearer <chave>' ou 'X-API-Key'"})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// readinessHandler confere se as dependências externas (PostgreSQL e
// Redis) estão realmente alcançáveis — diferente de /health (liveness, só
// confirma que o processo está de pé), usado por orquestradores para saber
// se a instância já pode receber tráfego.
func readinessHandler(conn interface{ PingContext(context.Context) error }, redisClient *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		dbErr := conn.PingContext(ctx)
		redisErr := redisClient.Ping(ctx).Err()

		status := http.StatusOK
		if dbErr != nil || redisErr != nil {
			status = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]any{
			"postgres": errString(dbErr),
			"redis":    errString(redisErr),
		})
	}
}

func errString(err error) string {
	if err == nil {
		return "ok"
	}
	return err.Error()
}
