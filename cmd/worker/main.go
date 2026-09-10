package main

import (
	"context"
	"fmt"
	"log"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"notification-engine/internal/channel"
	"notification-engine/internal/config"
	"notification-engine/internal/domain"
	"notification-engine/internal/queue"
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
	)

	// dispatch é o handler de negócio chamado pelo consumer para cada
	// mensagem lida do stream: identifica o conector do canal e dispara.
	dispatch := func(ctx context.Context, n *domain.Notification) error {
		sender, err := registry.Get(n.Channel)
		if err != nil {
			return fmt.Errorf("canal não suportado: %w", err)
		}

		if err := sender.Send(ctx, n); err != nil {
			log.Printf("[worker] FALHA id=%s channel=%s target=%s erro=%v",
				n.ID, n.Channel, n.Target, err)
			return err
		}

		log.Printf("[worker] SUCESSO id=%s channel=%s target=%s",
			n.ID, n.Channel, n.Target)
		return nil
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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
			if err := c.Consume(ctx, dispatch); err != nil && ctx.Err() == nil {
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
