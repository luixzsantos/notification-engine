# Changelog - V2.0.0

## 🚀 Overview

**v2.0.0** é um upgrade completo da V1, adicionando **persistência, confiabilidade, observabilidade e operacionalidade**:

- ✅ **Banco de dados PostgreSQL** para auditoria e retry scheduling
- ✅ **Retry automático** com exponential backoff e Dead Letter Queue
- ✅ **Rate limiting** por canal (Discord/Telegram/Email/Webhook)
- ✅ **API Bulk** para enviar até 500 notificações de uma vez
- ✅ **Métricas Prometheus** (API e Worker)
- ✅ **Bot Telegram** para consultar status e forçar retry manualmente
- ✅ **Dashboard web** minimalista para monitorar notificações em tempo real
- ✅ **Recuperação de mensagens** presas na fila (XAUTOCLAIM)

---

## 🔄 Comparação V1 → V2

### V1 (Original)
```
API → Redis Stream → Worker → Canal (Discord/Telegram/Gmail/Webhook)
         ↓
        Sucesso/Falha (sem auditoria, sem retry)
```

### V2 (Novo)
```
API → PostgreSQL (estado) + Redis Stream (fila)
  ↓
Worker (com rate limit) → Dispatcher → Canal
  ↓
  ├─ Sucesso → status=success (registra no DB)
  ├─ Falha → Backoff exponencial → status=retrying (agenda NextRetryAt)
  └─ Esgotadas tentativas → status=dlq (consultar via API/Bot/Dashboard)
  ↓
RetryPoller (goroutine) → varre DB a cada 10s → reenfileira retry vencido
```

---

## 📝 Mudanças Principais

### 1. **Domínio Estendido** (`internal/domain/notification.go`)

**Novos campos:**
```go
MaxAttempts  int        // padrão: 5
Attempts     int        // tentativa atual
LastError    string     // último erro
NextRetryAt  *time.Time // agendado para quando?
```

**Novos status:**
- `pending` → aceita, esperando envio
- `success` → entregue com sucesso
- `retrying` → falhou, aguardando retry
- `dlq` → esgotadas tentativas, requer intervenção

### 2. **Persistência** (`internal/db/`)

- **postgres.go**: conexão + schema auto-aplicado
- **schema.sql**: tabelas `notifications`, índices otimizados
- **notification_repository.go**: CRUD + queries para retry/DLQ

### 3. **Rate Limiting** (`internal/ratelimit/limiter.go`)

Token bucket por canal (implementado com `golang.org/x/time/rate`):
- Discord: 10 req/s (configurável)
- Telegram: 20 req/s
- Email: 5 req/s
- Webhook: 50 req/s

### 4. **Retry Automático** (`internal/retry/` + `internal/worker/`)

**backoff.go**: exponential backoff (1s → 2s → 4s → 8s... até 1h)

**dispatcher.go**: processa notificação, calcula próximo retry

**retry_poller.go**: goroutine que periodicamente:
1. Busca notificações com `next_retry_at` vencido
2. Muda status para `pending`
3. Reenfileira no Redis

### 5. **Métricas** (`internal/metrics/metrics.go`)

Prometheus counters/histogramas:
- `notifications_enqueued_total{channel}` — total aceito
- `notifications_sent_total{channel,result}` — success/failure
- `notifications_retried_total{channel}` — agendado retry
- `notifications_dlq_total{channel}` — esgotadas tentativas
- `notification_send_duration_seconds{channel}` — latência por canal

### 6. **Bulk API** (`internal/handler/notification_handler.go`)

`POST /api/v1/notifications/bulk` — até 500 itens de uma vez:
```json
[
  {"channel":"webhook","target":"...",  "message":"..."},
  {"channel":"discord","message":"..."}
]
```

Resposta: array com `{id, status}` ou `{status, error}` por item.

### 7. **Bot Telegram** (`internal/bot/telegram.go`)

Opcional via `TELEGRAM_BOT_ENABLED=true`:
- `/status <id>` — mostra canal, status, tentativas, último erro
- `/retry <id>` — reenfileira manual, zera tentativas

### 8. **Dashboard** (unificado em `main.html`)

Interface única — sem página separada. A barra lateral do `main.html` alterna entre "enviar" e "dashboard" via JS, sem recarregar:
- **Stats**: Total, Pending, Success, Retrying, DLQ (em tempo real)
- **Lista**: filtrável por status/canal
- **Retry manual**: botão para itens em retry/dlq
- Auto-refresh a cada 15s
- Links diretos para Prometheus e para os endpoints `/metrics`
- Indicador de status da API (online/offline) no rodapé

### 9. **Recuperação de Mensagens**

`internal/queue/redis_consumer.go` agora usa `XAUTOCLAIM`:
- A cada iteração, recupera mensagens presas na PEL há > 30s
- Previne perda de mensagens quando worker é derrubado/reiniciado

---

## 🐛 Bugs Corrigidos

1. **Telegram nunca funcionava**: sender nunca foi registrado no `Registry`
2. **main.html HTML inválido**: removidos blocos ` ``` ` de markdown
3. **Mensagens perdidas**: faltava `XAUTOCLAIM` para recuperar PEL
4. **Retry manual quebrado**: não zerava attempt counter (voltava direto pra DLQ)

---

## 📦 Dependências Novas

```
github.com/lib/pq v1.12.3                         # PostgreSQL driver
github.com/prometheus/client_golang v1.20.5       # Métricas
golang.org/x/time v0.5.0                           # Rate limiting
```

**Nenhuma lib de migration** — schema auto-aplicado via SQL embutido.

---

## 🔧 Como Rodar V2

### 1. Subir infraestrutura
```bash
docker compose up -d
```

### 2. Configurar `.env`
```bash
cp .env.example .env
# editar .env com credenciais reais (se necessário)
```

### 3. Rodar API e Worker
```bash
go run ./cmd/api/main.go       # terminal 1
go run ./cmd/worker/main.go    # terminal 2
```

### 4. Acessar
- API: `http://localhost:8080/api/v1/notifications`
- Form: `main.html`
- Interface (enviar + dashboard): `main.html`
- Métricas (API): `http://localhost:8080/metrics`
- Métricas (Worker): `http://localhost:9091/metrics`
- Prometheus: `http://localhost:9090`

---

## 📊 Novos Endpoints

| Método | Path | Descrição |
|--------|------|-----------|
| `POST` | `/api/v1/notifications` | Criar notificação |
| `POST` | `/api/v1/notifications/bulk` | Criar até 500 de uma vez |
| `GET` | `/api/v1/notifications` | Listar (filtro: status, channel, limit) |
| `GET` | `/api/v1/notifications/{id}` | Consultar uma |
| `POST` | `/api/v1/notifications/{id}/retry` | Retry manual |
| `GET` | `/api/v1/stats` | Stats por status/canal |
| `GET` | `/metrics` | Prometheus |

---

## 🔮 V3 (Ideias)

- [ ] WebSocket real-time stats
- [ ] GraphQL API
- [ ] Webhook para retry events
- [ ] Suporte a S3/Cloud Storage para audit logs
- [ ] Multi-tenant (isolamento por workspace)
- [ ] API autenticada (JWT/API key)

---

## 📜 Licença

MIT
