-- Schema da engine. Aplicado automaticamente na subida da API e do worker
-- (CREATE TABLE/INDEX IF NOT EXISTS), sem necessidade de uma ferramenta de
-- migration separada.

CREATE TABLE IF NOT EXISTS notifications (
    id              TEXT PRIMARY KEY,
    idempotency_key TEXT NOT NULL DEFAULT '',
    channel         TEXT NOT NULL,
    target          TEXT NOT NULL,
    subject         TEXT NOT NULL DEFAULT '',
    message         TEXT NOT NULL DEFAULT '',
    payload         JSONB NOT NULL DEFAULT '{}',
    headers         JSONB NOT NULL DEFAULT '{}',
    attachments     JSONB NOT NULL DEFAULT '[]',
    status          TEXT NOT NULL,
    attempts        INTEGER NOT NULL DEFAULT 0,
    max_attempts    INTEGER NOT NULL DEFAULT 5,
    last_error      TEXT NOT NULL DEFAULT '',
    next_retry_at   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL
);

-- Cobre instalações que criaram a tabela antes destes campos existirem.
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS attachments JSONB NOT NULL DEFAULT '[]';
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS idempotency_key TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_notifications_status ON notifications (status);
CREATE INDEX IF NOT EXISTS idx_notifications_created_at ON notifications (created_at DESC);

-- Índice único parcial: garante que duas notificações não compartilhem o
-- mesmo Idempotency-Key (a string vazia, quando o cliente não informa a
-- chave, fica de fora da constraint). Base da idempotência da API: uma
-- segunda requisição com a mesma chave falha aqui, e o service devolve a
-- notificação já existente em vez de criar uma duplicata.
CREATE UNIQUE INDEX IF NOT EXISTS idx_notifications_idempotency_key
    ON notifications (idempotency_key)
    WHERE idempotency_key <> '';

-- Índice parcial: acelera a varredura periódica de retries vencidos
-- (RetryPoller) sem pesar em linhas que já estão em success/dlq/pending.
CREATE INDEX IF NOT EXISTS idx_notifications_retry_due
    ON notifications (next_retry_at)
    WHERE status = 'retrying';
