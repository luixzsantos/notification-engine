# Guia de Upgrade e Uso - V2.0.0

## 📦 O que mudou na V2?

A V2 adiciona **persistência, confiabilidade e observabilidade** completas ao projeto. Se você estava usando a V1, este guia explica as mudanças e como iniciar com a V2.

---

## ⚠️ Alterações Necessárias para Iniciar

### 1. **Instalação de Dependências**

A V2 adiciona 3 dependências Go novas:

```bash
cd C:\dev\NotificationEngine
go get github.com/lib/pq@latest                          # PostgreSQL
go get github.com/prometheus/client_golang@latest        # Métricas
go get golang.org/x/time@latest                           # Rate limiting
go mod tidy
```

Se você clonar do GitHub, basta rodar:
```bash
go mod tidy
```

### 2. **Variáveis de Ambiente (.env)**

A V2 requer **novas variáveis de ambiente**. Copie o `.env.example` e adicione:

```bash
cp .env.example .env
```

**Novas seções obrigatórias:**

```env
# PostgreSQL (NOVO)
DB_HOST=localhost
DB_PORT=5432
DB_USER=engine
DB_PASSWORD=engine
DB_NAME=notification_engine
DB_SSL_MODE=disable

# Retry (NOVO)
RETRY_MAX_ATTEMPTS=5
RETRY_MAX_BACKOFF_SECONDS=3600
RETRY_POLL_INTERVAL_SECONDS=10
RETRY_BATCH_SIZE=50

# Rate Limiting (NOVO)
RATE_LIMIT_ENABLED=true
RATE_LIMIT_DISCORD_RPS=10
RATE_LIMIT_TELEGRAM_RPS=20
RATE_LIMIT_WEBHOOK_RPS=50
RATE_LIMIT_EMAIL_RPS=5

# Métricas (NOVO)
METRICS_ENABLED=true
METRICS_PORT=9091

# Bot Telegram (NOVO, opcional)
TELEGRAM_BOT_ENABLED=false
```

**Seções que já existiam (manter ou atualizar):**

```env
API_PORT=8080
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=
REDIS_DB=0
REDIS_STREAM_NAME=notifications:stream
REDIS_CONSUMER_GROUP=notification-workers
REDIS_CONSUMER_NAME=worker-1
WORKER_CONCURRENCY=10
HTTP_CLIENT_TIMEOUT_SECONDS=10

# Canais (manter credenciais reais)
TELEGRAM_BOT_TOKEN=seu-token-aqui
GMAIL_SMTP_HOST=smtp.gmail.com
GMAIL_SMTP_PORT=587
GMAIL_USERNAME=seu-email@gmail.com
GMAIL_APP_PASSWORD=sua-senha-de-app
GMAIL_FROM_NAME=

# Targets padrão (opcional)
DEFAULT_DISCORD_TARGET=
DEFAULT_TELEGRAM_TARGET=
DEFAULT_EMAIL_TARGET=
```

### 3. **Infraestrutura (Docker Compose)**

A V2 adiciona **PostgreSQL e Prometheus** à infraestrutura. O `compose.yml` agora sobe:

```yaml
services:
  redis         # já existia
  postgres      # NOVO
  prometheus    # NOVO
```

**Antes (V1):**
```bash
docker compose up -d redis
```

**Agora (V2):**
```bash
docker compose up -d  # sobe todos: redis, postgres, prometheus
```

### 4. **Schema do Banco de Dados**

A V2 **cria o schema automaticamente** na primeira subida da API/Worker. Você **NÃO precisa rodar migrations manualmente**:

- A API/Worker verificam se o PostgreSQL está rodando
- Na primeira conexão, a schema é criada via `db.EnsureSchema()`
- Tabelas criadas: `notifications` e índices otimizados
- Idempotente: rodar múltiplas vezes é seguro

---

## 🚀 Como Iniciar V2

### Opção 1: Windows, duplo clique (mais fácil)

1. **Configure o `.env`:**
   ```bash
   cp .env.example .env
   # edite .env com credenciais reais (Gmail, Telegram, etc.)
   ```

2. **Execute `start.bat`:**
   - Duplo clique em `start.bat`
   - Ele sobe Redis + PostgreSQL + Prometheus
   - Abre a API numa janela
   - Abre o Worker em outra janela
   - Abre `main.html` no navegador (formulário para enviar)

3. **Para parar: duplo clique em `stop.bat`**

### Opção 2: Manual, via terminal

```bash
# 1. Subir infraestrutura (Redis + PostgreSQL + Prometheus)
docker compose up -d

# 2. Configurar variáveis de ambiente
cp .env.example .env
# edite .env com suas credenciais reais

# 3. Baixar dependências
go mod tidy

# 4. Rodar a API (terminal 1)
go run ./cmd/api/main.go

# 5. Rodar o Worker (terminal 2)
go run ./cmd/worker/main.go

# 6. Acessar:
# - Formulário: main.html (abrir no navegador)
# - Interface (enviar + dashboard): main.html
# - API: http://localhost:8080/api/v1/notifications
# - Métricas (API): http://localhost:8080/metrics
# - Métricas (Worker): http://localhost:9091/metrics
# - Prometheus: http://localhost:9090
```

---

## 📚 Como Usar os Novos Recursos

### 1. **Criar Notificação (igual a V1)**

```bash
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{
    "channel": "webhook",
    "target": "https://example.com/hook",
    "message": "Olá!"
  }'
```

**Resposta:**
```json
{
  "id": "23ec8679-0165-4877-92b6-5a904545ccad",
  "status": "pending",
  "message": "notificação aceita e enfileirada para processamento"
}
```

### 2. **Enviar em Lote (NOVO)**

```bash
curl -X POST http://localhost:8080/api/v1/notifications/bulk \
  -H "Content-Type: application/json" \
  -d '[
    {"channel":"webhook","target":"https://a.com","message":"A"},
    {"channel":"discord","message":"B"},
    {"channel":"email","target":"user@example.com","subject":"Teste","message":"C"}
  ]'
```

**Resposta:**
```json
[
  {"id":"751a01b4...","status":"pending"},
  {"id":"2f8c3d9a...","status":"pending"},
  {"status":"rejected","error":"canal de notificação inválido"}
]
```

### 3. **Consultar Status de Uma Notificação (NOVO)**

```bash
curl http://localhost:8080/api/v1/notifications/23ec8679-0165-4877-92b6-5a904545ccad
```

**Resposta:**
```json
{
  "id": "23ec8679-0165-4877-92b6-5a904545ccad",
  "channel": "webhook",
  "target": "https://example.com/hook",
  "message": "Olá!",
  "status": "retrying",
  "attempts": 2,
  "max_attempts": 5,
  "last_error": "webhook: resposta com status 500",
  "next_retry_at": "2026-09-12T06:05:30.591769Z",
  "created_at": "2026-09-12T06:05:17.987722Z",
  "updated_at": "2026-09-12T06:05:26.591769Z"
}
```

### 4. **Listar Notificações com Filtro (NOVO)**

```bash
# Todas em 'retrying'
curl "http://localhost:8080/api/v1/notifications?status=retrying"

# Todas no canal 'webhook'
curl "http://localhost:8080/api/v1/notifications?channel=webhook"

# Combinado + limite
curl "http://localhost:8080/api/v1/notifications?status=dlq&channel=email&limit=10"
```

### 5. **Ver Estatísticas (NOVO)**

```bash
curl http://localhost:8080/api/v1/stats
```

**Resposta:**
```json
{
  "total": 42,
  "pending": 5,
  "success": 30,
  "retrying": 3,
  "dlq": 4,
  "by_channel": {
    "discord": 15,
    "webhook": 20,
    "email": 7
  }
}
```

### 6. **Retry Manual (NOVO)**

Se uma notificação falha e vai para `retrying` ou `dlq`:

```bash
curl -X POST http://localhost:8080/api/v1/notifications/23ec8679-0165-4877-92b6-5a904545ccad/retry
```

**Resposta:**
```json
{
  "id": "23ec8679-0165-4877-92b6-5a904545ccad",
  "status": "pending",
  "attempts": 0,
  "max_attempts": 5,
  "last_error": "",
  "next_retry_at": null
}
```

---

## 🎛️ Dashboard (NOVO)

O `main.html` agora é uma interface única: a barra lateral tem um item **dashboard** que troca a view sem recarregar a página.

**Funcionalidades:**
- **Stats em tempo real** — total, pending, success, retrying, dlq
- **Filtros** — por status ou canal
- **Lista** — ID, canal, status, tentativas, último erro, data de criação
- **Retry manual** — botão para itens em retry/dlq
- **Auto-refresh** — a cada 15 segundos (enquanto a view dashboard está aberta)
- **Links diretos** — Prometheus, métricas da API e do worker, na barra lateral e no rodapé
- **Status da API** — indicador online/offline no rodapé

---

## 🤖 Bot Telegram (NOVO, Opcional)

Se você configurar:
```env
TELEGRAM_BOT_TOKEN=seu-token-do-botfather
TELEGRAM_BOT_ENABLED=true
```

O Worker sobe um bot que escuta:

- `/status <id>` — mostra status completo de uma notificação
- `/retry <id>` — reenfileira manual
- `/help` — mostra comandos disponíveis

---

## 📊 Métricas Prometheus (NOVO)

Com `METRICS_ENABLED=true`, dois endpoints expõem métricas:

**API:**
```
http://localhost:8080/metrics
```

**Worker:**
```
http://localhost:9091/metrics
```

**Métricas disponíveis:**
- `notifications_enqueued_total{channel}` — total aceito
- `notifications_sent_total{channel,result}` — sucesso/falha
- `notifications_retried_total{channel}` — agendado retry
- `notifications_dlq_total{channel}` — esgotadas tentativas
- `notification_send_duration_seconds{channel}` — latência

**Prometheus:**
Acesse `http://localhost:9090` para visualizar gráficos. A config já está pronta em `prometheus.yml`.

---

## 🔄 Retry Automático (NOVO)

Quando uma notificação **falha**, o Worker:

1. Incrementa `attempts` (até `max_attempts`, default 5)
2. Calcula o próximo retry com **backoff exponencial**:
   - Tentativa 1 falha → próxima em 2 segundos
   - Tentativa 2 falha → próxima em 4 segundos
   - Tentativa 3 falha → próxima em 8 segundos
   - ... até o teto de `RETRY_MAX_BACKOFF_SECONDS` (default 3600 = 1h)

3. Se todas as tentativas falharem → `status=dlq`

**RetryPoller:** a cada `RETRY_POLL_INTERVAL_SECONDS` (default 10s), uma goroutine:
- Busca notificações com `next_retry_at` vencido
- Marca como `pending`
- Reenfileira no Redis

---

## 🚫 Dead Letter Queue (DLQ) - NOVO

Notificações que esgotam as tentativas vão para `status=dlq`. Você pode:

1. **Consultar via API:**
   ```bash
   curl "http://localhost:8080/api/v1/notifications?status=dlq"
   ```

2. **Reenfileirar manualmente:**
   ```bash
   curl -X POST http://localhost:8080/api/v1/notifications/{id}/retry
   ```

3. **Ver no Dashboard:** filtro `status=dlq`

4. **Consultar via Bot Telegram:** `/status <id>` mostra a razão da falha

---

## ⚡ Rate Limiting (NOVO)

Cada canal tem seu próprio limite de requisições/segundo:

```env
RATE_LIMIT_DISCORD_RPS=10
RATE_LIMIT_TELEGRAM_RPS=20
RATE_LIMIT_WEBHOOK_RPS=50
RATE_LIMIT_EMAIL_RPS=5
```

Se desabilitar:
```env
RATE_LIMIT_ENABLED=false
```

---

## 🔧 Mudanças de Configuração

### Novas variáveis obrigatórias:
- `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`
- `RETRY_MAX_ATTEMPTS`, `RETRY_MAX_BACKOFF_SECONDS`, `RETRY_POLL_INTERVAL_SECONDS`
- `RATE_LIMIT_ENABLED`, `RATE_LIMIT_*_RPS`
- `METRICS_ENABLED`, `METRICS_PORT`

### Novas variáveis opcionais:
- `TELEGRAM_BOT_ENABLED` (default: false)

### Compatibilidade com V1:
- Todas as variáveis da V1 continuam funcionando
- Infraestrutura (Redis) mantém compatibilidade
- API REST mantém compatibilidade (novos endpoints são adições)

---

## 🔄 Migração de V1 para V2

Se você tinha notificações na V1, elas **não migram automaticamente** para o PostgreSQL. Mas:

1. **Redis Stream continua funcionando** — mensagens antigas são preservadas
2. **RetryPoller recupera mensagens presas** — XAUTOCLAIM re-processa itens que ficaram na fila
3. **Novas notificações vão direto para PostgreSQL**

Para migrar histórico, implemente um script customizado.

---

## 🐛 Troubleshooting

### Erro: "falha ao conectar no PostgreSQL"
- Verifique se PostgreSQL está rodando: `docker compose ps`
- Confirme credenciais no `.env`
- Espere ~10s para PostgreSQL estar pronto (healthcheck)

### Erro: "canal de notificação inválido" ao usar Telegram
- Telegram não era um canal funcional na V1 — corrija confirmando token: `curl http://localhost:8080/api/v1/notifications -d '{"channel":"telegram",...}'`

### Notificações ficam em "retrying" para sempre
- Verifique `last_error` via Dashboard ou API
- Cause comum: URL inválida, credenciais expiradas, rate limit da API externa
- Retry manual: `/retry <id>` após corrigir (zera tentativas)

### Métricas vazias no Prometheus
- Verifique `METRICS_ENABLED=true` no `.env`
- Acesse `http://localhost:9091/metrics` para ver se worker expõe métricas
- Prometheus raspa a cada 15s (padrão) — aguarde

---

## 📝 Checklist de Upgrade V1 → V2

- [ ] Atualizar código: `git pull` ou clonar v2.0.0
- [ ] Instalar dependências: `go mod tidy`
- [ ] Copiar `.env`: `cp .env.example .env`
- [ ] Adicionar credenciais reais no `.env`
- [ ] Subir Docker: `docker compose up -d`
- [ ] Rodar API: `go run ./cmd/api/main.go`
- [ ] Rodar Worker: `go run ./cmd/worker/main.go`
- [ ] Testar via formulário: `main.html`
- [ ] Monitorar via Dashboard: `main.html` (aba "dashboard")

---

## 🚀 Pronto!

Sua instância V2 está pronta para produção. Aproveite persistência, retry automático, métricas e observabilidade!

Dúvidas? Consulte [CHANGELOG_V2.md](CHANGELOG_V2.md) para detalhes das mudanças.
