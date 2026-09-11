package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"notification-engine/internal/domain"
)

// CreateNotificationInput representa o payload aceito pela API para
// criação de uma nova notificação.
type CreateNotificationInput struct {
	Channel domain.ChannelType `json:"channel"`
	Target  string             `json:"target"`
	Message string             `json:"message"`
	Payload map[string]any     `json:"payload,omitempty"`
	Headers map[string]string  `json:"headers,omitempty"`
}

// NotificationService concentra a regra de negócio de validação e
// enfileiramento de notificações. Não conhece detalhes de transporte
// (HTTP) nem de infraestrutura (Redis) — depende apenas da abstração
// domain.Producer, respeitando a Clean Architecture.
type NotificationService struct {
	producer       domain.Producer
	defaultTargets map[domain.ChannelType]string
}

// NewNotificationService recebe opcionalmente um mapa de "targets padrão"
// por canal (ex: a URL fixa de um webhook do Discord). Quando a requisição
// não informar "target", esse default é usado — útil para testes rápidos.
func NewNotificationService(producer domain.Producer, defaultTargets map[domain.ChannelType]string) *NotificationService {
	return &NotificationService{producer: producer, defaultTargets: defaultTargets}
}

// CreateNotification valida o input, monta a entidade Notification e a
// envia para a fila. Retorna a notificação criada (com ID e status
// "pending") para que o handler HTTP responda 202 Accepted com o ID de
// rastreio ao cliente.
func (s *NotificationService) CreateNotification(ctx context.Context, input CreateNotificationInput) (*domain.Notification, error) {
	now := time.Now().UTC()

	target := input.Target
	if target == "" {
		if defaultTarget, ok := s.defaultTargets[input.Channel]; ok {
			target = defaultTarget
		}
	}

	notification := &domain.Notification{
		ID:        uuid.NewString(),
		Channel:   input.Channel,
		Target:    target,
		Message:   input.Message,
		Payload:   input.Payload,
		Headers:   input.Headers,
		Status:    domain.StatusPending,
		Attempts:  0,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := notification.Validate(); err != nil {
		return nil, err
	}

	if err := s.producer.Enqueue(ctx, notification); err != nil {
		return nil, fmt.Errorf("service: falha ao enfileirar notificação: %w", err)
	}

	return notification, nil
}
