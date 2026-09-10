# 🚀 Webhook & Notification Engine

> Engine assíncrona de alto desempenho construída em **Go** para envio de notificações multicanais (**Discord**, **Telegram**, **Gmail** e **Webhooks**) usando **Redis Streams**.

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev/)
[![Redis Streams](https://img.shields.io/badge/Redis-Streams-DC382D?style=flat-square&logo=redis&logoColor=white)](https://redis.io/)
[![License](https://img.shields.io/badge/License-MIT-green?style=flat-square)](LICENSE)

---

## ⚡ Visão Geral

O **Notification Engine** desacopla o recebimento de solicitações de notificação da entrega real aos provedores finais.

A API HTTP valida e enfileira a mensagem no **Redis Streams** em milissegundos (retornando `202 Accepted`), enquanto **Workers independentes** gerenciam o consumo paralelo, entrega e resiliência por meio de goroutines.

### 🎯 Principais Diferenciais
- **Desempenho não-bloqueante:** Resposta imediata para a aplicação cliente.
- **Concorrência Nativa:** Consumo em paralelo via Goroutines e Consumer Groups do Redis.
- **Arquitetura Modular:** Fácil adição de novos conectores de canal.
- **Resiliente:** Estrutura preparada para Retry e Dead Letter Queue (DLQ).

---

## 🏗️ Arquitetura

```text
[ Cliente / App ]
       │
       ▼ (POST /api/v1/notifications)
┌──────────────┐
│   API (Go)   │ ──► [202 Accepted + UUID]
└──────┬───────┘
       │
       ▼ (Publish Event)
┌──────────────────────────────────────┐
│     Redis Stream (Consumer Group)    │
└──────────────────┬───────────────────┘
                   │
                   ▼ (Worker Goroutines)
┌──────────────────────────────────────┐
│            Worker Engine             │
└──────┬───────────┬───────────┬───────┘
       │           │           │
       ▼           ▼           ▼
   [Discord]  [Telegram]    [Gmail]  [Webhook]
```
🛠️ Stack Tecnológica
Componente	Tecnologia	Função
Linguagem	Go 1.22+	API REST & Worker Engine
Message Broker	Redis Streams	Fila assíncrona persistente
Driver Redis	go-redis/v9	Conexão de alta performance com Redis
Ambiente	Docker Compose	Orquestração da infraestrutura local
📂 Estrutura do Projeto
Plaintext
.
├── main.go           # Aplicação consolidada (API + Worker Engine)
├── docker-compose.yml
└── .env.example
🚀 Como Executar
Pré-requisitos
Go 1.22+ instalado

Docker e Docker Compose

1. Iniciar Infraestrutura
Bash
docker compose up -d
2. Configurar Variáveis de Ambiente
Bash
cp .env.example .env
3. Executar a Aplicação (Tudo em um único processo)
Bash
go run main.go
A API estará rodando em http://localhost:8080.

📡 Endpoints da API
POST /api/v1/notifications
Payload - Discord
JSON
{
  "channel": "discord",
  "destination": "[https://discord.com/api/webhooks/SEU_WEBHOOK](https://discord.com/api/webhooks/SEU_WEBHOOK)",
  "message": "🚀 Mensagem de teste da Notification Engine!"
}
Payload - Telegram
JSON
{
  "channel": "telegram",
  "destination": "BOT_TOKEN|CHAT_ID",
  "message": "🤖 Notificação via Telegram!"
}
Payload - Gmail (SMTP)
JSON
{
  "channel": "email",
  "destination": "destino@exemplo.com",
  "message": "📧 Teste de e-mail assíncrono."
}
Resposta (202 Accepted)
JSON
{
  "id": "f81d4fae-7dec-11d0-a765-00a0c91e6bf6",
  "status": "pending",
  "message": "notificação aceita e enfileirada para processamento"
}
🗺️ Roadmap (Próximas Evoluções)
[ ] Lógica de Retry com Exponential Backoff

[ ] Dead Letter Queue (DLQ) no Redis

[ ] Métrica e monitoramento com Prometheus/Grafana

[ ] Dockerfiles Multi-stage para produção

👨‍💻 Autor
Feito por Luiz Santos

GitHub: @luixzsantos

LinkedIn: Luiz Santos

📄 Licença
Este projeto está sob a licença MIT.
'@ -Encoding utf8; git add README.md; git commit -m "docs: adiciona README consolidado"; git push -u origin main --force
