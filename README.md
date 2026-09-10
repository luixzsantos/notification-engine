# 🚀 Notification Engine & Webhooks

Uma plataforma assíncrona de alto desempenho desenvolvida em **Go** e **Redis Streams**, projetada para receber requisições de notificações de alta concorrência e entregá-las de forma confiável através de múltiplos canais.

---

## 📌 Por que este projeto foi construído?

Sistemas modernos precisam enviar confirmações, alertas e e-mails instantaneamente. Contudo, realizar chamadas HTTP diretas a provedores externos (como Discord, Telegram ou Gmail) durante a requisição do usuário introduz **latência extrema** e **pontos únicos de falha**.

A **Notification Engine** resolve esse problema desacoplando a recepção do envio:
- A API aceita a solicitação e responde ao cliente em **menos de 5ms**.
- O processamento pesado e a comunicação com as APIs externas acontecem em segundo plano via **Workers assíncronos**.

---

## ⚙️ Funcionamento da Arquitetura

O fluxo foi desenhado utilizando o padrão **Producer/Consumer** sob **Clean Architecture**:

```text
[ Cliente / App ]
       │
       │  1. Request (POST /api/v1/notifications)
       ▼
┌──────────────┐
│   API (Go)   │ ──► Resposta Imediata: [202 Accepted + ID do Evento]
└──────┬───────┘
       │
       │  2. Publica evento no Stream
       ▼
┌──────────────────────────────────────┐
│     Redis Stream (Consumer Group)    │
└──────────────────┬───────────────────┘
                   │
                   │  3. Consumo paralelo via Goroutines
                   ▼
┌──────────────────────────────────────┐
│            Worker Engine             │
└──────┬───────────┬───────────┬───────┘
       │           │           │
       ▼           ▼           ▼
   [Discord]  [Telegram]    [Gmail]  [Webhooks]
```
🛠️ Tecnologias Utilizadas
Linguagem: Go (1.22+) — Escolhida pela alta performance, baixo consumo de memória e concorrência nativa (Goroutines).

Mensageria: Redis Streams — Garantia de persistência, ordenação e suporte nativo a Consumer Groups.

Infraestrutura: Docker & Docker Compose — Para reprodução rápida do ambiente local.

🚀 Como Executar e Testar
Pré-requisitos
Docker e Docker Compose instalados.

Go 1.22 ou superior (opcional, caso rode fora do Docker).

1. Subir a Infraestrutura
Clone o repositório e inicie os containers do Redis:

Bash
git clone [https://github.com/luixzsantos/notification-engine.git](https://github.com/luixzsantos/notification-engine.git)
cd notification-engine
docker compose up -d
2. Configurar as Variáveis de Ambiente
Crie o arquivo .env com base no modelo de exemplo:

Bash
cp .env.example .env
3. Rodar a Aplicação
Bash
go run main.go
A API estará acessível em http://localhost:8080.

🧪 Demonstração Prática (Exemplos de Uso)
Você pode testar o envio de mensagens utilizando os comandos curl abaixo no seu terminal:

1. Notificação via Discord (Webhook)
Bash
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{
    "channel": "discord",
    "destination": "[https://discord.com/api/webhooks/SEU_WEBHOOK_AQUI](https://discord.com/api/webhooks/SEU_WEBHOOK_AQUI)",
    "message": "🚀 Teste de notificação assíncrona via Discord!"
  }'
2. Notificação via Telegram (Bot)
Bash
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{
    "channel": "telegram",
    "destination": "SEU_BOT_TOKEN|SEU_CHAT_ID",
    "message": "🤖 Mensagem enviada pelo Notification Engine!"
  }'
3. Notificação via E-mail (Gmail/SMTP)
Bash
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{
    "channel": "email",
    "destination": "seu-email@exemplo.com",
    "message": "📧 Teste de e-mail disparado pela fila do Redis."
  }'
Resposta Padrão da API (202 Accepted):
JSON
{
  "id": "a1b2c3d4-e5f6-7890-abcd-1234567890ab",
  "status": "pending",
  "message": "notificação aceita e enfileirada para processamento"
}
🗺️ Visão de Futuro (Roadmap)
[ ] Mecanismo de Reentrega (Retry Pattern): Reenvio automático com Exponential Backoff em caso de falha de rede.

[ ] Dead Letter Queue (DLQ): Armazenamento de mensagens que falharam definitivamente para análise posterior.

[ ] Observabilidade: Coleta de métricas em tempo real com Prometheus e dashboards no Grafana.

👨‍💻 Autor
Desenvolvido por Luiz Santos

GitHub: @luixzsantos

LinkedIn: Luiz Santos

📄 Licença
Este projeto está sob a licença MIT.


<ElicitationsGroup message="Precisa de algum ajuste específico para a sua apresentação?">
  <Elicitation label="Adicionar detalhes técnicos do Redis Streams" query="Adicione uma seção explicando detalhadamente como o Consumer Group do Redis Streams funciona neste projeto."/>
  <Elicitation label="Criar um roteiro para apresentação oral" query="Crie um roteiro resumido de fala para eu usar durante a apresentação deste projeto."/>
</ElicitationsGroup>

```
git add . — Inclui todas as suas modificações no envio.

git commit -m "..." — Salva as alterações com uma mensagem descritiva.

git push origin main — Sobe tudo diretamente para o repositório no GitHub.
```
