# 📨 Webhook & Notification Engine

Serviço assíncrono de alto desempenho para disparo de notificações multicanais (**Discord**, **Telegram**, **Gmail** e **Webhooks genéricos**), construído em **Go** seguindo os princípios de **Clean Architecture**, com concorrência nativa via goroutines e fila de processamento no **Redis Streams**.

> 📘 **Primeira vez rodando o projeto?** Siga o [Guia Passo a Passo](GUIA-PASSO-A-PASSO.md) — cobre desde a instalação do Go/Docker até o teste de cada canal, com solução dos erros mais comuns.

---

## Índice

- [O que o projeto faz](#o-que-o-projeto-faz)
- [Arquitetura](#arquitetura)
- [Stack](#stack)
- [Estrutura de pastas](#estrutura-de-pastas)
- [Pré-requisitos](#pré-requisitos)
- [Como rodar](#como-rodar)
- [Variáveis de ambiente](#variáveis-de-ambiente)
- [Formas de enviar notificações](#formas-de-enviar-notificações)
- [Uso da API](#uso-da-api)
- [Roadmap (V2)](#roadmap-v2)
- [Licença](#licença)

---

## O que o projeto faz

Em resumo: é um "correio automático". Você manda um pedido de notificação pra API, ela responde na hora (sem te fazer esperar a entrega de verdade), guarda o pedido numa fila, e um processo separado (o **Worker**) entrega essa notificação no Discord, Telegram, Gmail ou qualquer Webhook — em segundo plano, com múltiplas entregas acontecendo em paralelo.

Essa separação entre "receber o pedido" (API) e "entregar de verdade" (Worker) é o que permite o sistema aguentar picos de volume sem travar, e continuar funcionando mesmo se um canal específico estiver fora do ar.

---

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

1. O cliente faz um `POST` para `/api/v1/notifications` (via `curl`, `Invoke-RestMethod`, ou a interface web `enviar.html`).
2. A API valida o payload e publica o evento no Redis Stream, respondendo **202 Accepted** com um `id` de rastreio — em milissegundos, sem esperar a entrega real.
3. O Worker roda de forma independente, com múltiplas goroutines consumindo o mesmo *consumer group* do Redis, garantindo processamento paralelo sem duplicidade de entrega.
4. Cada notificação é roteada ao conector do canal correspondente, que dispara a requisição HTTP (ou SMTP, no caso do Gmail).
5. Sucesso ou falha são logados no terminal do Worker.

---

## Stack

- **Go 1.22+** — API e Worker como binários independentes
- **Redis Streams** — fila de processamento assíncrono com consumer groups
- **net/http** (stdlib) — API REST, sem framework externo
- **go-redis/v9**, **google/uuid**, **joho/godotenv** — únicas dependências externas

---

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
├── enviar.html                  # Interface web para enviar notificações sem terminal
├── iniciar.bat                  # Duplo clique: sobe Redis + API + Worker (Windows)
├── parar.bat                    # Duplo clique: encerra tudo (Windows)
├── start.ps1 / test.ps1 / stop.ps1  # Equivalentes em PowerShell
├── docker-compose.yml           # Redis local para desenvolvimento
├── go.mod
├── .env.example
└── GUIA-PASSO-A-PASSO.md        # Tutorial completo do zero
```

---

## Pré-requisitos

- [Go 1.22+](https://go.dev/dl/)
- [Docker + Docker Compose](https://www.docker.com/products/docker-desktop/) (Docker Desktop precisa estar **aberto** antes de rodar)

> **Windows:** evite instalar o projeto dentro de pastas sincronizadas pelo OneDrive — use uma pasta local simples como `C:\dev\notification-engine`, para não sofrer com arquivos "placeholder" incompletos.

---

## Como rodar

### Opção 1 — Windows, com um duplo clique (mais fácil)

1. Extraia o projeto, copie `.env.example` para `.env` e preencha as credenciais dos canais que for usar (veja a seção abaixo).
2. Dê **dois cliques** em `iniciar.bat`. Ele sobe o Redis, abre a API numa janela e o Worker em outra.
3. Para encerrar tudo, dê dois cliques em `parar.bat`.

### Opção 2 — Manual, via terminal

```bash
# 1. Subir o Redis local
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

A API sobe em `http://localhost:8080` por padrão.

---

## Variáveis de ambiente

Veja todos os detalhes em [`.env.example`](.env.example). Resumo:

| Variável | Obrigatória para | Descrição |
|---|---|---|
| `REDIS_ADDR` | Sempre | Endereço do Redis (default: `localhost:6379`) |
| `TELEGRAM_BOT_TOKEN` | Canal `telegram` | Token gerado pelo [@BotFather](https://t.me/BotFather) |
| `GMAIL_USERNAME` / `GMAIL_APP_PASSWORD` | Canal `email` | Conta Gmail e [senha de app](https://myaccount.google.com/apppasswords) (requer 2FA ativo) |
| `DEFAULT_DISCORD_TARGET` | Opcional | URL do webhook usada quando `target` não é enviado na requisição |
| `DEFAULT_TELEGRAM_TARGET` | Opcional | Chat ID usado quando `target` não é enviado |
| `DEFAULT_EMAIL_TARGET` | Opcional | E-mail usado quando `target` não é enviado |
| `WORKER_CONCURRENCY` | Opcional | Nº de goroutines consumidoras (default: `10`) |

Configurar os `DEFAULT_*` evita ter que colar a URL/ID/e-mail em toda requisição de teste.

---

## Formas de enviar notificações

### 1. Interface web (`enviar.html`) — recomendado para uso manual

Com a API rodando, dê dois cliques em `enviar.html`. Ele abre no navegador com um formulário: escolha o canal, escreva a mensagem, clique em enviar. Não precisa de terminal nem de montar JSON manualmente.

> A API libera CORS (`Access-Control-Allow-Origin: *`) especificamente para permitir que essa página, aberta direto do disco (`file://`), consiga chamar `localhost:8080`. Isso é adequado para uso local — não é recomendado manter essa configuração em um ambiente de produção exposto publicamente.

### 2. Script de teste automático (`test.ps1`)

Dispara uma notificação de teste para os 4 canais de uma vez, usando os `DEFAULT_*` configurados no `.env`:

```powershell
.\test.ps1
```

### 3. Requisição manual

```powershell
Invoke-RestMethod -Uri "http://localhost:8080/api/v1/notifications" -Method Post -ContentType "application/json" -Body '{"channel":"discord","message":"Olá!"}'
```

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
(`target` pode ser omitido se `DEFAULT_DISCORD_TARGET` estiver configurado)

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
{
  "id": "441b54a5-3fd7-4657-9b22-b3513d5056a4",
  "status": "pending",
  "message": "notificação aceita e enfileirada para processamento"
}
```

### `GET /health`

Healthcheck simples, retorna `{"status": "ok"}`.

---

## Roadmap (V2)

- [ ] Retry com Exponential Backoff + Dead Letter Queue (DLQ)
- [ ] Persistência histórica de auditoria (PostgreSQL/SQLite)
- [ ] Rate limiting por canal/destino
- [ ] Endpoint de envio em lote (`/notifications/bulk`)
- [ ] Bot com resposta automática (Telegram via webhook)
- [ ] Dashboard de métricas de envio

---

## Licença

[MIT](LICENSE)
