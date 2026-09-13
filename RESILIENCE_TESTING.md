# Testes de resiliência (chaos testing manual)

Este documento registra cenários de falha injetados manualmente contra uma
instância real (Docker Compose + API + Worker rodando localmente), com
evidência (logs, respostas HTTP, linhas do banco) de como o sistema se
comportou em cada um. Objetivo: provar empiricamente as garantias descritas
em [ARCHITECTURE.md](ARCHITECTURE.md), não só documentá-las.

Todos os cenários abaixo foram executados na mesma sessão, contra a versão
do código deste repositório.

---

## 1. Redis cai durante a criação de uma notificação

**Ação**: `docker stop redis-notification`, depois `POST /api/v1/notifications`.

**Esperado**: a API não deveria falhar a requisição nem perder a notificação — ela já documentava esse fallback (consistência Postgres/Redis).

**Resultado**:
```
HTTP 202
{"id":"efff063a-...","status":"retrying", ...}
```
A notificação foi persistida no Postgres e, como o `Enqueue` no Redis falhou, o service tratou isso como uma falha de entrega comum (`retry.ApplyFailure`), agendando um retry em vez de deixá-la presa em `pending`. Erro registrado na própria linha:
```
last_error: redis producer: falha ao publicar no stream "notifications:stream":
read tcp [::1]:58119->[::1]:6379: wsarecv: An established connection was aborted...
```

**Recuperação**: `docker start redis-notification`. Sem nenhuma ação manual, o `RetryPoller` (roda a cada 10s) reenfileirou a notificação assim que o Redis voltou:
```
[retry-poller] reenfileirado id=efff063a-... channel=webhook tentativa=1/5
[dispatcher] sucesso id=efff063a-... channel=webhook target=http://localhost:8090/
```
Status final: `success`. **Nenhuma notificação perdida, nenhuma intervenção manual.**

---

## 2. PostgreSQL cai durante a criação de uma notificação

**Ação**: `docker stop postgres-notification`, depois `POST /api/v1/notifications`.

**Esperado**: diferente do caso do Redis, aqui não há como aceitar a notificação de forma segura — o Postgres é a fonte de verdade, e sem ele não há onde persistir nada. O correto é falhar a requisição (não fingir sucesso), sem derrubar o processo da API.

**Resultado**: `HTTP 500`, API continuou de pé (não crashou), e nenhuma notificação fantasma foi criada.

**Achado colateral corrigido nesta sessão**: a primeira versão desse teste expôs que a API devolvia o erro interno **completo** ao cliente, incluindo detalhes do driver do Postgres:
```json
{"error":"service: falha ao persistir notificação: db: falha ao inserir notificação: dial tcp [::1]:5432: connectex: ..."}
```
Isso é vazamento de informação de infraestrutura para um cliente externo. Corrigido: agora qualquer erro que não seja de validação (`service.IsValidationError`) é logado por completo no servidor e devolvido ao cliente como uma mensagem genérica:
```json
{"error":"falha interna ao processar a notificação, tente novamente"}
```
A mesma correção foi aplicada ao `/bulk` (que tinha o mesmo problema por item do lote).

**Recuperação**: `docker start postgres-notification`. A API **voltou a aceitar notificações normalmente sem precisar reiniciar o processo** — `database/sql` reconecta sozinho assim que o Postgres volta a responder.

---

## 3. Canal externo indisponível (Discord/Telegram/Gmail fora do ar)

**Ação**: derrubado o `echoserver` (simulando o canal externo), configurado `RETRY_MAX_ATTEMPTS=5` (default), criada uma notificação `webhook` apontando para ele.

**Esperado**: retry com backoff (e jitter, ver [ARCHITECTURE.md](ARCHITECTURE.md#retry-backoff-exponencial-com-jitter)) até esgotar as tentativas, então DLQ.

**Resultado** (log real do worker):
```
tentativa=1/5 em=...T04:31:28Z
tentativa=2/5 em=...T04:31:39Z   (reenfileirado pelo RetryPoller em 04:31:35)
tentativa=3/5 em=...T04:31:50Z   (reenfileirado em 04:31:45)
tentativa=4/5 em=...T04:32:01Z   (reenfileirado em 04:31:55)
DLQ tentativas=5/5 em 04:32:05
```
Comportamento correto: cada tentativa é reenfileirada pelo `RetryPoller` (que roda a cada `RETRY_POLL_INTERVAL_SECONDS=10s`) assim que `next_retry_at` vence, e a 5ª falha move para `dlq` automaticamente.

**Recuperação manual**: com o `echoserver` de volta, `POST /notifications/{id}/retry`:
```json
{"id":"...","status":"pending","attempts":0, ...}
```
Segundos depois, status `success`. O retry manual zera `attempts`, dando um ciclo novo completo — sem precisar recriar a notificação.

---

## 4. Worker morre no meio do processamento (mensagem presa na PEL)

**Ação**: `echoserver` configurado com 30s de latência artificial (`-latency 30s`), notificação criada, worker (`worker-A`) morto via `kill -9` equivalente (`Stop-Process -Force`) enquanto o `Send` ainda estava em andamento.

**Esperado**: a mensagem fica na *Pending Entries List* (PEL) do consumer group, "dona" de um consumidor que não existe mais; outro consumidor deveria reivindicá-la depois de `claimMinIdle` (30s) via `XAUTOCLAIM`.

**Evidência imediata após matar o worker**:
```bash
$ redis-cli XPENDING notifications:stream notification-workers
1
1789274066128-0
1789274066128-0
worker-A-0        # dono é um consumidor morto
1
```
A notificação continuava `pending` no Postgres — nem sucesso nem falha registrados, porque o processo morreu antes de conseguir persistir qualquer coisa.

**Resultado**: subiu um novo worker (`worker-B`, mesmo consumer group). Assim que o idle da mensagem passou de 30s, o loop de consumo do `worker-B` (que roda `XAUTOCLAIM` a cada iteração) reivindicou a mensagem sozinho:
```
[dispatcher] retry agendado id=c59b5ac1-... tentativa=1/5 ... context deadline exceeded
[retry-poller] reenfileirado id=c59b5ac1-... tentativa=1/5
```
A mensagem **não ficou perdida nem presa** — foi reivindicada e reprocessada automaticamente por um consumidor diferente, sem nenhuma ação manual. Isso já prova o que o teste se propôs a provar: nenhuma perda quando um worker morre no meio do processamento (garantia *at-least-once* de ARCHITECTURE.md).

O que aconteceu depois foi um efeito colateral do **timing do teste, não do sistema**: o `echoserver` continuou respondendo com os 30s de latência artificial por mais tempo do que o planejado (o tempo real gasto escrevendo esta documentação entre os comandos), então as tentativas seguintes continuaram batendo no timeout do http.Client (10s) e a notificação esgotou as 5 tentativas, indo para `dlq` — o mesmo comportamento correto do cenário 3. Reenfileirada manualmente com `POST /retry` depois que o `echoserver` rápido finalmente estava no ar, chegou em `success` normalmente. Nenhuma mensagem foi perdida em nenhum momento — só demorou mais para ser entregue do que o cenário original pretendia demonstrar.

**Nota sobre exactly-once**: como ARCHITECTURE.md observa, a garantia aqui é *at-least-once*, não *exactly-once*: se o worker tivesse morrido **depois** do `echoserver` responder 200 mas **antes** do ACK, a reentrega teria causado um envio duplicado ao canal externo — cenário que o `Idempotency-Key` não cobre, porque a duplicidade aconteceria depois da notificação já aceita, não numa nova requisição HTTP.

---

## 5. Duas (ou vinte) requisições concorrentes com a mesma `Idempotency-Key`

**Ação**: 20 requisições `POST /api/v1/notifications` disparadas **simultaneamente** (em paralelo, via `curl ... &` + `wait`) com o mesmo header `Idempotency-Key`.

**Esperado**: a constraint `UNIQUE` parcial no Postgres (`idx_notifications_idempotency_key`) deveria garantir uma única notificação, mesmo com 20 tentativas de INSERT concorrentes disputando a mesma chave.

**Resultado**:
```
20 respostas HTTP, todas 202 Accepted
20 IDs retornados → 1 único valor distinto
SELECT count(*) FROM notifications WHERE idempotency_key = '...' → 1
```
Nenhuma das 20 requisições recebeu erro — as que perderam a corrida do `INSERT` (violação da constraint `UNIQUE`, capturada como `domain.ErrDuplicateIdempotencyKey`) automaticamente buscaram e devolveram a notificação já criada pela vencedora. **Zero duplicatas, zero erros expostos ao cliente**, mesmo sob concorrência real (não apenas no teste unitário com fakes que já cobria esse caso).

(A única notificação criada por essa corrida também foi pega pelo mesmo atraso do `echoserver` do cenário 4 e passou por retry até `dlq` antes de ser reenfileirada manualmente com sucesso — o ponto testado aqui, unicidade sob concorrência, já estava validado antes disso acontecer.)

---

## Resumo

| Cenário | Comportamento observado |
|---|---|
| Redis cai | Fallback para retry via Postgres; recupera sozinho quando volta |
| Postgres cai | Falha explícita (500) sem crashar; erro sanitizado (corrigido nesta rodada); reconecta sozinho |
| Canal externo indisponível | Retry com backoff+jitter até DLQ; retry manual funciona |
| Worker morre em processamento | XAUTOCLAIM reivindica a mensagem; nenhuma perda (at-least-once) |
| Requisições concorrentes com mesma Idempotency-Key | Exatamente 1 notificação criada, sem erros |

Nenhum desses testes exigiu Transactional Outbox, Kafka ou qualquer peça de infraestrutura nova — todos os mecanismos que fizeram esses cenários se recuperarem sozinhos (RetryPoller, XAUTOCLAIM, constraint UNIQUE, backoff com jitter) já faziam parte do sistema antes desta rodada de testes. O que mudou foi a **prova empírica** de que funcionam, mais uma correção real que só um teste sob carga (não testes unitários) conseguiria revelar — veja [BENCHMARKS.md](BENCHMARKS.md#achado-pool-de-conexões-do-postgresql-sem-limite).
