# Changelog - V3.0.0

## 🎯 Overview

A **v3.0.0** não adiciona canais nem funcionalidades novas de negócio —
depois da V2 já ter coberto persistência, retry/DLQ, bulk, métricas, bot e
dashboard, esta rodada foca em **qualidade, segurança e confiabilidade**:
transformar um projeto de portfólio tecnicamente interessante em algo com
características muito mais próximas de uma aplicação backend profissional.

- ✅ **Suíte de testes unitários** (domain, retry, security, ratelimit, service, worker, bot, config)
- ✅ **CI no GitHub Actions** (`gofmt`, `go vet`, `go build`, `go test`, `go test -race`)
- ✅ **Idempotência** via header `Idempotency-Key` (constraint `UNIQUE` no Postgres)
- ✅ **Consistência Postgres/Redis**: falha ao enfileirar não deixa mais notificação órfã
- ✅ **Proteção contra SSRF** em webhook/discord (bloqueio de IP privado/interno + defesa contra DNS rebinding)
- ✅ **Autenticação da API** via `API_KEY` (Bearer token ou `X-API-Key`)
- ✅ **Autorização do bot do Telegram** via lista de `chat_id`
- ✅ **CORS configurável** por variável de ambiente
- ✅ **Health checks separados** (liveness vs. readiness)
- ✅ **Jitter no backoff exponencial** (evita thundering herd)
- ✅ **ARCHITECTURE.md** com diagrama e decisões técnicas documentadas

---

## 📝 Mudanças detalhadas

### 1. Testes unitários

Cobertura nova em `internal/domain`, `internal/retry`, `internal/security`,
`internal/ratelimit`, `internal/service`, `internal/worker`, `internal/bot` e
`internal/config` — todos usando fakes em memória para
`domain.Repository`/`domain.Producer`, sem depender de Postgres/Redis reais.

Testes de integração reais com SQL/Redis Streams ficaram fora do escopo
desta rodada (validados manualmente contra os serviços via `docker compose`)
— um próximo passo natural seria Testcontainers ou similar.

### 2. CI (`.github/workflows/ci.yml`)

Roda em todo push/PR para `main`: `gofmt -l` (falha se algo não estiver
formatado), `go vet`, `go build`, `go test` e `go test -race`.

### 3. Idempotência (`Idempotency-Key`)

- `domain.Notification` ganhou o campo `IdempotencyKey`.
- Nova coluna `idempotency_key` + índice único parcial
  (`WHERE idempotency_key <> ''`) no Postgres — duas notificações não podem
  compartilhar a mesma chave.
- `NotificationService.CreateNotification`: se a chave já existir, devolve a
  notificação existente em vez de criar duplicata (cobre inclusive a corrida
  entre duas requisições simultâneas com a mesma chave).
- `/bulk` aceita `idempotency_key` por item no corpo JSON (não há como
  mandar um header por item).

### 4. Consistência PostgreSQL/Redis

Problema: se `producer.Enqueue` falhar logo depois de `repo.Create` ter
persistido a notificação, ela ficava presa em `status=pending` para sempre —
sem nenhum processo responsável por ela.

Solução: essa falha agora é tratada com a mesma lógica de uma falha de
entrega comum (`retry.ApplyFailure`) — a notificação recebe
`status=retrying` com backoff, e o `RetryPoller` (que já existia) a
reenfileira automaticamente quando o prazo vence. Deliberadamente mais
simples que um Transactional Outbox completo — veja a justificativa em
[ARCHITECTURE.md](ARCHITECTURE.md#consistência-entre-postgresql-e-redis).

### 5. Proteção contra SSRF (`internal/security`, novo pacote)

- `security.ValidateTargetURL`: checagem síncrona na criação (rejeita
  `localhost`, IPs privados/loopback/link-local literais, esquemas
  diferentes de http/https).
- `security.SafeHTTPClient`: `http.Client` cujo `Transport.DialContext`
  resolve o hostname e recusa a conexão se o IP resolvido cair numa faixa
  bloqueada — fecha a brecha de DNS rebinding que a checagem síncrona
  sozinha não cobre. Usado pelos senders de `webhook` e `discord`.
- `discord.go` ganhou uma checagem adicional: o host do `target` precisa
  terminar em `discord.com`/`discordapp.com`.
- Nova variável `ALLOW_PRIVATE_NETWORK_TARGETS` (default `false`) para
  desligar a proteção em desenvolvimento local.

### 6. Autenticação da API (`API_KEY`)

Middleware `requireAPIKey` em `cmd/api/main.go`, aplicado a todos os
endpoints `/api/v1/*`. Aceita `Authorization: Bearer <chave>` ou
`X-API-Key: <chave>`. Vazio (default) mantém o comportamento anterior (sem
autenticação) para não quebrar quem só roda localmente — a API loga um
aviso no startup quando isso acontece.

### 7. Autorização do bot do Telegram (`TELEGRAM_ALLOWED_CHAT_IDS`)

`bot.New` agora recebe a lista de `chat_id`s autorizados; `/retry` e
`/status` respondem "não autorizado" para qualquer chat fora da lista. Lista
vazia (default) mantém o comportamento anterior.

### 8. CORS configurável (`CORS_ALLOWED_ORIGINS`)

`corsMiddleware` deixou de fixar `Access-Control-Allow-Origin: *` e passou a
aceitar uma lista de origens via variável de ambiente. `*` continua sendo o
default.

### 9. Health checks separados

- `GET /health` (alias `GET /health/live`): liveness, sempre `200`.
- `GET /health/ready` (novo): readiness — `200` só se PostgreSQL e Redis
  responderem ao ping, `503` caso contrário.

### 10. Jitter no backoff exponencial

`retry.NextBackoff` deixou de retornar um valor fixo por tentativa e passou
a sortear uniformemente entre 1s e o teto daquela tentativa ("full jitter").
Evita que um lote de notificações que falhou junto seja reenviado todo no
mesmo instante.

### 11. `ARCHITECTURE.md`

Novo documento com diagrama (Mermaid) do fluxo completo e a justificativa
por trás das decisões técnicas mais importantes: por que Redis Streams, como
o Consumer Group evita duplicidade, garantias de entrega (at-least-once),
mecânica do retry, consistência Postgres/Redis, e a proteção contra SSRF em
detalhe.

---

## 🚫 Fora do escopo (decisão deliberada)

- **Transactional Outbox completo**: o problema de consistência foi resolvido
  reaproveitando o RetryPoller já existente (item 4 acima), com uma fração
  da complexidade operacional de uma tabela de outbox + publisher dedicado.
- **Migrations com ferramenta dedicada** (ex: `golang-migrate`): o
  `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` já resolveu bem a evolução do
  schema neste projeto (testado na prática ao adicionar `attachments` e
  `idempotency_key`); a complexidade de uma ferramenta de migration separada
  não se paga no estágio atual.
- **Testes de integração com Postgres/Redis reais**: mencionado no item 1.

---

## 📜 Licença

MIT
