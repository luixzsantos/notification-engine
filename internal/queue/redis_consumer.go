package queue

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/redis/go-redis/v9"

	"notification-engine/internal/domain"
)

// claimMinIdle é o tempo mínimo que uma mensagem precisa estar parada na
// Pending Entries List (PEL) antes de ser reivindicada por reclaimStale.
// Evita reivindicar uma mensagem que outro consumidor ainda está processando.
const claimMinIdle = 30 * time.Second

// RedisConsumer implementa domain.Consumer utilizando Redis Streams com
// Consumer Groups (XREADGROUP), permitindo múltiplos workers escalarem
// horizontalmente sem processar a mesma mensagem duas vezes. Mensagens que
// ficam presas na PEL (worker derrubado, reiniciado, ou interrompido durante
// o graceful shutdown) são reivindicadas de volta via XAUTOCLAIM.
type RedisConsumer struct {
	client       *redis.Client
	streamName   string
	consumerGrp  string
	consumerName string
}

func NewRedisConsumer(client *redis.Client, streamName, consumerGroup, consumerName string) *RedisConsumer {
	return &RedisConsumer{
		client:       client,
		streamName:   streamName,
		consumerGrp:  consumerGroup,
		consumerName: consumerName,
	}
}

// ensureGroup cria o consumer group caso ele ainda não exista.
// "$" indica que o grupo só receberá mensagens adicionadas a partir de agora;
// "0" faria o grupo reprocessar todo o histórico do stream.
func (c *RedisConsumer) ensureGroup(ctx context.Context) error {
	err := c.client.XGroupCreateMkStream(ctx, c.streamName, c.consumerGrp, "$").Err()
	if err != nil && !errors.Is(err, redis.Nil) {
		// BUSYGROUP significa que o grupo já existe — não é um erro real.
		if err.Error() != "BUSYGROUP Consumer Group name already exists" {
			return err
		}
	}
	return nil
}

// Consume entra em loop bloqueante lendo novas mensagens do stream e
// delegando o processamento ao handler informado. A cada iteração também
// reivindica mensagens presas na PEL de outros consumidores (reclaimStale).
// Em caso de sucesso a mensagem é confirmada (XACK); em caso de erro do
// handler ela permanece pendente para uma futura reivindicação.
func (c *RedisConsumer) Consume(ctx context.Context, handler func(context.Context, *domain.Notification) error) error {
	if err := c.ensureGroup(ctx); err != nil {
		return err
	}

	log.Printf("[consumer] escutando stream=%q group=%q consumer=%q", c.streamName, c.consumerGrp, c.consumerName)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		c.reclaimStale(ctx, handler)

		streams, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    c.consumerGrp,
			Consumer: c.consumerName,
			Streams:  []string{c.streamName, ">"}, // ">" = apenas mensagens nunca entregues
			Count:    10,
			Block:    5 * time.Second,
		}).Result()

		if err != nil {
			if errors.Is(err, redis.Nil) || errors.Is(err, context.Canceled) {
				continue // timeout do Block, tenta novamente
			}
			log.Printf("[consumer] erro ao ler do stream: %v", err)
			time.Sleep(1 * time.Second)
			continue
		}

		for _, stream := range streams {
			for _, message := range stream.Messages {
				c.processMessage(ctx, message, handler)
			}
		}
	}
}

// reclaimStale reivindica mensagens paradas há mais de claimMinIdle na PEL
// de qualquer consumidor do grupo (inclui as deste próprio, caso ele tenha
// sido reiniciado) e as processa como se tivessem acabado de chegar.
func (c *RedisConsumer) reclaimStale(ctx context.Context, handler func(context.Context, *domain.Notification) error) {
	messages, _, err := c.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   c.streamName,
		Group:    c.consumerGrp,
		Consumer: c.consumerName,
		MinIdle:  claimMinIdle,
		Start:    "0-0",
		Count:    10,
	}).Result()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			log.Printf("[consumer] erro ao reivindicar mensagens presas na PEL: %v", err)
		}
		return
	}

	for _, message := range messages {
		log.Printf("[consumer] mensagem %s reivindicada da PEL (idle > %s)", message.ID, claimMinIdle)
		c.processMessage(ctx, message, handler)
	}
}

func (c *RedisConsumer) processMessage(ctx context.Context, message redis.XMessage, handler func(context.Context, *domain.Notification) error) {
	rawData, ok := message.Values["data"].(string)
	if !ok {
		log.Printf("[consumer] mensagem %s sem campo 'data' válido, descartando", message.ID)
		c.ack(ctx, message.ID)
		return
	}

	var notification domain.Notification
	if err := json.Unmarshal([]byte(rawData), &notification); err != nil {
		log.Printf("[consumer] falha ao deserializar mensagem %s: %v", message.ID, err)
		c.ack(ctx, message.ID)
		return
	}

	if err := handler(ctx, &notification); err != nil {
		// O handler (worker.Dispatcher) só devolve erro em shutdown — a
		// mensagem permanece na PEL e será reivindicada por reclaimStale
		// assim que o tempo mínimo de idle passar.
		log.Printf("[consumer] falha ao processar notificação id=%s channel=%s: %v",
			notification.ID, notification.Channel, err)
		return
	}

	c.ack(ctx, message.ID)
}

func (c *RedisConsumer) ack(ctx context.Context, messageID string) {
	if err := c.client.XAck(ctx, c.streamName, c.consumerGrp, messageID).Err(); err != nil {
		log.Printf("[consumer] falha ao confirmar (ACK) mensagem %s: %v", messageID, err)
	}
}
