package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"notification-engine/internal/domain"
	"notification-engine/internal/metrics"
	"notification-engine/internal/retry"
	"notification-engine/internal/security"
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

	// Usados pelo canal "whatsapp" — veja domain.Notification.TemplateName.
	TemplateName   string   `json:"template_name,omitempty"`
	TemplateLocale string   `json:"template_locale,omitempty"`
	TemplateParams []string `json:"template_params,omitempty"`

	// IdempotencyKey normalmente vem do header HTTP Idempotency-Key (POST
	// /notifications); no /bulk, como não há um header por item, também
	// pode ser informado diretamente aqui no corpo JSON de cada item.
	IdempotencyKey string `json:"idempotency_key,omitempty"`
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
	producer             domain.Producer
	repo                 domain.Repository
	defaultTargets       map[domain.ChannelType]string
	maxAttempts          int
	maxBackoffSeconds    int
	allowPrivateNetworks bool
}

func NewNotificationService(
	producer domain.Producer,
	repo domain.Repository,
	defaultTargets map[domain.ChannelType]string,
	maxAttempts int,
	maxBackoffSeconds int,
	allowPrivateNetworks bool,
) *NotificationService {
	return &NotificationService{
		producer:             producer,
		repo:                 repo,
		defaultTargets:       defaultTargets,
		maxAttempts:          maxAttempts,
		maxBackoffSeconds:    maxBackoffSeconds,
		allowPrivateNetworks: allowPrivateNetworks,
	}
}

// CreateNotification valida o input, persiste a entidade e a enfileira
// para entrega. Retorna a notificação criada (com ID e status "pending")
// para o handler HTTP responder 202 Accepted com o ID de rastreio.
//
// Se um Idempotency-Key for informado e já existir uma notificação criada
// com essa mesma chave, a notificação existente é devolvida sem criar
// duplicata nem reenfileirar — torna seguro o cliente reenviar a mesma
// requisição (ex: após um timeout de rede) sem risco de entrega duplicada.
func (s *NotificationService) CreateNotification(ctx context.Context, input CreateNotificationInput) (*domain.Notification, error) {
	if input.IdempotencyKey != "" {
		if existing, err := s.repo.FindByIdempotencyKey(ctx, input.IdempotencyKey); err == nil {
			return existing, nil
		} else if !errors.Is(err, domain.ErrNotFound) {
			return nil, fmt.Errorf("service: falha ao consultar idempotency key: %w", err)
		}
	}

	n := s.build(input)

	if err := n.Validate(); err != nil {
		return nil, err
	}

	if n.Channel == domain.ChannelWebhook || n.Channel == domain.ChannelDiscord {
		if err := security.ValidateTargetURL(n.Target, s.allowPrivateNetworks); err != nil {
			return nil, err
		}
	}

	if err := s.repo.Create(ctx, n); err != nil {
		if errors.Is(err, domain.ErrDuplicateIdempotencyKey) {
			// Corrida entre duas requisições com a mesma chave: a que
			// perdeu a inserção devolve o resultado da que ganhou.
			existing, ferr := s.repo.FindByIdempotencyKey(ctx, input.IdempotencyKey)
			if ferr == nil {
				return existing, nil
			}
			return nil, fmt.Errorf("service: %w", err)
		}
		return nil, fmt.Errorf("service: falha ao persistir notificação: %w", err)
	}

	if err := s.producer.Enqueue(ctx, n); err != nil {
		// A notificação já está persistida no Postgres — em vez de deixá-la
		// "pending" sem nenhum processo responsável por ela (órfã, caso o
		// Redis tenha falhado bem nesse instante), tratamos como uma falha
		// de entrega comum: agenda retry (ou DLQ, se já esgotou tentativas)
		// e deixa o RetryPoller do worker reenfileirar mais tarde. O cliente
		// recebe 202 normalmente — o sistema já se comprometeu a entregar.
		log.Printf("[service] falha ao enfileirar %s no Redis, agendando retry via Postgres: %v", n.ID, err)
		retry.ApplyFailure(n, err, s.maxBackoffSeconds)
		if uerr := s.repo.Update(ctx, n); uerr != nil {
			log.Printf("[service] falha ao registrar retry pós-enqueue de %s: %v", n.ID, uerr)
		}
		return n, nil
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
			if IsValidationError(err) {
				results[i] = BulkResult{Status: "rejected", Error: err.Error()}
			} else {
				// Erro interno (ex: Postgres/Redis inacessível): detalhe
				// completo só no log do servidor, resposta genérica no item.
				log.Printf("[service] erro interno ao criar item %d do bulk: %v", i, err)
				results[i] = BulkResult{Status: "rejected", Error: "falha interna ao processar este item, tente novamente"}
			}
			continue
		}
		results[i] = BulkResult{ID: n.ID, Status: string(n.Status)}
	}

	return results
}

// IsValidationError classifica um erro de CreateNotification como "culpa do
// cliente" (input inválido, alvo bloqueado por SSRF) — seguro para devolver
// a mensagem original na resposta HTTP. Qualquer outro erro é tratado como
// falha interna: o chamador deve logar o detalhe e responder com uma
// mensagem genérica, para não vazar detalhes de infraestrutura (strings de
// conexão, erros de driver etc.) para o cliente da API.
func IsValidationError(err error) bool {
	return errors.Is(err, domain.ErrInvalidChannel) ||
		errors.Is(err, domain.ErrEmptyTarget) ||
		errors.Is(err, domain.ErrEmptyMessage) ||
		errors.Is(err, domain.ErrTooManyAttachments) ||
		errors.Is(err, domain.ErrAttachmentTooLarge) ||
		errors.Is(err, security.ErrBlockedTarget)
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
		ID:             uuid.NewString(),
		IdempotencyKey: input.IdempotencyKey,
		Channel:        input.Channel,
		Target:         target,
		Subject:        input.Subject,
		Message:        input.Message,
		Payload:        input.Payload,
		Headers:        input.Headers,
		Attachments:    input.Attachments,
		TemplateName:   input.TemplateName,
		TemplateLocale: input.TemplateLocale,
		TemplateParams: input.TemplateParams,
		Status:         domain.StatusPending,
		MaxAttempts:    s.maxAttempts,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}
