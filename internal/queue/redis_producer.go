package queue

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"

	"notification-engine/internal/domain"
)

// RedisProducer implementa domain.Producer utilizando Redis Streams (XADD).
// Streams foram escolhidos em vez de Pub/Sub puro por garantirem persistência
// da mensagem e permitirem consumer groups com ACK/retry na V2.
type RedisProducer struct {
	client     *redis.Client
	streamName string
}

func NewRedisProducer(client *redis.Client, streamName string) *RedisProducer {
	return &RedisProducer{client: client, streamName: streamName}
}

// Enqueue serializa a notificação em JSON e adiciona ao stream do Redis.
func (p *RedisProducer) Enqueue(ctx context.Context, n *domain.Notification) error {
	data, err := json.Marshal(n)
	if err != nil {
		return fmt.Errorf("redis producer: falha ao serializar notificação: %w", err)
	}

	err = p.client.XAdd(ctx, &redis.XAddArgs{
		Stream: p.streamName,
		Values: map[string]any{
			"id":   n.ID,
			"data": data,
		},
	}).Err()
	if err != nil {
		return fmt.Errorf("redis producer: falha ao publicar no stream %q: %w", p.streamName, err)
	}

	return nil
}
