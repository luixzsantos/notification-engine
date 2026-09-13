package domain

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strings"
	"time"
)

// ChannelType representa os canais de notificação suportados pela engine.
type ChannelType string

const (
	ChannelDiscord  ChannelType = "discord"
	ChannelTelegram ChannelType = "telegram"
	ChannelWebhook  ChannelType = "webhook"
	ChannelEmail    ChannelType = "email"    // Gmail (SMTP)
	ChannelWhatsApp ChannelType = "whatsapp" // WhatsApp Cloud API (Meta)
	ChannelOutlook  ChannelType = "outlook"  // Microsoft 365 / Outlook (Graph API)
	ChannelTeams    ChannelType = "teams"    // Microsoft Teams (Workflow webhook + Adaptive Card)
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

	// ErrDuplicateIdempotencyKey é retornado pelo Repository quando o
	// Idempotency-Key informado já pertence a outra notificação (violação da
	// constraint UNIQUE). O service trata isso retornando a notificação já
	// existente em vez de criar uma duplicata.
	ErrDuplicateIdempotencyKey = errors.New("já existe uma notificação com esse Idempotency-Key")
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
	ID             string            `json:"id"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"` // opcional; evita duplicar entrega em reenvios do cliente
	Channel        ChannelType       `json:"channel"`
	Target         string            `json:"target"`            // URL (discord/webhook), chat_id (telegram), e-mail (email/outlook) ou telefone E.164 (whatsapp)
	Subject        string            `json:"subject,omitempty"` // usado pelos canais "email" e "outlook"
	Message        string            `json:"message"`
	Payload        map[string]any    `json:"payload,omitempty"`     // corpo customizado (usado no webhook genérico)
	Headers        map[string]string `json:"headers,omitempty"`     // headers customizados (usado no webhook genérico)
	Attachments    []Attachment      `json:"attachments,omitempty"` // arquivos/fotos anexados

	// Campos de template, usados pelo canal "whatsapp": mensagens de negócio
	// (iniciadas pela empresa, fora de uma janela de conversa de 24h) exigem
	// um Message Template pré-aprovado pela Meta em vez de texto livre.
	// TemplateParams preenche as variáveis posicionais do template ({{1}},
	// {{2}}...), na ordem. Se TemplateName estiver vazio, o envio usa texto
	// livre (Message) — só funciona dentro de uma janela de conversa ativa.
	TemplateName   string   `json:"template_name,omitempty"`
	TemplateLocale string   `json:"template_locale,omitempty"` // ex: "pt_BR"; default "pt_BR" se omitido
	TemplateParams []string `json:"template_params,omitempty"`

	// Table, quando informado, anexa uma tabela simples à notificação.
	// Canais com renderização nativa de tabela (Teams via Adaptive Card,
	// e-mail via HTML) mostram uma tabela de verdade; os demais (Discord,
	// Telegram, WhatsApp em texto livre) recebem uma versão em texto
	// monoespaçado. Veja Table.FormatMonospace/FormatHTML.
	Table *Table `json:"table,omitempty"`

	Status      Status     `json:"status"`
	Attempts    int        `json:"attempts"`
	MaxAttempts int        `json:"max_attempts"`
	LastError   string     `json:"last_error,omitempty"`
	NextRetryAt *time.Time `json:"next_retry_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// IsValidChannel confere se o canal informado é suportado pela engine.
func IsValidChannel(c ChannelType) bool {
	switch c {
	case ChannelDiscord, ChannelTelegram, ChannelWebhook, ChannelEmail, ChannelWhatsApp, ChannelOutlook, ChannelTeams:
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
	if n.Message == "" && n.Payload == nil && len(n.Attachments) == 0 && n.TemplateName == "" && n.Table == nil {
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

// Table representa uma tabela simples (cabeçalho opcional + linhas) que
// pode ser anexada a uma notificação. Não é um formato específico de
// nenhum canal — cada Sender decide como renderizá-la (tabela nativa,
// HTML, ou texto monoespaçado) através de FormatMonospace/FormatHTML.
type Table struct {
	Headers []string   `json:"headers,omitempty"`
	Rows    [][]string `json:"rows"`
}

// columnCount retorna o maior número de colunas entre o cabeçalho e as
// linhas, para lidar com linhas de tamanhos desiguais sem entrar em pânico.
func (t *Table) columnCount() int {
	columns := len(t.Headers)
	for _, row := range t.Rows {
		if len(row) > columns {
			columns = len(row)
		}
	}
	return columns
}

func cellAt(cells []string, i int) string {
	if i < len(cells) {
		return cells[i]
	}
	return ""
}

// FormatMonospace renderiza a tabela como texto de largura fixa (colunas
// alinhadas por espaços) — usado pelos canais sem suporte nativo a tabelas
// (Discord, Telegram, WhatsApp em texto livre), tipicamente dentro de um
// bloco de código para preservar o alinhamento.
func (t *Table) FormatMonospace() string {
	if t == nil || len(t.Rows) == 0 {
		return ""
	}

	columns := t.columnCount()
	widths := make([]int, columns)

	measure := func(cells []string) {
		for i := 0; i < columns; i++ {
			if w := len(cellAt(cells, i)); w > widths[i] {
				widths[i] = w
			}
		}
	}
	measure(t.Headers)
	for _, row := range t.Rows {
		measure(row)
	}

	var b strings.Builder
	writeRow := func(cells []string) {
		for i := 0; i < columns; i++ {
			cell := cellAt(cells, i)
			b.WriteString(cell)
			if i < columns-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-len(cell)+2))
			}
		}
		b.WriteString("\n")
	}

	if len(t.Headers) > 0 {
		writeRow(t.Headers)
		total := columns - 1 // espaço entre colunas já contado abaixo
		for _, w := range widths {
			total += w
		}
		b.WriteString(strings.Repeat("-", total))
		b.WriteString("\n")
	}
	for _, row := range t.Rows {
		writeRow(row)
	}

	return strings.TrimRight(b.String(), "\n")
}

// FormatHTML renderiza a tabela como uma tag <table> HTML simples (com
// bordas), usada pelos canais de e-mail (Gmail/Outlook) quando o corpo da
// mensagem é enviado como HTML.
func (t *Table) FormatHTML() string {
	if t == nil || len(t.Rows) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(`<table border="1" cellpadding="6" cellspacing="0" style="border-collapse:collapse;">`)

	if len(t.Headers) > 0 {
		b.WriteString("<tr>")
		for _, h := range t.Headers {
			b.WriteString("<th>" + html.EscapeString(h) + "</th>")
		}
		b.WriteString("</tr>")
	}

	for _, row := range t.Rows {
		b.WriteString("<tr>")
		for _, cell := range row {
			b.WriteString("<td>" + html.EscapeString(cell) + "</td>")
		}
		b.WriteString("</tr>")
	}

	b.WriteString("</table>")
	return b.String()
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
	// FindByIdempotencyKey busca uma notificação já criada com essa chave.
	// Retorna ErrNotFound se nenhuma existir.
	FindByIdempotencyKey(ctx context.Context, key string) (*Notification, error)
	FindDueForRetry(ctx context.Context, before time.Time, limit int) ([]*Notification, error)
	List(ctx context.Context, filter ListFilter) ([]*Notification, error)
	Stats(ctx context.Context) (Stats, error)
}
