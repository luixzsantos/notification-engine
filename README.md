# 📨 Webhook & Notification Engine

Serviço assíncrono de alto desempenho para disparo de notificações multicanais (**Discord**, **Telegram**, **Gmail** e **Webhooks genéricos**), construído em **Go** seguindo os princípios de **Clean Architecture**, com concorrência nativa via goroutines, fila de processamento no **Redis Streams**, persistência/auditoria em **PostgreSQL**, retry com backoff exponencial, rate limiting por canal e métricas Prometheus.

---

## Índice

- [O que o projeto faz](#o-que-o-projeto-faz)
- [Arquitetura](#arquitetura)
- [Stack](#stack)
- [Estrutura de pastas](#estrutura-de-pastas)
- [Pré-requisitos](#pré-requisitos)
- [Como rodar](#como-rodar)
- [Variáveis de ambiente](#variáveis-de-ambiente)
- [Uso da API](#uso-da-api)
- [Retry e Dead Letter Queue (DLQ)](#retry-e-dead-letter-queue-dlq)
- [Rate limiting](#rate-limiting)
- [Métricas (Prometheus)](#métricas-prometheus)
- [Bot do Telegram](#bot-do-telegram)
- [Dashboard](#dashboard)
- [Licença](#licença)

---

## O que o projeto faz

Em resumo: é um "correio automático". Você manda um pedido de notificação pra API, ela responde na hora (sem te fazer esperar a entrega de verdade), guarda o pedido numa fila, e um processo separado (o **Worker**) entrega essa notificação no Discord, Telegram, Gmail ou qualquer Webhook — em segundo plano, com múltiplas entregas acontecendo em paralelo.

Essa separação entre "receber o pedido" (API) e "entregar de verdade" (Worker) é o que permite o sistema aguentar picos de volume sem travar, e continuar funcionando mesmo se um canal específico estiver fora do ar. Quando um envio falha, a notificação não é descartada: o worker agenda novas tentativas com backoff exponencial e, se todas falharem, ela vai para uma DLQ consultável via API, bot ou dashboard.

---

## Arquitetura

```
Cliente → POST /api/v1/notifications → API (Go) ── grava ──→ PostgreSQL
                                          │                    (estado/auditoria)
                                          ▼                        ▲
                                 Redis Stream (fila)                │
                                          │                         │
                                          ▼                         │
                          Worker (N goroutines consumidoras)        │
                              │           │           │      │      │
                          rate limit  dispatcher  atualiza status ──┘
                              │           │
                ┌─────────────┼───────────┼────────────┐
                ▼             ▼           ▼             ▼
            Discord       Telegram      Gmail        Webhook
            Webhook       Bot API       (SMTP)       genérico

  Falha? → agenda retry (backoff exponencial) ou move para DLQ
              │
              ▼
      RetryPoller (goroutine do worker, varre o Postgres a cada N segundos)
              │
              └──→ reenfileira no Redis Stream quando o retry vence
```

1. O cliente faz um `POST` para `/api/v1/notifications` (via `curl`, `Invoke-RestMethod`, ou a interface web `main.html`).
2. A API valida o payload, **persiste no PostgreSQL** (status `pending`) e publica o evento no Redis Stream, respondendo **202 Accepted** com um `id` de rastreio.
3. O Worker consome o stream com múltiplas goroutines no mesmo *consumer group* (processamento paralelo, sem duplicidade de entrega), aplica **rate limiting por canal** e dispara pelo conector correspondente.
4. Sucesso → status `success`. Falha → status `retrying` (com backoff exponencial) ou `dlq` (tentativas esgotadas). O estado sempre é persistido no Postgres.
5. Um `RetryPoller` (goroutine do worker) varre periodicamente o banco por notificações com retry vencido e as reenfileira automaticamente.
6. Um bot do Telegram (opcional) e um dashboard web permitem consultar status e forçar retry manualmente.

---

## Stack

- **Go 1.22+** — API e Worker como binários independentes
- **Redis Streams** — fila de processamento assíncrono com consumer groups
- **PostgreSQL** — persistência de estado, auditoria e base para retry/DLQ
- **Prometheus client** — métricas de envio, retry e DLQ
- **net/http** (stdlib) — API REST, sem framework externo
- **golang.org/x/time/rate** — rate limiting (token bucket) por canal
- Bot do Telegram via long polling puro (sem SDK), no mesmo estilo dos conectores de canal

---

## Estrutura de pastas

```
notification-engine/
├── cmd/
│   ├── api/main.go              # Entry point da API REST
│   └── worker/main.go           # Entry point do worker consumidor
├── internal/
│   ├── config/                  # Leitura de variáveis de ambiente
│   ├── domain/                  # Entidades, interfaces (Producer/Consumer/Sender/Repository)
│   ├── handler/                 # Controladores HTTP
│   ├── service/                 # Regras de negócio (criar, listar, retry, bulk)
│   ├── queue/                   # Producer/Consumer do Redis Streams
│   ├── channel/                 # Conectores: discord, telegram, gmail, webhook
│   ├── db/                      # Conexão PostgreSQL + repositório + schema.sql
│   ├── worker/                  # Dispatcher (rate limit + retry/DLQ) + RetryPoller
│   ├── ratelimit/                # Token bucket por canal
│   ├── retry/                   # Cálculo de backoff exponencial
│   ├── metrics/                 # Contadores/histogramas Prometheus
│   └── bot/                     # Bot do Telegram (/status, /retry)
├── main.html                    # Interface web para enviar notificações
├── dashboard.html                # Dashboard: stats, lista, retry manual
├── prometheus.yml                # Config de scrape do Prometheus
├── start.bat / stop.bat           # Sobe/derruba tudo (Windows)
├── compose.yml                   # Redis + PostgreSQL + Prometheus (dev)
├── go.mod
└── .env.example
```

---

## Pré-requisitos

- [Go 1.22+](https://go.dev/dl/)
- [Docker + Docker Compose](https://www.docker.com/products/docker-desktop/) (Docker Desktop precisa estar **aberto** antes de rodar)

> **Windows:** evite instalar o projeto dentro de pastas sincronizadas pelo OneDrive — use uma pasta local simples como `C:\dev\notification-engine`, para não sofrer com arquivos "placeholder" incompletos.

---

## Como rodar

### Opção 1 — Windows, com um duplo clique (mais fácil)

1. Copie `.env.example` para `.env` e preencha as credenciais dos canais que for usar.
2. Dê **dois cliques** em `start.bat`. Ele sobe Redis + PostgreSQL + Prometheus, abre a API numa janela e o Worker em outra.
3. Para encerrar tudo, dê dois cliques em `stop.bat`.

### Opção 2 — Manual, via terminal

```bash
# 1. Subir a infraestrutura (Redis + PostgreSQL + Prometheus)
docker compose up -d

# 2. Configurar variáveis de ambiente
cp .env.example .env
# edite o .env com suas credenciais

# 3. Baixar dependências
go mod tidy

# 4. Rodar a API (terminal 1)
go run cmd/api/main.go

# 5. Rodar o Worker (terminal 2)
go run cmd/worker/main.go
```

A API sobe em `http://localhost:8080`. O schema do PostgreSQL é criado automaticamente na primeira subida (não precisa rodar migration manual).

---

## Variáveis de ambiente

Veja todos os detalhes em [`.env.example`](.env.example). Resumo:

| Variável | Obrigatória para | Descrição |
|---|---|---|
| `REDIS_ADDR` | Sempre | Endereço do Redis (default: `localhost:6379`) |
| `DB_HOST` / `DB_PORT` / `DB_USER` / `DB_PASSWORD` / `DB_NAME` | Sempre | Conexão com o PostgreSQL |
| `TELEGRAM_BOT_TOKEN` | Canal `telegram` e/ou bot | Token gerado pelo [@BotFather](https://t.me/BotFather) |
| `TELEGRAM_BOT_ENABLED` | Bot do Telegram | `true` para o worker escutar comandos `/status` e `/retry` |
| `GMAIL_USERNAME` / `GMAIL_APP_PASSWORD` | Canal `email` | Conta Gmail e [senha de app](https://myaccount.google.com/apppasswords) (requer 2FA ativo) |
| `DEFAULT_DISCORD_TARGET` / `DEFAULT_TELEGRAM_TARGET` / `DEFAULT_EMAIL_TARGET` | Opcional | Usados quando `target` não é enviado na requisição |
| `WORKER_CONCURRENCY` | Opcional | Nº de goroutines consumidoras (default: `10`) |
| `RETRY_MAX_ATTEMPTS` | Opcional | Tentativas antes de mover para a DLQ (default: `5`) |
| `RETRY_MAX_BACKOFF_SECONDS` | Opcional | Teto do backoff exponencial (default: `3600`) |
| `RATE_LIMIT_*_RPS` | Opcional | Requisições/segundo por canal (default: 10/20/50/5) |
| `METRICS_ENABLED` / `METRICS_PORT` | Opcional | Liga `/metrics` na API e no worker (porta separada) |

---

## Uso da API

### `POST /api/v1/notifications`

**Webhook genérico**
```json
{"channel":"webhook","target":"https://example.com/hook","message":"Olá!"}
```

**Discord**
```json
{"channel":"discord","target":"https://discord.com/api/webhooks/...","message":"Olá!"}
```

**Telegram**
```json
{"channel":"telegram","target":"<chat_id>","message":"Olá!"}
```

**Gmail**
```json
{"channel":"email","target":"destinatario@exemplo.com","subject":"Assunto","message":"Olá!"}
```

**Resposta (202 Accepted)**
```json
{"id": "441b54a5-...", "status": "pending", "message": "notificação aceita e enfileirada para processamento"}
```

### `POST /api/v1/notifications/bulk`

Aceita um array de até 500 notificações no mesmo formato acima. Retorna um array com o resultado de cada item (`id`+`status`, ou `error` quando rejeitado na validação):

```json
[
  {"channel":"discord","message":"um"},
  {"channel":"webhook","target":"https://x.com","message":"dois"}
]
```

### `GET /api/v1/notifications?status=&channel=&limit=`

Lista as notificações mais recentes, com filtros opcionais por `status` (`pending`/`success`/`retrying`/`dlq`) e `channel`.

### `GET /api/v1/notifications/{id}`

Consulta uma notificação específica (status, tentativas, último erro).

### `POST /api/v1/notifications/{id}/retry`

Reenfileira manualmente uma notificação parada em `retrying` ou `dlq`.

### `GET /api/v1/stats`

Contagem total de notificações por status e por canal — usado pelo dashboard.

### `GET /health`

Healthcheck simples, retorna `{"status": "ok"}`.

---

## Retry e Dead Letter Queue (DLQ)

Quando o envio a um canal falha, o worker **não descarta** a notificação:

1. Incrementa `attempts` e grava o erro (`last_error`) no PostgreSQL.
2. Se `attempts < RETRY_MAX_ATTEMPTS`: status vira `retrying`, com `next_retry_at` calculado por backoff exponencial (2s, 4s, 8s, 16s... até `RETRY_MAX_BACKOFF_SECONDS`).
3. Se as tentativas se esgotaram (ou o canal nem existe): status vira `dlq`, e a notificação fica parada até uma ação manual.

O `RetryPoller` (goroutine do worker) varre o banco a cada `RETRY_POLL_INTERVAL_SECONDS` procurando notificações com `next_retry_at` vencido e as reenfileira automaticamente no Redis. O PostgreSQL é a única fonte de verdade sobre o que precisa de retry — o Redis Stream é só o transporte.

Notificações em `dlq` podem ser reenviadas manualmente via `POST /notifications/{id}/retry`, pelo bot do Telegram (`/retry <id>`) ou pelo botão "Retry" no dashboard.

---

## Rate limiting

Cada canal tem um limite de requisições por segundo independente (token bucket via `golang.org/x/time/rate`), configurável em `RATE_LIMIT_*_RPS`. Isso evita que um pico de volume sature a API externa do Discord/Telegram e gere bloqueios. Pode ser desligado globalmente com `RATE_LIMIT_ENABLED=false`.

---

## Métricas (Prometheus)

Com `METRICS_ENABLED=true`:

- API expõe `GET /metrics` em `http://localhost:8080/metrics`
- Worker expõe `GET /metrics` em `http://localhost:9091/metrics` (porta separada, configurável em `METRICS_PORT`)

Métricas expostas: `notifications_enqueued_total`, `notifications_sent_total{channel,result}`, `notifications_retried_total`, `notifications_dlq_total`, `notification_send_duration_seconds`.

O `compose.yml` já sobe um Prometheus (`http://localhost:9090`) configurado para fazer scrape dos dois endpoints via `prometheus.yml`.

---

## Bot do Telegram

Com `TELEGRAM_BOT_TOKEN` e `TELEGRAM_BOT_ENABLED=true`, o worker sobe um bot que escuta comandos via long polling:

- `/status <id>` — retorna canal, status, tentativas e último erro de uma notificação
- `/retry <id>` — reenfileira uma notificação em `retrying` ou `dlq`

---

## Dashboard

Abra `dashboard.html` (com a API rodando) para ver, em tempo real: total de notificações por status, lista filtrável por status/canal, último erro de cada uma, e um botão de retry manual para itens em `retrying`/`dlq`. Assim como `main.html`, é uma página estática que fala direto com a API via `fetch` (CORS liberado para uso local).

---

## Licença

[MIT](LICENSE)
