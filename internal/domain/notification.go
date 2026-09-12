package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ChannelType representa os canais de notificação suportados pela engine.
type ChannelType string

const (
	ChannelDiscord  ChannelType = "discord"
	ChannelTelegram ChannelType = "telegram"
	ChannelWebhook  ChannelType = "webhook"
	ChannelEmail    ChannelType = "email" // Gmail (SMTP)
)

// Status representa o estado do ciclo de vida de uma notificação.
type Status string

const (
	StatusPending  Status = "pending"  // aceita e enfileirada, aguardando processamento
	StatusSuccess  Status = "success"  // entregue com sucesso
	StatusRetrying Status = "retrying" // falhou, aguardando NextRetryAt para nova tentativa
	StatusDLQ      Status = "dlq"      // esgotou as tentativas, requer intervenção manual
)

// Erros de domínio conhecidos, usados para validação na camada de serviço.
var (
	ErrInvalidChannel     = errors.New("canal de notificação inválido")
	ErrEmptyTarget        = errors.New("o campo 'target' é obrigatório")
	ErrEmptyMessage       = errors.New("o campo 'message' é obrigatório")
	ErrNotFound           = errors.New("notificação não encontrada")
	ErrTooManyAttachments = fmt.Errorf("no máximo %d anexos por notificação", MaxAttachments)
	ErrAttachmentTooLarge = fmt.Errorf("cada anexo deve ter no máximo %dMB", MaxAttachmentBytes/1024/1024)
)

// Limites de anexos: alinhados ao teto mais restritivo entre os canais
// suportados (Discord Webhook sem boost: 8MB por arquivo) para manter o
// comportamento consistente em todos os canais.
const (
	MaxAttachments     = 5
	MaxAttachmentBytes = 8 * 1024 * 1024 // 8MB por anexo, já decodificado
)

// Attachment representa um arquivo anexado a uma notificação (imagem,
// documento etc). Data é o conteúdo em base64 puro (sem o prefixo
// "data:<mime>;base64,").
type Attachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Data        string `json:"data"`
}

// Notification é a entidade central do domínio. Representa um evento de
// notificação, desde a recepção via API até a entrega final pelo worker.
type Notification struct {
	ID          string            `json:"id"`
	Channel     ChannelType       `json:"channel"`
	Target      string            `json:"target"`            // URL (discord/webhook), chat_id (telegram) ou e-mail do destinatário (email)
	Subject     string            `json:"subject,omitempty"` // usado apenas pelo canal "email"
	Message     string            `json:"message"`
	Payload     map[string]any    `json:"payload,omitempty"`     // corpo customizado (usado no webhook genérico)
	Headers     map[string]string `json:"headers,omitempty"`     // headers customizados (usado no webhook genérico)
	Attachments []Attachment      `json:"attachments,omitempty"` // arquivos/fotos anexados
	Status      Status            `json:"status"`
	Attempts    int               `json:"attempts"`
	MaxAttempts int               `json:"max_attempts"`
	LastError   string            `json:"last_error,omitempty"`
	NextRetryAt *time.Time        `json:"next_retry_at,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// IsValidChannel confere se o canal informado é suportado pela engine.
func IsValidChannel(c ChannelType) bool {
	switch c {
	case ChannelDiscord, ChannelTelegram, ChannelWebhook, ChannelEmail:
		return true
	default:
		return false
	}
}

// Validate aplica as regras de negócio mínimas para aceitar uma notificação.
func (n *Notification) Validate() error {
	if !IsValidChannel(n.Channel) {
		return ErrInvalidChannel
	}
	if n.Target == "" {
		return ErrEmptyTarget
	}
	if n.Message == "" && n.Payload == nil && len(n.Attachments) == 0 {
		return ErrEmptyMessage
	}
	if len(n.Attachments) > MaxAttachments {
		return ErrTooManyAttachments
	}
	for i := range n.Attachments {
		n.Attachments[i].Data = stripDataURIPrefix(n.Attachments[i].Data)
		if decodedSize(n.Attachments[i].Data) > MaxAttachmentBytes {
			return ErrAttachmentTooLarge
		}
	}
	return nil
}

// stripDataURIPrefix remove um eventual prefixo "data:<mime>;base64," que o
// cliente possa ter enviado junto do conteúdo (ex: copiado direto de um
// <input type="file"> lido via FileReader.readAsDataURL).
func stripDataURIPrefix(data string) string {
	if idx := strings.Index(data, ";base64,"); idx != -1 && strings.HasPrefix(data, "data:") {
		return data[idx+len(";base64,"):]
	}
	return data
}

// decodedSize estima o tamanho em bytes do conteúdo original a partir do
// comprimento da string base64 (aprox. 3/4 do tamanho codificado).
func decodedSize(base64Data string) int {
	return len(base64Data) / 4 * 3
}

// ListFilter restringe uma consulta de notificações por status e/ou canal.
// Campos vazios são ignorados (sem filtro).
type ListFilter struct {
	Status  Status
	Channel ChannelType
	Limit   int
}

// Stats resume a distribuição de notificações por status e por canal,
// usado pelo endpoint /api/v1/stats e pelo dashboard.
type Stats struct {
	Total     int                 `json:"total"`
	Pending   int                 `json:"pending"`
	Success   int                 `json:"success"`
	Retrying  int                 `json:"retrying"`
	DLQ       int                 `json:"dlq"`
	ByChannel map[ChannelType]int `json:"by_channel"`
}

// Producer define o contrato para enfileirar notificações (implementado
// pelo pacote queue, ex: Redis Streams).
type Producer interface {
	Enqueue(ctx context.Context, n *Notification) error
}

// Consumer define o contrato para consumir notificações da fila e
// processá-las através de um handler.
type Consumer interface {
	Consume(ctx context.Context, handler func(context.Context, *Notification) error) error
}

// Sender define o contrato que todo conector de canal (Discord, Telegram,
// Webhook genérico...) precisa implementar para disparar uma notificação.
type Sender interface {
	Send(ctx context.Context, n *Notification) error
	Channel() ChannelType
}

// Repository define a persistência histórica das notificações (PostgreSQL),
// usada para auditoria, consulta pelo dashboard/bot e para localizar
// notificações prontas para nova tentativa (retry).
type Repository interface {
	Create(ctx context.Context, n *Notification) error
	Update(ctx context.Context, n *Notification) error
	FindByID(ctx context.Context, id string) (*Notification, error)
	FindDueForRetry(ctx context.Context, before time.Time, limit int) ([]*Notification, error)
	List(ctx context.Context, filter ListFilter) ([]*Notification, error)
	Stats(ctx context.Context) (Stats, error)
}
