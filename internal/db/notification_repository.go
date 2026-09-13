package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"

	"notification-engine/internal/domain"
)

// pqUniqueViolation é o código de erro do PostgreSQL para violação de
// constraint UNIQUE (usado para detectar Idempotency-Key duplicado).
const pqUniqueViolation = "23505"

// NotificationRepository implementa domain.Repository sobre PostgreSQL,
// usando database/sql + SQL puro (sem ORM).
type NotificationRepository struct {
	conn *sql.DB
}

func NewNotificationRepository(conn *sql.DB) *NotificationRepository {
	return &NotificationRepository{conn: conn}
}

const selectColumns = `
	id, idempotency_key, channel, target, subject, message, payload, headers, attachments,
	template_name, template_locale, template_params,
	status, attempts, max_attempts, last_error, next_retry_at,
	created_at, updated_at`

func (r *NotificationRepository) Create(ctx context.Context, n *domain.Notification) error {
	payload, err := json.Marshal(orEmptyMap(n.Payload))
	if err != nil {
		return fmt.Errorf("db: falha ao serializar payload: %w", err)
	}

	headers, err := json.Marshal(orEmptyStringMap(n.Headers))
	if err != nil {
		return fmt.Errorf("db: falha ao serializar headers: %w", err)
	}

	attachments, err := json.Marshal(orEmptyAttachments(n.Attachments))
	if err != nil {
		return fmt.Errorf("db: falha ao serializar anexos: %w", err)
	}

	templateParams, err := json.Marshal(orEmptyStringSlice(n.TemplateParams))
	if err != nil {
		return fmt.Errorf("db: falha ao serializar parâmetros de template: %w", err)
	}

	_, err = r.conn.ExecContext(ctx, `
		INSERT INTO notifications
			(id, idempotency_key, channel, target, subject, message, payload, headers, attachments,
			 template_name, template_locale, template_params,
			 status, attempts, max_attempts, last_error, next_retry_at,
			 created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`,
		n.ID, n.IdempotencyKey, n.Channel, n.Target, n.Subject, n.Message, payload, headers, attachments,
		n.TemplateName, n.TemplateLocale, templateParams,
		n.Status, n.Attempts, n.MaxAttempts, n.LastError, n.NextRetryAt,
		n.CreatedAt, n.UpdatedAt,
	)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqUniqueViolation {
			return domain.ErrDuplicateIdempotencyKey
		}
		return fmt.Errorf("db: falha ao inserir notificação: %w", err)
	}

	return nil
}

// Update grava o estado mutável da notificação (status, tentativas, erro e
// agendamento de retry). Os campos imutáveis (canal, destino, mensagem...)
// não são reescritos.
func (r *NotificationRepository) Update(ctx context.Context, n *domain.Notification) error {
	result, err := r.conn.ExecContext(ctx, `
		UPDATE notifications
		SET status = $1, attempts = $2, last_error = $3, next_retry_at = $4, updated_at = $5
		WHERE id = $6`,
		n.Status, n.Attempts, n.LastError, n.NextRetryAt, n.UpdatedAt, n.ID,
	)
	if err != nil {
		return fmt.Errorf("db: falha ao atualizar notificação %s: %w", n.ID, err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("db: falha ao confirmar atualização de %s: %w", n.ID, err)
	}
	if rows == 0 {
		return fmt.Errorf("db: %w (id=%s)", domain.ErrNotFound, n.ID)
	}

	return nil
}

func (r *NotificationRepository) FindByID(ctx context.Context, id string) (*domain.Notification, error) {
	row := r.conn.QueryRowContext(ctx, "SELECT "+selectColumns+" FROM notifications WHERE id = $1", id)

	n, err := scanNotification(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("db: falha ao buscar notificação %s: %w", id, err)
	}

	return n, nil
}

// FindByIdempotencyKey busca a notificação já criada com essa chave, usada
// pelo service para devolver o resultado original em vez de reprocessar
// uma requisição repetida (retry de rede do lado do cliente, por exemplo).
func (r *NotificationRepository) FindByIdempotencyKey(ctx context.Context, key string) (*domain.Notification, error) {
	row := r.conn.QueryRowContext(ctx, "SELECT "+selectColumns+" FROM notifications WHERE idempotency_key = $1", key)

	n, err := scanNotification(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("db: falha ao buscar notificação por idempotency_key: %w", err)
	}

	return n, nil
}

// FindDueForRetry retorna notificações em retry cujo NextRetryAt já venceu,
// usadas pelo RetryPoller para reenfileirar no Redis.
func (r *NotificationRepository) FindDueForRetry(ctx context.Context, before time.Time, limit int) ([]*domain.Notification, error) {
	rows, err := r.conn.QueryContext(ctx, `
		SELECT `+selectColumns+`
		FROM notifications
		WHERE status = $1 AND next_retry_at <= $2
		ORDER BY next_retry_at ASC
		LIMIT $3`,
		domain.StatusRetrying, before, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("db: falha ao buscar retries pendentes: %w", err)
	}
	defer rows.Close()

	return scanNotifications(rows)
}

// List retorna as notificações mais recentes, filtradas opcionalmente por
// status e/ou canal (usado pela API de consulta e pelo dashboard).
func (r *NotificationRepository) List(ctx context.Context, filter domain.ListFilter) ([]*domain.Notification, error) {
	query := "SELECT " + selectColumns + " FROM notifications WHERE 1=1"
	var args []any

	if filter.Status != "" {
		args = append(args, filter.Status)
		query += fmt.Sprintf(" AND status = $%d", len(args))
	}
	if filter.Channel != "" {
		args = append(args, filter.Channel)
		query += fmt.Sprintf(" AND channel = $%d", len(args))
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit)
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", len(args))

	rows, err := r.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("db: falha ao listar notificações: %w", err)
	}
	defer rows.Close()

	return scanNotifications(rows)
}

// Stats agrega contagens por status e por canal em duas consultas simples
// (evita trazer todas as linhas para agregar em memória).
func (r *NotificationRepository) Stats(ctx context.Context) (domain.Stats, error) {
	stats := domain.Stats{ByChannel: make(map[domain.ChannelType]int)}

	statusRows, err := r.conn.QueryContext(ctx, "SELECT status, COUNT(*) FROM notifications GROUP BY status")
	if err != nil {
		return stats, fmt.Errorf("db: falha ao agregar por status: %w", err)
	}
	defer statusRows.Close()

	for statusRows.Next() {
		var status domain.Status
		var count int
		if err := statusRows.Scan(&status, &count); err != nil {
			return stats, fmt.Errorf("db: falha ao ler agregação por status: %w", err)
		}

		stats.Total += count
		switch status {
		case domain.StatusPending:
			stats.Pending = count
		case domain.StatusSuccess:
			stats.Success = count
		case domain.StatusRetrying:
			stats.Retrying = count
		case domain.StatusDLQ:
			stats.DLQ = count
		}
	}
	if err := statusRows.Err(); err != nil {
		return stats, fmt.Errorf("db: erro ao iterar agregação por status: %w", err)
	}

	channelRows, err := r.conn.QueryContext(ctx, "SELECT channel, COUNT(*) FROM notifications GROUP BY channel")
	if err != nil {
		return stats, fmt.Errorf("db: falha ao agregar por canal: %w", err)
	}
	defer channelRows.Close()

	for channelRows.Next() {
		var channel domain.ChannelType
		var count int
		if err := channelRows.Scan(&channel, &count); err != nil {
			return stats, fmt.Errorf("db: falha ao ler agregação por canal: %w", err)
		}
		stats.ByChannel[channel] = count
	}
	if err := channelRows.Err(); err != nil {
		return stats, fmt.Errorf("db: erro ao iterar agregação por canal: %w", err)
	}

	return stats, nil
}

// row abstrai *sql.Row e *sql.Rows, que compartilham o método Scan mas não
// uma interface comum na stdlib.
type row interface {
	Scan(dest ...any) error
}

func scanNotification(r row) (*domain.Notification, error) {
	var n domain.Notification
	var payload, headers, attachments, templateParams []byte
	var nextRetryAt sql.NullTime

	err := r.Scan(
		&n.ID, &n.IdempotencyKey, &n.Channel, &n.Target, &n.Subject, &n.Message, &payload, &headers, &attachments,
		&n.TemplateName, &n.TemplateLocale, &templateParams,
		&n.Status, &n.Attempts, &n.MaxAttempts, &n.LastError, &nextRetryAt,
		&n.CreatedAt, &n.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(payload, &n.Payload); err != nil {
		return nil, fmt.Errorf("payload inválido: %w", err)
	}
	if err := json.Unmarshal(headers, &n.Headers); err != nil {
		return nil, fmt.Errorf("headers inválidos: %w", err)
	}
	if err := json.Unmarshal(attachments, &n.Attachments); err != nil {
		return nil, fmt.Errorf("anexos inválidos: %w", err)
	}
	if err := json.Unmarshal(templateParams, &n.TemplateParams); err != nil {
		return nil, fmt.Errorf("parâmetros de template inválidos: %w", err)
	}
	if nextRetryAt.Valid {
		n.NextRetryAt = &nextRetryAt.Time
	}

	return &n, nil
}

func scanNotifications(rows *sql.Rows) ([]*domain.Notification, error) {
	items := make([]*domain.Notification, 0)

	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, fmt.Errorf("falha ao ler notificação: %w", err)
		}
		items = append(items, n)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("erro ao iterar notificações: %w", err)
	}

	return items, nil
}

func orEmptyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func orEmptyStringMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func orEmptyAttachments(a []domain.Attachment) []domain.Attachment {
	if a == nil {
		return []domain.Attachment{}
	}
	return a
}

func orEmptyStringSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// buildDSN monta a connection string do PostgreSQL a partir das partes
// configuradas via .env, evitando expor a senha em caso de erro de parsing.
func BuildDSN(host, port, user, password, name, sslMode string) string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		user, password, host, port, name, orDefault(sslMode, "disable"),
	)
}

func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
