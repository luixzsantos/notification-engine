# Benchmarks

Este documento registra medições reais de desempenho do Notification Engine,
feitas com as ferramentas `cmd/loadtest` (gerador de carga) e `cmd/echoserver`
(alvo de webhook que responde instantaneamente, para isolar a medição da
capacidade da própria engine da latência de um serviço externo real).
Nenhuma das duas ferramentas faz parte do runtime de produção.

## Ambiente

- Windows 11, Docker Desktop (WSL2) rodando PostgreSQL 16 e Redis 7 via `compose.yml`, tudo em `localhost`
- `go run` (sem build otimizado) para API, Worker e ferramentas — números em produção com binário compilado tendem a ser iguais ou melhores
- `ALLOW_PRIVATE_NETWORK_TARGETS=true` só nesta sessão de benchmark, para o webhook poder apontar para o `echoserver` local (`localhost`) sem ser bloqueado pela proteção contra SSRF — **nunca ligue isso em produção**
- Reproduzir:
  ```bash
  go run ./cmd/echoserver -port 8090
  WORKER_CONCURRENCY=N RATE_LIMIT_ENABLED=false ALLOW_PRIVATE_NETWORK_TARGETS=true go run ./cmd/worker/main.go
  ALLOW_PRIVATE_NETWORK_TARGETS=true go run ./cmd/api/main.go
  go run ./cmd/loadtest -n 3000 -concurrency 100 -target http://localhost:8090/
  ```

## O que é medido

- **Fase 1 (ingestão)**: tempo entre o `loadtest` disparar `POST /api/v1/notifications` e receber `202 Accepted`. Reflete a capacidade da API + escrita no Postgres + `XADD` no Redis — independe de quantos workers existem.
- **Fase 2 (fim-a-fim)**: tempo desde o início da criação até **todas** as notificações do lote saírem de `pending`/`retrying` (sucesso ou DLQ), medido via `GET /api/v1/stats`. Reflete a capacidade real de *entrega*, e é isso que escala com `WORKER_CONCURRENCY`.

## Resultado 1: escalonamento horizontal do worker (sem rate limit)

N=3000 notificações, 100 requisições de criação simultâneas, canal `webhook` apontando para o `echoserver` (latência ~0), `RATE_LIMIT_ENABLED=false` (mede a capacidade real da engine, sem o limitador artificial de RPS por canal mascarando o resultado).

| `WORKER_CONCURRENCY` | Ingestão (fase 1) | Fim-a-fim (fase 2) | Erros |
|---:|---:|---:|---:|
| 1  | 4 116 req/s | **456,9** notif/s | 0 |
| 2  | 5 544 req/s | **877,6** notif/s | 0 |
| 5  | 4 241 req/s | **1 505,3** notif/s | 0 |
| 10 | 4 117 req/s | **1 981,8** notif/s | 0 |

**Leitura**: a ingestão (fase 1) fica sempre na mesma faixa (~4-5,5 mil req/s) porque não depende do worker — é gargalo de API+Postgres+Redis. A entrega fim-a-fim (fase 2), que é o trabalho do worker, escala de forma consistente com o número de goroutines consumidoras: **de 1 para 10 workers, o throughput fim-a-fim multiplicou por ~4,3x**. O ganho é sub-linear (não 10x) porque a partir de um certo ponto o gargalo passa a ser I/O local (Postgres/Redis via Docker Desktop no Windows) em vez de CPU — em produção, com Postgres/Redis dedicados e não compartilhando a mesma máquina/rede virtualizada, o ganho por worker adicional tende a ser maior.

## Resultado 2: o rate limiter é o teto real, não o número de workers

Mesmo teste, mas com `RATE_LIMIT_ENABLED=true` e `RATE_LIMIT_WEBHOOK_RPS=50` (valores default do `.env.example`), N=500:

| `WORKER_CONCURRENCY` | Fim-a-fim |
|---:|---:|
| 1  | 54,9 notif/s |
| 10 | 55,3 notif/s |

**Leitura**: com o rate limit ligado, 1 worker ou 10 workers entregam praticamente a mesma taxa (~55/s, o esperado dado o burst do token bucket em cima de 50 RPS configurados). Isso confirma um detalhe de implementação importante: `ratelimit.Limiters` é uma única instância compartilhada por **todos** os workers de um mesmo processo — o limite é por canal, não por worker. Aumentar `WORKER_CONCURRENCY` só ajuda a escalar a entrega até o teto do rate limit configurado; depois disso, o parâmetro que importa é `RATE_LIMIT_*_RPS`.

## Achado: pool de conexões do PostgreSQL sem limite

A primeira rodada com `WORKER_CONCURRENCY=5` (antes da correção abaixo) produziu 25 erros de criação e **4 notificações presas permanentemente em `pending`**, mesmo após vários minutos. Investigando:

```
[dispatcher] falha ao persistir sucesso de <id>: db: falha ao atualizar notificação <id>:
pq: sorry, too many clients already (53300)
```

Causa raiz: `db.Connect` chamava `sql.Open` sem `SetMaxOpenConns` — sob um pico de concorrência (a API recebendo 100 requisições simultâneas do `loadtest`, somado ao worker e ao retry-poller), o número de conexões que o processo tentava abrir ao mesmo tempo passou do `max_connections=100` do PostgreSQL. O envio ao canal já tinha sido feito com sucesso (o `echoserver` recebeu a requisição), mas o `UPDATE` que gravaria isso no Postgres falhava — e como o dispatcher só logava o erro sem tentar de novo, a notificação ficava presa em `pending` para sempre: **entregue de verdade, mas o sistema nunca saberia disso**.

Correção aplicada (ver commit): `db.Connect` agora recebe um `maxOpenConns` configurável (`DB_MAX_OPEN_CONNS`, default `25`) e chama `SetMaxOpenConns`/`SetMaxIdleConns` — o driver passa a enfileirar a goroutine até uma conexão do pool ficar livre, em vez de tentar abrir mais uma. Além disso, o `Dispatcher` ganhou `updateWithRetry` (até 3 tentativas com um pequeno atraso) para o `Repo.Update` do resultado final, cobrindo qualquer outra falha transitória do mesmo tipo. Repetindo a mesma rodada (`WORKER_CONCURRENCY=5`, N=3000) depois da correção: **0 erros, 0 notificações presas, throughput subiu para 1 505,3 notif/s** (a tabela acima já reflete a versão corrigida).

Esse é exatamente o tipo de problema que só aparece sob carga real — nenhum teste unitário com fakes o pegaria, porque ele depende do comportamento real do driver do Postgres sob contenção de conexões. Veja também [RESILIENCE_TESTING.md](RESILIENCE_TESTING.md) para os demais cenários de falha testados manualmente.
