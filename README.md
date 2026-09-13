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
- [Segurança](#segurança)
- [Idempotência](#idempotência)
- [Health checks](#health-checks)
- [Testes e CI](#testes-e-ci)
- [Arquitetura e decisões técnicas](#arquitetura-e-decisões-técnicas)
- [Upgrade V1 → V2 → V3](#upgrade-v1--v2--v3)
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
├── main.html                    # Interface única: enviar + dashboard + links de monitoramento
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
| `API_KEY` | Opcional (recomendado em produção) | Exige `Authorization: Bearer <chave>` ou `X-API-Key` em `/api/v1/*`. Vazio = sem autenticação |
| `CORS_ALLOWED_ORIGINS` | Opcional | Origens liberadas para chamar a API do navegador, separadas por vírgula. `*` (default) libera qualquer uma |
| `TELEGRAM_ALLOWED_CHAT_IDS` | Opcional (recomendado com o bot ligado) | `chat_id`s autorizados a usar `/retry` e `/status`, separados por vírgula. Vazio = qualquer chat |
| `ALLOW_PRIVATE_NETWORK_TARGETS` | Opcional | `true` desliga a proteção contra SSRF em webhook/discord. **Nunca em produção** |

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

**Com Idempotency-Key** (evita duplicar a entrega se o cliente reenviar a mesma requisição, ex: após um timeout):
```bash
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Idempotency-Key: pedido-12345" \
  -H "Content-Type: application/json" \
  -d '{"channel":"webhook","target":"https://example.com/hook","message":"Olá!"}'
```
Reenviar a mesma requisição com a mesma `Idempotency-Key` devolve a notificação já criada, sem enfileirar de novo.

**Com API Key** (se `API_KEY` estiver configurada):
```bash
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Authorization: Bearer sua-api-key" \
  -H "Content-Type: application/json" \
  -d '{"channel":"webhook","target":"https://example.com/hook","message":"Olá!"}'
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

### `GET /health` / `GET /health/live` / `GET /health/ready`

Veja a seção [Health checks](#health-checks).

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

Abra `main.html` (com a API rodando) e clique em **dashboard** na barra lateral para ver, em tempo real: total de notificações por status, lista filtrável por status/canal, último erro de cada uma, e um botão de retry manual para itens em `retrying`/`dlq`. A mesma página também traz links diretos para Prometheus e para os endpoints de métricas. É uma página estática que fala direto com a API via `fetch` (CORS liberado para uso local).

---

## Segurança

- **Autenticação da API** (`API_KEY`): quando configurada, todo endpoint de negócio (`/api/v1/*`) exige `Authorization: Bearer <chave>` ou `X-API-Key: <chave>`. `/health*` e `/metrics` continuam sempre abertos. Vazio (default) desativa a autenticação — adequado só para uso local.
- **CORS configurável** (`CORS_ALLOWED_ORIGINS`): lista de origens separadas por vírgula. `*` (default) libera qualquer uma; em produção, configure com as origens reais do seu frontend.
- **Proteção contra SSRF**: os canais `webhook` e `discord` fazem uma requisição HTTP para uma URL fornecida pelo cliente. A engine bloqueia por padrão qualquer destino que resolva para um IP privado, loopback, link-local (o que cobre o endereço de metadados de nuvem `169.254.169.254`) — tanto na criação quanto no momento do envio (fecha a brecha de DNS rebinding). O canal `discord` também exige que o host seja `discord.com`/`discordapp.com`. Veja [ARCHITECTURE.md](ARCHITECTURE.md#proteção-contra-ssrf-server-side-request-forgery) para detalhes.
- **Autorização do bot do Telegram** (`TELEGRAM_ALLOWED_CHAT_IDS`): restringe `/retry` e `/status` a uma lista de `chat_id`. Sem essa lista, qualquer um que descubra o bot pode usá-lo.

---

## Idempotência

`POST /api/v1/notifications` aceita um header `Idempotency-Key` opcional. Se você reenviar a mesma requisição com a mesma chave (ex: porque não teve certeza se a primeira tentativa chegou), a API devolve a notificação já criada da primeira vez, em vez de criar uma duplicata e enfileirar de novo. A chave tem uma constraint `UNIQUE` no PostgreSQL — mesmo duas requisições concorrentes com a mesma chave resultam em uma única notificação.

No `/bulk`, como não há um header por item, cada item do array pode incluir `"idempotency_key": "..."` diretamente no corpo.

---

## Health checks

| Endpoint | Uso |
|---|---|
| `GET /health` (alias de `/health/live`) | **Liveness** — só confirma que o processo está de pé. Não verifica dependências. |
| `GET /health/ready` | **Readiness** — verifica se PostgreSQL e Redis estão realmente alcançáveis. Retorna `503` se algum estiver fora, `200` se ambos OK. |

Use `/health/live` para o orquestrador saber quando reiniciar o processo, e `/health/ready` para saber quando ele já pode receber tráfego.

---

## Testes e CI

```bash
go test ./...          # roda a suíte de testes unitários
go test -race ./...    # com o detector de race conditions (recomendado dado o uso de goroutines)
gofmt -l .              # confere formatação
go vet ./...            # análise estática
```

Cobertura atual: `domain`, `retry` (backoff/jitter), `security` (proteção SSRF), `ratelimit`, `service` (idempotência, fallback de consistência, retry manual) e `worker` (dispatcher: sucesso, retry, DLQ, canal não registrado) têm testes unitários com fakes em memória — sem depender de Postgres/Redis reais. O pipeline (`.github/workflows/ci.yml`) roda tudo isso automaticamente a cada push/PR para `main`.

---

## Arquitetura e decisões técnicas

Para o diagrama completo do fluxo e o *porquê* das decisões técnicas mais importantes — por que Redis Streams (e não Pub/Sub), como o Consumer Group evita processamento duplicado, quais são as garantias reais de entrega (at-least-once), como o backoff com jitter funciona, como a consistência entre PostgreSQL e Redis é mantida sem um Transactional Outbox completo, e os detalhes da proteção contra SSRF — veja **[ARCHITECTURE.md](ARCHITECTURE.md)**.

---

## Upgrade V1 → V2 → V3

Se você estava usando a V1, consulte **[UPGRADE_V2_GUIDE.md](UPGRADE_V2_GUIDE.md)** para:

- Alterações necessárias para iniciar (novas dependências, variáveis de ambiente, infraestrutura Docker)
- Passo-a-passo de setup e teste da V2
- Documentação completa dos novos recursos (Bulk API, Retry manual, Dashboard, Bot, Métricas)
- Troubleshooting e checklist de migração

Para detalhes técnicos das mudanças da V2, veja **[CHANGELOG_V2.md](CHANGELOG_V2.md)**; para a rodada de testes/segurança/confiabilidade (V3), veja **[CHANGELOG_V3.md](CHANGELOG_V3.md)**.

---

## Licença

[MIT](LICENSE)
