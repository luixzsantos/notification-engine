# Changelog - V5.0.0

## 🎯 Overview

A **v5.0.0** adiciona o **Microsoft Teams** como canal e uma forma
genérica de anexar **tabelas estruturadas** a qualquer notificação — os
dois pedidos diretamente para o caso de uso de uma empresa mandando
resumos/alertas com dados tabulares (ex: lista de aprovações, status de
tarefas) para vários canais ao mesmo tempo. De quebra, uma correção real
de robustez: a validação de host específica de canal (Discord, e agora
Teams) passou a rodar também na criação da notificação, não só no envio.

- ✅ **Canal `teams`**: envio via Workflow do Power Automate (substituto
  oficial dos Incoming Webhooks do Teams, descontinuados pela Microsoft),
  com o corpo em Adaptive Card
- ✅ **`domain.Table`**: tabela genérica (`headers` + `rows`) anexável a
  qualquer notificação, renderizada de forma diferente por canal (tabela
  nativa no Teams e HTML no e-mail; texto monoespaçado nos demais)
- ✅ **Correção**: `ValidateDiscordHost`/`ValidateTeamsHost` agora também
  rodam na criação (antes só rodavam no envio) — um host errado é
  rejeitado com 400 na hora, em vez de gastar 5 tentativas de retry até
  cair em DLQ
- ✅ **Sintaxe embutida na mensagem** (estilo Notion): `/h1`/`/h2`/`/h3`
  viram título, um bloco `/table` vira `domain.Table` — sem precisar de
  campo separado; `main.html` ficou com um único campo de mensagem em vez
  de "Mensagem" + "Tabela"
- ✅ Testes unitários para o novo canal, para `domain.Table` e para o
  parsing da sintaxe embutida
- ✅ Validado manualmente contra o endpoint real do Azure Logic Apps (sem
  um Workflow configurado): a requisição chega corretamente formatada e o
  Azure responde com um erro real (`MissingApiVersionParameter`), não um
  erro de rede — confirma que a integração está correta

---

## 📝 Mudanças detalhadas

### 1. Canal Microsoft Teams (`internal/channel/teams.go`)

A Microsoft **descontinuou os Incoming Webhooks clássicos** do Teams. O
substituto oficial é um **Workflow do Power Automate** disparado por
"When a Teams webhook request is received" — `target` é a URL desse
gatilho (sempre termina em `.logic.azure.com`).

- `TeamsSender` monta um envelope `{"type":"message","attachments":[...]}`
  com um **Adaptive Card** dentro — o formato que a ação "Post card in a
  chat or channel" do Power Automate espera.
- `ValidateTeamsHost` restringe `target` a hosts `*.logic.azure.com`
  (mesmo padrão do `ValidateDiscordHost` já existente): não é proteção
  contra SSRF (já coberta pelo `SafeHTTPClient` genérico), é para impedir
  que o canal seja usado como proxy HTTP para qualquer servidor público.
- Passo a passo completo de configuração do Workflow, incluindo o JSON de
  exemplo a usar ao configurar o gatilho, no README
  ([Configurando o Microsoft Teams](README.md#configurando-o-microsoft-teams)).
- Nova variável: `DEFAULT_TEAMS_TARGET`, `RATE_LIMIT_TEAMS_RPS`. Sem
  variável de credencial própria — `target` já é a URL completa.

### 2. Tabelas estruturadas (`domain.Table`)

- Novo campo `Table *Table` em `Notification` (`{Headers []string, Rows
  [][]string}`), persistido no Postgres (coluna `table_data`, `JSONB`
  nullable — diferente de `payload`/`attachments`, que sempre têm um valor
  "vazio" natural, aqui a ausência de tabela precisa ser distinguível de
  uma tabela vazia).
- `Table.FormatMonospace()`: renderiza como texto de largura fixa (colunas
  alinhadas), usado por Discord/Telegram/WhatsApp (texto livre) dentro de
  um bloco de código.
- `Table.FormatHTML()`: renderiza como `<table>` HTML (escapada), usado
  por Gmail/Outlook — muda o `Content-Type` do e-mail para HTML só quando
  há tabela, preservando texto simples no caso comum.
- Teams renderiza a tabela como elemento `Table` **nativo** do Adaptive
  Card 1.5 (a única renderização "de verdade", não uma aproximação em
  texto).
- Webhook genérico inclui a tabela como mais um campo (`table`) no JSON.
- `domain.Notification.Validate()` passou a aceitar `Table` como
  alternativa a `Message` (uma tabela sem texto livre é conteúdo válido).

### 3. Correção: validação de host específica de canal agora roda na criação

Ao implementar o Teams, percebi que `ValidateTeamsHost` só era chamada
dentro do `Sender.Send()` — ou seja, só no worker, no momento do envio.
Isso significa que `POST /notifications` com `channel=teams` e um
`target` de qualquer domínio público (não `*.logic.azure.com`) era
**aceito com 202**, e só falhava depois de **5 tentativas de retry
inúteis** no worker, antes de cair em DLQ. O mesmo problema já existia
(sem eu ter notado antes) para `ValidateDiscordHost`.

Corrigido: `service.CreateNotification` agora chama
`channel.ValidateDiscordHost`/`channel.ValidateTeamsHost` também na
criação (além da checagem genérica de SSRF), rejeitando com 400
imediatamente. A checagem dentro do `Sender` continua existindo, como
defesa em profundidade.

### 4. `main.html`

- Nova opção `Microsoft Teams` no seletor de canal.
- Novo campo **Tabela** (textarea CSV simples: uma linha por linha de
  tabela, células separadas por vírgula, checkbox "primeira linha é
  cabeçalho") — disponível para qualquer canal, com uma nota explicando
  que Teams/e-mail mostram tabela de verdade e os demais recebem texto
  alinhado.
- Filtro de canal do Dashboard atualizado com Teams.

### 5. Testes

`internal/channel/teams_test.go` cobre validação de host, construção do
Adaptive Card (com e sem tabela) e o envio contra um servidor `httptest`.
`internal/domain/notification_test.go` ganhou testes para
`Table.FormatMonospace`/`FormatHTML` (incluindo linhas de tamanhos
desiguais e escaping de HTML) e para a validação aceitar `Table` sem
`Message`. `internal/service/notification_service_test.go` cobre a
correção do item 3 (rejeição na criação para Discord e Teams).

---

### 6. Título e tabela embutidos na mensagem (sintaxe estilo Notion)

O campo `table` estruturado (item 2 acima) continua existindo — é o
contrato certo para quem integra programaticamente — mas passou a ser
opcional na prática: `domain.ParseRichMessage` interpreta uma sintaxe leve
diretamente no texto de `Message`, então quem escreve a notificação (o
formulário em `main.html`, ou qualquer cliente) não precisa mais de um
campo separado.

- `/h1 texto`, `/h2 texto`, `/h3 texto` numa linha viram título (o marcador
  some, o texto é normalizado pro Markdown canônico `#`/`##`/`###`); uma
  mensagem sem nenhum marcador continua idêntica a antes.
- Um bloco `/table` (uma linha por linha, células separadas por vírgula,
  terminando na primeira linha em branco ou no fim da mensagem) vira um
  `domain.Table`, exatamente como o campo `table` explícito — se ambos
  estiverem presentes, o campo explícito prevalece.
- O parsing roda uma única vez, na criação da notificação
  (`service.NotificationService.build`); o que é persistido e enfileirado
  já é a forma final.
- Título ganhou renderização própria por canal: nativa no Discord (que já
  entende `#`/`##`/`###`), negrito no Telegram/WhatsApp, `<h1>`/`<h2>`/
  `<h3>` reais no e-mail, e um `TextBlock` dedicado (com tamanho/negrito
  proporcional ao nível) por título no Adaptive Card do Teams.
- `main.html`: o campo "Tabela" separado foi removido — um único campo de
  mensagem, com uma dica explicando a sintaxe.

## 📜 Licença

MIT
