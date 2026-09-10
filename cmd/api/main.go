package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"notification-engine/internal/config"
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
	defer cancelPing()
	if err := redisClient.Ping(ctxPing).Err(); err != nil {
		log.Fatalf("[api] falha ao conectar no Redis (%s): %v", cfg.RedisAddr, err)
	}
	log.Printf("[api] conectado ao Redis em %s", cfg.RedisAddr)

	producer := queue.NewRedisProducer(redisClient, cfg.RedisStreamName)
	notificationService := service.NewNotificationService(producer)
	notificationHandler := handler.NewNotificationHandler(notificationService)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", notificationHandler.HealthCheck)
	mux.HandleFunc("POST /api/v1/notifications", notificationHandler.Create)

	server := &http.Server{
		Addr:         ":" + cfg.APIPort,
		Handler:      mux,
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
