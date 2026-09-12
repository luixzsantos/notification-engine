package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
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
	conn, err := db.Connect(dsn)
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

	notificationService := service.NewNotificationService(producer, repo, defaultTargets, cfg.RetryMaxAttempts)
	notificationHandler := handler.NewNotificationHandler(notificationService)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", notificationHandler.HealthCheck)
	mux.HandleFunc("POST /api/v1/notifications", notificationHandler.Create)
	mux.HandleFunc("POST /api/v1/notifications/bulk", notificationHandler.Bulk)
	mux.HandleFunc("GET /api/v1/notifications", notificationHandler.List)
	mux.HandleFunc("GET /api/v1/notifications/{id}", notificationHandler.Get)
	mux.HandleFunc("POST /api/v1/notifications/{id}/retry", notificationHandler.Retry)
	mux.HandleFunc("GET /api/v1/stats", notificationHandler.Stats)

	if cfg.MetricsEnabled {
		mux.Handle("GET /metrics", promhttp.Handler())
	}

	server := &http.Server{
		Addr:         ":" + cfg.APIPort,
		Handler:      corsMiddleware(mux),
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

// corsMiddleware libera chamadas vindas de qualquer origem (ex: a página
// main.html aberta direto do disco, file://) para poder
// chamar a API. Adequado para uso local/desenvolvimento.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
