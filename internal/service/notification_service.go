package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"notification-engine/internal/domain"
	"notification-engine/internal/metrics"
)

// CreateNotificationInput representa o payload aceito pela API para
// criação de uma nova notificação.
type CreateNotificationInput struct {
	Channel     domain.ChannelType  `json:"channel"`
	Target      string              `json:"target"`
	Subject     string              `json:"subject,omitempty"`
	Message     string              `json:"message"`
	Payload     map[string]any      `json:"payload,omitempty"`
	Headers     map[string]string   `json:"headers,omitempty"`
	Attachments []domain.Attachment `json:"attachments,omitempty"`
}

// BulkResult é o resultado individual de um item enviado via /bulk: ID e
// status quando aceito, ou Error quando rejeitado na validação.
type BulkResult struct {
	ID     string `json:"id,omitempty"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// NotificationService concentra a regra de negócio de validação,
// persistência e enfileiramento de notificações. Depende só das
// abstrações domain.Producer/domain.Repository, sem conhecer detalhes de
// transporte (HTTP) nem de infraestrutura (Redis/Postgres).
type NotificationService struct {
	producer       domain.Producer
	repo           domain.Repository
	defaultTargets map[domain.ChannelType]string
	maxAttempts    int
}

func NewNotificationService(
	producer domain.Producer,
	repo domain.Repository,
	defaultTargets map[domain.ChannelType]string,
	maxAttempts int,
) *NotificationService {
	return &NotificationService{
		producer:       producer,
		repo:           repo,
		defaultTargets: defaultTargets,
		maxAttempts:    maxAttempts,
	}
}

// CreateNotification valida o input, persiste a entidade e a enfileira
// para entrega. Retorna a notificação criada (com ID e status "pending")
// para o handler HTTP responder 202 Accepted com o ID de rastreio.
func (s *NotificationService) CreateNotification(ctx context.Context, input CreateNotificationInput) (*domain.Notification, error) {
	n := s.build(input)

	if err := n.Validate(); err != nil {
		return nil, err
	}

	if err := s.repo.Create(ctx, n); err != nil {
		return nil, fmt.Errorf("service: falha ao persistir notificação: %w", err)
	}

	if err := s.producer.Enqueue(ctx, n); err != nil {
		return nil, fmt.Errorf("service: falha ao enfileirar notificação: %w", err)
	}

	metrics.RecordEnqueued(string(n.Channel))
	return n, nil
}

// CreateBulk processa um lote de notificações, isolando a falha de um item
// (validação, persistência ou fila) dos demais.
func (s *NotificationService) CreateBulk(ctx context.Context, inputs []CreateNotificationInput) []BulkResult {
	results := make([]BulkResult, len(inputs))

	for i, input := range inputs {
		n, err := s.CreateNotification(ctx, input)
		if err != nil {
			results[i] = BulkResult{Status: "rejected", Error: err.Error()}
			continue
		}
		results[i] = BulkResult{ID: n.ID, Status: string(n.Status)}
	}

	return results
}

func (s *NotificationService) GetByID(ctx context.Context, id string) (*domain.Notification, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *NotificationService) List(ctx context.Context, filter domain.ListFilter) ([]*domain.Notification, error) {
	return s.repo.List(ctx, filter)
}

func (s *NotificationService) Stats(ctx context.Context) (domain.Stats, error) {
	return s.repo.Stats(ctx)
}

// Retry reenfileira manualmente uma notificação parada em "retrying" ou
// "dlq" (usado pelo endpoint POST /notifications/{id}/retry e pelo bot).
func (s *NotificationService) Retry(ctx context.Context, id string) (*domain.Notification, error) {
	n, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if n.Status != domain.StatusDLQ && n.Status != domain.StatusRetrying {
		return nil, fmt.Errorf("service: notificação %s não está em retry ou DLQ (status atual: %s)", id, n.Status)
	}

	// Retry manual = intervenção humana (ex: corrigiu a URL do webhook).
	// Zera as tentativas para dar um ciclo novo, em vez de falhar uma vez
	// e cair direto de volta na DLQ.
	n.Status = domain.StatusPending
	n.Attempts = 0
	n.LastError = ""
	n.NextRetryAt = nil
	n.UpdatedAt = time.Now().UTC()

	if err := s.producer.Enqueue(ctx, n); err != nil {
		return nil, fmt.Errorf("service: falha ao reenfileirar: %w", err)
	}
	if err := s.repo.Update(ctx, n); err != nil {
		return nil, fmt.Errorf("service: falha ao atualizar status: %w", err)
	}

	return n, nil
}

func (s *NotificationService) build(input CreateNotificationInput) *domain.Notification {
	now := time.Now().UTC()

	target := input.Target
	if target == "" {
		target = s.defaultTargets[input.Channel]
	}

	return &domain.Notification{
		ID:          uuid.NewString(),
		Channel:     input.Channel,
		Target:      target,
		Subject:     input.Subject,
		Message:     input.Message,
		Payload:     input.Payload,
		Headers:     input.Headers,
		Attachments: input.Attachments,
		Status:      domain.StatusPending,
		MaxAttempts: s.maxAttempts,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}
