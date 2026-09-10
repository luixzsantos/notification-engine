# Webhook & Notification Engine

Serviço assíncrono de alto desempenho para disparo de notificações multicanais (**Discord**, **Telegram**, **Gmail** e **Webhooks genéricos**), construído em **Go** seguindo os princípios de **Clean Architecture**, com concorrência nativa via goroutines e fila de processamento no **Redis Streams**.

## Índice

- [Arquitetura](#arquitetura)
- [Stack](#stack)
- [Estrutura de pastas](#estrutura-de-pastas)
- [Pré-requisitos](#pré-requisitos)
- [Como rodar](#como-rodar)
- [Variáveis de ambiente](#variáveis-de-ambiente)
- [Uso da API](#uso-da-api)
- [Roadmap (V2)](#roadmap-v2)

## Arquitetura

```
Cliente → POST /api/v1/notifications → API (Go)
                                          │
                                          ▼
                                 Redis Stream (fila)
                                          │
                                          ▼
                          Worker (N goroutines consumidoras)
                                          │
                              ┌───────────┼───────────┬────────────┐
                              ▼           ▼           ▼            ▼
                          Discord     Telegram      Gmail       Webhook
                          Webhook     Bot API       (SMTP)      genérico
```

1. O cliente faz um `POST` para `/api/v1/notifications`.
2. A API valida o payload e publica o evento no Redis Stream, respondendo **202 Accepted** com um `id` de rastreio — em milissegundos, sem esperar a entrega real.
3. O Worker roda de forma independente, com múltiplas goroutines consumindo o mesmo *consumer group* do Redis, garantindo processamento paralelo sem duplicidade de entrega.
4. Cada notificação é roteada ao conector do canal correspondente, que dispara a requisição HTTP (ou SMTP, no caso do Gmail).
5. Sucesso ou falha são logados; falhas permanecem na *Pending Entries List* do Redis, servindo de base para a estratégia de retry/DLQ da V2.

## Stack

- **Go 1.22+** — API e Worker como binários independentes
- **Redis Streams** — fila de processamento assíncrono com consumer groups
- **net/http** (stdlib) — API REST, sem framework externo
- **go-redis/v9**, **google/uuid**, **joho/godotenv** — únicas dependências externas

## Estrutura de pastas

```
notification-engine/
├── cmd/
│   ├── api/main.go              # Entry point da API REST
│   └── worker/main.go           # Entry point do worker consumidor
├── internal/
│   ├── config/                  # Leitura de variáveis de ambiente
│   ├── domain/                  # Entidades e interfaces do domínio
│   ├── handler/                 # Controladores HTTP
│   ├── service/                 # Regras de negócio
│   ├── queue/                   # Producer/Consumer do Redis Streams
│   └── channel/                 # Conectores: discord, telegram, gmail, webhook
├── docker-compose.yml           # Redis local para desenvolvimento
├── go.mod
└── .env.example
```

## Pré-requisitos

- [Go 1.22+](https://go.dev/dl/)
- [Docker + Docker Compose](https://www.docker.com/products/docker-desktop/)

## Como rodar

```bash
# 1. Subir o Redis local
docker compose up -d

# 2. Configurar variáveis de ambiente
cp .env.example .env
# edite o .env com suas credenciais (Telegram/Gmail), se for usar esses canais

# 3. Baixar dependências
go mod tidy

# 4. Rodar a API (terminal 1)
go run cmd/api/main.go

# 5. Rodar o Worker (terminal 2)
go run cmd/worker/main.go
```

A API sobe em `http://localhost:8080` por padrão.

## Variáveis de ambiente

Veja todos os detalhes e defaults em [`.env.example`](.env.example). Resumo:

| Variável | Obrigatória para | Descrição |
|---|---|---|
| `REDIS_ADDR` | Sempre | Endereço do Redis (default: `localhost:6379`) |
| `TELEGRAM_BOT_TOKEN` | Canal `telegram` | Token gerado pelo [@BotFather](https://t.me/BotFather) |
| `GMAIL_USERNAME` / `GMAIL_APP_PASSWORD` | Canal `email` | Conta Gmail e [senha de app](https://myaccount.google.com/apppasswords) (requer 2FA ativo) |
| `WORKER_CONCURRENCY` | Opcional | Nº de goroutines consumidoras (default: `10`) |

## Uso da API

### `POST /api/v1/notifications`

**Webhook genérico**
```bash
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{"channel":"webhook","target":"https://example.com/hook","message":"Olá!"}'
```

**Discord**
```bash
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{"channel":"discord","target":"https://discord.com/api/webhooks/...","message":"Olá!"}'
```

**Telegram**
```bash
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{"channel":"telegram","target":"<chat_id>","message":"Olá!"}'
```

**Gmail**
```bash
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{"channel":"email","target":"destinatario@exemplo.com","subject":"Assunto","message":"Olá!"}'
```

**Resposta (202 Accepted)**
```json
{
  "id": "441b54a5-3fd7-4657-9b22-b3513d5056a4",
  "status": "pending",
  "message": "notificação aceita e enfileirada para processamento"
}
```

### `GET /health`

Healthcheck simples, retorna `{"status": "ok"}`.

## Roadmap (V2)

- [ ] Retry com Exponential Backoff + Dead Letter Queue (DLQ)
- [ ] Persistência histórica de auditoria (PostgreSQL/SQLite)
- [ ] Rate limiting por canal/destino
- [ ] Dashboard de métricas de envio

## Licença

[MIT](LICENSE)
