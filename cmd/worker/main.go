package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"notification-engine/internal/bot"
	"notification-engine/internal/channel"
	"notification-engine/internal/config"
	"notification-engine/internal/db"
	"notification-engine/internal/domain"
	"notification-engine/internal/queue"
	"notification-engine/internal/ratelimit"
	"notification-engine/internal/service"
	"notification-engine/internal/worker"
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
		log.Fatalf("[worker] falha ao conectar no Redis (%s): %v", cfg.RedisAddr, err)
	}
	cancelPing()
	log.Printf("[worker] conectado ao Redis em %s", cfg.RedisAddr)

	dsn := db.BuildDSN(cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBSSLMode)
	conn, err := db.Connect(dsn, cfg.DBMaxOpenConns)
	if err != nil {
		log.Fatalf("[worker] falha ao conectar no PostgreSQL (%s:%s): %v", cfg.DBHost, cfg.DBPort, err)
	}
	defer conn.Close()
	if err := db.EnsureSchema(conn); err != nil {
		log.Fatalf("[worker] falha ao aplicar schema do banco: %v", err)
	}
	log.Printf("[worker] conectado ao PostgreSQL em %s:%s", cfg.DBHost, cfg.DBPort)

	repo := db.NewNotificationRepository(conn)
	producer := queue.NewRedisProducer(redisClient, cfg.RedisStreamName)

	registry := channel.NewRegistry(
		time.Duration(cfg.HTTPClientTimeoutSeconds)*time.Second,
		cfg.TelegramBotToken,
		channel.GmailConfig{
			Host:        cfg.GmailSMTPHost,
			Port:        cfg.GmailSMTPPort,
			Username:    cfg.GmailUsername,
			AppPassword: cfg.GmailAppPassword,
			FromName:    cfg.GmailFromName,
		},
		channel.WhatsAppConfig{
			PhoneNumberID: cfg.WhatsAppPhoneNumberID,
			AccessToken:   cfg.WhatsAppAccessToken,
		},
		channel.OutlookConfig{
			TenantID:     cfg.OutlookTenantID,
			ClientID:     cfg.OutlookClientID,
			ClientSecret: cfg.OutlookClientSecret,
			SenderEmail:  cfg.OutlookSenderEmail,
		},
		cfg.AllowPrivateNetworkTargets,
	)
	if cfg.AllowPrivateNetworkTargets {
		log.Println("[worker] AVISO: ALLOW_PRIVATE_NETWORK_TARGETS=true — proteção contra SSRF desligada (não use em produção)")
	}

	rps := map[domain.ChannelType]float64{
		domain.ChannelDiscord:  cfg.RateLimitDiscordRPS,
		domain.ChannelTelegram: cfg.RateLimitTelegramRPS,
		domain.ChannelWebhook:  cfg.RateLimitWebhookRPS,
		domain.ChannelEmail:    cfg.RateLimitEmailRPS,
		domain.ChannelWhatsApp: cfg.RateLimitWhatsAppRPS,
		domain.ChannelOutlook:  cfg.RateLimitOutlookRPS,
	}
	if !cfg.RateLimitEnabled {
		// RPS "infinito" na prática: token bucket com burst altíssimo.
		for ch := range rps {
			rps[ch] = 1e6
		}
	}
	limiters := ratelimit.New(rps)

	dispatcher := &worker.Dispatcher{
		Registry:          registry,
		Repo:              repo,
		Limiters:          limiters,
		MaxBackoffSeconds: cfg.RetryMaxBackoffSeconds,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	retryPoller := &worker.RetryPoller{
		Repo:      repo,
		Producer:  producer,
		Interval:  time.Duration(cfg.RetryPollIntervalSeconds) * time.Second,
		BatchSize: cfg.RetryBatchSize,
	}
	go retryPoller.Run(ctx)

	if cfg.MetricsEnabled {
		go startMetricsServer(cfg.MetricsPort)
	}

	if cfg.TelegramBotEnabled && cfg.TelegramBotToken != "" {
		defaultTargets := map[domain.ChannelType]string{} // bot só consulta/reenfileira, não precisa de defaults
		svc := service.NewNotificationService(
			producer, repo, defaultTargets,
			cfg.RetryMaxAttempts, cfg.RetryMaxBackoffSeconds, cfg.AllowPrivateNetworkTargets,
		)
		if len(cfg.TelegramAllowedChatIDs) == 0 {
			log.Println("[worker] AVISO: TELEGRAM_ALLOWED_CHAT_IDS não configurado — qualquer chat pode executar /retry e /status")
		}
		telegramBot := bot.New(cfg.TelegramBotToken, svc, cfg.TelegramAllowedChatIDs)
		go telegramBot.Run(ctx)
	}

	// Concorrência nativa: sobe N consumidores independentes (goroutines),
	// cada um com um consumer-name único dentro do MESMO consumer group.
	// O Redis garante que cada mensagem do stream seja entregue a apenas
	// um dos consumidores do grupo, permitindo processamento paralelo
	// seguro sem duplicidade de entrega.
	var wg sync.WaitGroup
	for i := 0; i < cfg.WorkerConcurrency; i++ {
		consumerName := fmt.Sprintf("%s-%d", cfg.RedisConsumerName, i)
		consumer := queue.NewRedisConsumer(
			redisClient,
			cfg.RedisStreamName,
			cfg.RedisConsumerGroup,
			consumerName,
		)

		wg.Add(1)
		go func(c *queue.RedisConsumer, name string) {
			defer wg.Done()
			if err := c.Consume(ctx, dispatcher.Handle); err != nil && ctx.Err() == nil {
				log.Printf("[worker] consumidor %s encerrado com erro: %v", name, err)
			}
		}(consumer, consumerName)
	}

	log.Printf("[worker] %d goroutines consumidoras iniciadas (group=%s)",
		cfg.WorkerConcurrency, cfg.RedisConsumerGroup)

	<-ctx.Done()
	log.Println("[worker] sinal de encerramento recebido, aguardando goroutines finalizarem...")

	wg.Wait()

	if err := redisClient.Close(); err != nil {
		log.Printf("[worker] erro ao fechar conexão com Redis: %v", err)
	}

	log.Println("[worker] encerrado com sucesso")
}

// startMetricsServer sobe um servidor HTTP dedicado só para /metrics.
// Fica em processo/porta separados da API porque o worker não tem
// (e não precisa ter) um servidor HTTP de negócio.
func startMetricsServer(port string) {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.Handler())

	log.Printf("[worker] métricas expostas em :%s/metrics", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Printf("[worker] servidor de métricas encerrado: %v", err)
	}
}
