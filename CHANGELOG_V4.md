# Changelog - V4.0.0

## 🎯 Overview

A **v4.0.0** adiciona dois canais de negócio pedidos diretamente para o caso
de uso de uma empresa que precisa alertar/avisar pessoas por vários meios ao
mesmo tempo: **Outlook / Microsoft 365** (a maioria das empresas usa
Microsoft, não Gmail) e **WhatsApp** (o canal de mensagens mais usado no
Brasil). Os dois seguem exatamente o mesmo padrão dos conectores já
existentes — chamada HTTP direta ao provedor, sem SDK — e reaproveitam toda
a infraestrutura de retry, DLQ, rate limiting e métricas que já existia.

- ✅ **Canal `outlook`**: envio via Microsoft Graph API (`POST
  /users/{email}/sendMail`), autenticação OAuth2 *client credentials* com
  cache de token
- ✅ **Canal `whatsapp`**: envio via WhatsApp Cloud API (Meta), com suporte
  a texto livre (dentro de uma janela de conversa ativa) e a Message
  Templates pré-aprovados (obrigatório para mensagens de negócio)
- ✅ Ambos sem superfície de SSRF (target não é uma URL — é e-mail/telefone,
  a requisição sempre vai para um host fixo do provedor)
- ✅ Rate limiting e default target por canal, no mesmo padrão dos demais
- ✅ `main.html` atualizado com os dois canais no formulário, incluindo os
  campos de template do WhatsApp
- ✅ Testes unitários com `httptest` para os dois senders (payload,
  cache/renovação de token, propagação de erro da API)

---

## 📝 Mudanças detalhadas

### 1. Canal Outlook / Microsoft 365 (`internal/channel/outlook.go`)

- `OutlookSender` autentica via OAuth2 *client credentials* contra
  `login.microsoftonline.com` e envia via Microsoft Graph API
  (`graph.microsoft.com/v1.0/users/{sender}/sendMail`).
- Requer um app registrado no Azure AD com permissão de **aplicativo**
  `Mail.Send` consentida por um admin do tenant (não é OAuth delegado —
  não há usuário interativo).
- **Cache de token**: o token de acesso (válido ~1h) é cacheado em memória e
  renovado com 60s de antecedência do vencimento (`tokenRefreshMargin`),
  evitando autenticar a cada envio. Chamadas concorrentes disputam o mesmo
  mutex — só uma busca um token novo, as demais reaproveitam.
- Suporta anexos (reaproveita `domain.Attachment`, já em base64 — o formato
  `fileAttachment` do Graph API espera exatamente isso).
- Novas variáveis: `OUTLOOK_TENANT_ID`, `OUTLOOK_CLIENT_ID`,
  `OUTLOOK_CLIENT_SECRET`, `OUTLOOK_SENDER_EMAIL`, `DEFAULT_OUTLOOK_TARGET`,
  `RATE_LIMIT_OUTLOOK_RPS`.

### 2. Canal WhatsApp (`internal/channel/whatsapp.go`)

- `WhatsAppSender` envia via WhatsApp Cloud API
  (`graph.facebook.com/v21.0/{phone-number-id}/messages`), autenticado com
  um token de acesso permanente (Bearer).
- **Dois modos de envio**:
  - **Texto livre** (`message`): só é aceito pela API dentro de uma janela
    de conversa ativa (últimas 24h, quando o destinatário mandou mensagem
    recentemente).
  - **Template** (`template_name` + `template_params`): obrigatório para
    mensagens que a empresa inicia (alertas, avisos) — o caminho correto
    para o caso de uso de notificação/alerta. O template precisa estar
    pré-aprovado no WhatsApp Manager.
- `domain.Notification` ganhou os campos `TemplateName`, `TemplateLocale`
  (default `pt_BR`) e `TemplateParams` (preenchem as variáveis posicionais
  `{{1}}`, `{{2}}`... do template, na ordem) — persistidos no Postgres
  (colunas `template_name`, `template_locale`, `template_params`) para que
  um retry automático ou manual continue usando o mesmo template.
- `domain.Notification.Validate()` passou a aceitar `TemplateName` como
  alternativa a `Message` (uma notificação de template não precisa de
  `message`).
- Números de telefone são normalizados (`normalizePhoneNumber`): aceita
  formatação humana (`+55 11 99999-9999`, `(11) 99999-9999`...) e converte
  para dígitos puros, como a API espera.
- Novas variáveis: `WHATSAPP_PHONE_NUMBER_ID`, `WHATSAPP_ACCESS_TOKEN`,
  `DEFAULT_WHATSAPP_TARGET`, `RATE_LIMIT_WHATSAPP_RPS`.

### 3. Sem superfície de SSRF nos dois canais novos

Diferente de webhook/discord, `target` em outlook/whatsapp nunca é uma URL
fornecida pelo cliente — é um e-mail ou telefone, e a requisição HTTP vai
sempre para um host fixo do provedor (`graph.microsoft.com`,
`graph.facebook.com`). Por isso os dois usam um `http.Client` comum (sem o
`security.SafeHTTPClient` hardened que webhook/discord exigem) — não há
necessidade nem o que proteger. Detalhes em
[ARCHITECTURE.md](ARCHITECTURE.md#canais-outlook-e-whatsapp-por-que-não-têm-superfície-de-ssrf).

### 4. `main.html`

- Novas opções `Outlook (Microsoft 365)` e `WhatsApp` no seletor de canal.
- Campo **Assunto** agora também aparece para Outlook (igual ao Gmail).
- Novos campos **Nome do template** e **Parâmetros do template** (visíveis
  só para WhatsApp), com texto explicando a exigência de template fora de
  uma janela de conversa ativa.
- Filtro de canal do Dashboard atualizado com as duas novas opções.

### 5. Testes

`internal/channel/outlook_test.go` e `internal/channel/whatsapp_test.go`
cobrem, contra servidores `httptest` (não contra as APIs reais — sem
credenciais de Meta/Microsoft neste ambiente): construção do payload
(inclusive template com variáveis), normalização de telefone, cache e
renovação de token OAuth2, e propagação de erro da API de origem.

**Validado manualmente** contra os endpoints reais (`graph.facebook.com` e
`login.microsoftonline.com`) sem credenciais válidas: as requisições
chegam corretamente formatadas e os provedores respondem com erros de
autenticação/permissão reais (não erros de rede ou de formato) — prova de
que a integração está correta, faltando só credenciais de uma conta real
para o envio de ponta a ponta.

---

## 📜 Licença

MIT
