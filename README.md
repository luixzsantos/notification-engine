Para enviar a atualização do README.md e sincronizar seu projeto no GitHub de uma vez só, execute o bloco de comandos abaixo no terminal do PowerShell.Ele grava a nova versão do arquivo README.md, salva no Git e envia tudo para o seu repositório:PowerShellSet-Content -Path "README.md" -Value @'
# Webhook & Notification Engine

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.22+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go Version" />
  <img src="https://img.shields.io/badge/Redis-Streams-DC382D?style=for-the-badge&logo=redis&logoColor=white" alt="Redis" />
  <img src="https://img.shields.io/badge/Docker-Ready-2496ED?style=for-the-badge&logo=docker&logoColor=white" alt="Docker" />
  <img src="https://img.shields.io/badge/License-MIT-green.svg?style=for-the-badge" alt="License" />
</p>

Serviço assíncrono de alto desempenho para disparo de notificações multicanais (**Discord**, **Telegram**, **Gmail** e **Webhooks genéricos**), construído em **Go** seguindo os princípios de **Clean Architecture**, com concorrência nativa via goroutines e fila de processamento no **Redis Streams**.

---

## 📌 Índice

- [Arquitetura](#-arquitetura)
- [Stack Tecnológica](#-stack-tecnológica)
- [Estrutura de Pastas](#-estrutura-de-pastas)
- [Pré-requisitos](#-pré-requisitos)
- [Como Rodar o Projeto](#-como-rodar-o-projeto)
- [Variáveis de Ambiente](#-variáveis-de-ambiente)
- [Uso da API (Endpoints)](#-uso-da-api-endpoints)
- [Roadmap (V2)](#-roadmap-v2)
- [Autor](#-autor)
- [Licença](#-licença)

---

## 🏗️ Arquitetura

```text
Cliente → POST /api/v1/notifications → API (Go)
                                         │
                                         ▼
                                  Redis Stream (fila)
                                         │
                                         ▼
                                Worker (N goroutines consumidoras)
                                         │
                          ┌──────────────┼──────────────┬──────────────┐
                          ▼              ▼              ▼              ▼
                       Discord        Telegram        Gmail         Webhook
                       Webhook        Bot API        (SMTP)        genérico
Recepção: O cliente faz um POST para /api/v1/notifications.Enfileiramento de Alta Performance: A API valida o payload e publica o evento no Redis Stream, respondendo imediatamente com 202 Accepted e um id de rastreio em milissegundos — sem bloquear o cliente para aguardar a entrega real.Consumo Concorrente: O Worker roda de forma independente, utilizando múltiplas goroutines que consomem o mesmo consumer group do Redis, garantindo processamento paralelo e sem duplicidade de entrega.Conectores Modulares: Cada notificação é roteada ao conector do canal correspondente (discord, telegram, email, webhook), que dispara a requisição HTTP/SMTP.Tratamento de Falhas: Sucesso e falhas são logados; registros pendentes permanecem na Pending Entries List (PEL) do Redis, servindo de base para a futura estratégia de Retry e Dead Letter Queue (DLQ).💻 Stack TecnológicaGo 1.22+ — API REST e Worker desacoplados executados como binários independentes.Redis Streams — Fila de mensagens persistente e assíncrona com suporte a Consumer Groups.net/http — Biblioteca nativa do Go para o servidor HTTP, sem dependências pesadas de frameworks externos.Dependências externas: go-redis/v9 (driver do Redis), google/uuid (geração de IDs únicos) e joho/godotenv (leitura de variáveis locais).📁 Estrutura de PastasPlaintextnotification-engine/
├── cmd/
│   ├── api/main.go               # Entry point da API REST (Producer)
│   └── worker/main.go            # Entry point do Worker (Consumer)
├── internal/
│   ├── config/                   # Carregamento e validação das variáveis de ambiente
│   ├── domain/                   # Entidades, payloads e interfaces de domínio
│   ├── handler/                  # Controladores e handlers HTTP
│   ├── service/                  # Regras de negócio da aplicação
│   ├── queue/                    # Lógica do Producer e Consumer do Redis Streams
│   └── channel/                  # Conectores de entrega: Discord, Telegram, Gmail, Webhook
├── docker-compose.yml            # Infraestrutura do Redis para ambiente local
├── .env.example                  # Modelo de variáveis de ambiente
├── go.mod                        # Módulos do Go
└── README.md
⚙️ Pré-requisitosAntes de iniciar, certifique-se de ter instalado em sua máquina:Go 1.22 ou superiorDocker & Docker Compose🚀 Como Rodar o ProjetoClone o repositório:Bashgit clone [https://github.com/luixzsantos/Webhook-notification-eng.git](https://github.com/luixzsantos/Webhook-notification-eng.git)
cd Webhook-notification-eng
Inicie o contêiner do Redis:Bashdocker compose up -d
Configure o arquivo de variáveis de ambiente:Bashcp .env.example .env
Edite o arquivo .env para incluir suas credenciais (ex: token do Telegram ou Senha de App do Gmail).Sincronize as dependências do Go:Bashgo mod tidy
Inicie a API REST (Terminal 1):Bashgo run cmd/api/main.go
Inicie o Worker Consumidor (Terminal 2):Bashgo run cmd/worker/main.go
A API estará escutando na porta configurada (padrão: http://localhost:8080).🔐 Variáveis de AmbienteAs configurações do sistema são gerenciadas via variáveis de ambiente. Consulte o arquivo .env.example para referência:VariávelObrigatóriaDescriçãoAPI_PORTNãoPorta do servidor HTTP (default: 8080)REDIS_ADDRSimEndereço de conexão com o Redis (default: localhost:6379)REDIS_STREAM_NAMENãoNome da Stream no Redis (default: notifications:stream)TELEGRAM_BOT_TOKENApenas p/ TelegramToken de acesso do Bot retornado pelo @BotFatherGMAIL_USERNAMEApenas p/ GmailEndereço do e-mail remetente do GmailGMAIL_APP_PASSWORDApenas p/ GmailSenha de App de 16 dígitos gerada no Google (requer 2FA)WORKER_CONCURRENCYNãoNúmero de goroutines operando em paralelo no worker (default: 10)📡 Uso da API (Endpoints)1. Criar e Enfileirar NotificaçãoPOST /api/v1/notifications🔹 Exemplo: Webhook GenéricoBashcurl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{
    "channel": "webhook",
    "target": "[https://sua-api.com/webhooks/receber](https://sua-api.com/webhooks/receber)",
    "message": "Mensagem de teste via Webhook genérico!"
  }'
🔹 Exemplo: DiscordBashcurl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{
    "channel": "discord",
    "target": "[https://discord.com/api/webhooks/SEU_WEBHOOK_AQUI](https://discord.com/api/webhooks/SEU_WEBHOOK_AQUI)",
    "message": "🚀 Notificação enviada para o Discord via Go!"
  }'
🔹 Exemplo: TelegramBashcurl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{
    "channel": "telegram",
    "target": "<CHAT_ID>",
    "message": "🤖 Mensagem automática enviada via Telegram Bot!"
  }'
🔹 Exemplo: Gmail (SMTP)Bashcurl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{
    "channel": "email",
    "target": "destinatario@exemplo.com",
    "subject": "Aviso do Sistema",
    "message": "Sua solicitação foi processada com sucesso!"
  }'
✉️ Resposta de Sucesso (202 Accepted):JSON{
  "id": "441b54a5-3fd7-4657-9b22-b3513d5056a4",
  "status": "pending",
  "message": "notificação aceita e enfileirada para processamento"
}
2. HealthcheckGET /healthRetorna o status da API para monitoramento de disponibilidade.Bashcurl -X GET http://localhost:8080/health
🛣️ Roadmap (V2)[ ] Lógica de Retry com Exponential Backoff e Dead Letter Queue (DLQ) para mensagens com erro persistente.[ ] Persistência de auditoria e histórico de envios utilizando PostgreSQL ou SQLite.[ ] Implementação de Rate Limiting por canal e por destino.[ ] Construção de imagens Docker multi-stage e atualização do docker-compose.yml para rodar API e Worker em contêineres.👨‍💻 AutorDesenvolvido por Luiz Santos.GitHub: @luixzsantosLinkedIn: Luiz Santos📜 LicençaEste projeto está sob a licença MIT.'@ -Encoding utf8git add .git commit -m "docs: adicionado README.md com documentacao completa e autor"git push -u origin main --force
Ao terminar a execução desses comandos, basta atualizar a página do seu repositório no GitHub para ver a nova documentação publicada.
