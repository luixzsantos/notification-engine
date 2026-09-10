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

O que faz o projeto
A Webhook & Notification Engine é um intermediário (middleware) de alta performance que recebe pedidos de envio de notificações por HTTP e os entrega nos canais finais de forma assíncrona.

Recepção em milissegundos: A API valida a requisição, gera um ID único (UUID), salva o evento no Redis Streams e responde imediatamente com o status 202 Accepted.

Consumo paralelo em background: Workers escritos em Go usam Goroutines e Consumer Groups do Redis para ler a fila simultaneamente e disparar os pacotes para Discord (Webhooks), Telegram (Bot API), Gmail (SMTP) ou Webhooks genéricos.

Isolamento de falhas: Se o Gmail ou a API do Telegram ficarem lentos ou fora do ar, a sua aplicação principal não trava nem perde desempenho.

Por que é útil (O problema real que resolve)
Em arquiteturas tradicionais, quando uma aplicação precisa enviar uma notificação (por exemplo, um e-mail de boas-vindas ou um alerta no Discord), a chamada é feita de forma síncrona:

Plaintext
[Cliente] ──► [Sua API] ──► (Aguarda 2-3s a API do Gmail/Discord responder) ──► [Cliente recebe resposta]
Isso gera problemas graves em produção:

Latência alta para o usuário: O usuário fica esperando a tela carregar enquanto sua aplicação espera o servidor de e-mail responder.

Cascata de falhas: Se o Discord/Telegram passar por instabilidade ou lentidão, as requisições da sua API começam a acumular, esgotam os recursos do servidor e derrubam o sistema inteiro.

Bloqueio de concorrência: Requisições I/O (rede) travam threads. Em momentos de pico (como uma promoção ou disparo em massa), o servidor entra em colapso.

Com a Notification Engine:
```
Plaintext
[Cliente] ──► [Sua API] ──► [Redis Stream] (Responde em < 5ms) ──► [Cliente livre]
                                  │
                                  └─► [Worker Go] ──► [Disparo em background]
```
Escalabilidade massiva: O Go gerencia milhares de Goroutines consumindo a fila com uso insignificante de memória e CPU.

Resiliência e Persistência: Como as notificações ficam salvas no Redis Streams, nenhuma mensagem é perdida se um worker reiniciar.

Desacoplamento: Para adicionar um novo canal (ex: SMS via Twilio ou Push Notification), basta criar um novo adapter dentro da engine sem alterar uma linha de código da aplicação principal.

```
git add . — Inclui todas as suas modificações no envio.

git commit -m "..." — Salva as alterações com uma mensagem descritiva.

git push origin main — Sobe tudo diretamente para o repositório no GitHub.
```

Abaixo estão as 5 perguntas técnicas mais prováveis que um entrevistador backend faria sobre a sua Notification Engine, junto com a explicação do que ele quer avaliar e a resposta ideal para você dominar a entrevista.1. "Por que você escolheu o Redis Streams em vez de ferramentas como RabbitMQ ou Apache Kafka?"O que o entrevistador quer saber: Se você entende os trade-offs de arquitetura e não usou a ferramenta apenas por "modismo".Como responder:"O Redis Streams foi a escolha ideal para o escopo e orçamento deste projeto porque entrega o recurso de Consumer Groups e persistência de mensagens mantendo um footprint de memória extremamente baixo. O RabbitMQ exige uma infraestrutura mais pesada e gerenciamento de trocas de mensagens (exchanges), enquanto o Kafka é voltado para volumetria massiva de streaming de dados em grande escala. Como o projeto já podia utilizar o Redis para cache ou estado temporário, o Redis Streams trouxe o padrão Publish/Subscribe garantido com menor complexidade operacional e latência na casa dos sub-milissegundos."2. "Como seu sistema lida com falhas no envio de notificações (ex: a API do Discord ficou fora do ar ou deu Rate Limit)?"O que o entrevistador quer saber: Se você pensa em resiliência, tolerância a falhas e consistência eventual.Como responder:"Atualmente, o sistema captura a falha e registra o erro logado sem derrubar a goroutine do worker. Como evolução arquitetural mapped no roadmap, a estratégia ideal é implementar o Retry Pattern com Exponential Backoff utilizando o mecanismo de mensagens pendentes (PENDING List/XPENDING) do próprio Redis Streams. Se após $N$ tentativas o envio continuar falhando (como em um erro 4xx definitivo de validação), a mensagem é movida para uma Dead Letter Queue (DLQ) para auditoria manual sem reprocessamento infinito."3. "Como você garante que uma mesma notificação não seja processada mais de uma vez pelos Workers?"O que o entrevistador quer saber: Se você compreende concorrência, idempotência e entrega de mensagens em sistemas distribuídos (At-Least-Once vs Exactly-Once).Como responder:"Garantimos o isolamento no consumo utilizando Consumer Groups do Redis Streams. Quando um worker lê uma mensagem da stream (XREADGROUP), o Redis marca aquela mensagem especificamente para aquele worker, evitando que outros trabalhadores no mesmo grupo a leiam simultaneamente. Para garantir idempotência total no lado do provedor, as notificações carregam um UUID único gerado na borda (API HTTP), garantindo que no caso de um reenvio o destinatário identifique que se trata do mesmo evento."4. "Como as Goroutines gerenciam o consumo e o encerramento gracioso (Graceful Shutdown) sem perder dados em processamento?"O que o entrevistador quer saber: Seu nível de domínio sobre concorrência nativa em Go e gerenciamento do ciclo de vida da aplicação.Como responder:"As goroutines operam em um loop contínuo de consumo bloqueante leve do Redis Streams. Para evitar condições de corrida (race conditions) e término abrupto em um SIGTERM (fechamento da aplicação), implementamos Graceful Shutdown utilizando context.WithCancel e sync.WaitGroup. Quando o processo é encerrado, o sinal é interceptado, o consumo de novas mensagens é interrompido e a aplicação aguarda o WaitGroup confirmar que as goroutines ativas finalizaram as chamadas HTTP pendentes antes de fechar a conexão com o Redis e encerrar o programa."5. "Como a Clean Architecture ajudou você a estruturar este projeto?"O que o entrevistador quer saber: Se você escreve código sustentável, testável e desacoplado.Como responder:"A Clean Architecture desacoplou completamente as regras de negócio das tecnologias externas. A camada internal/domain define os contratos e interfaces das notificações. Os conectores (Discord, Telegram, SMTP) atuam como Adapters na camada de infraestrutura. Isso significa que, se amanhã eu quiser trocar a API HTTP pelo protocolo gRPC, ou trocar o Redis Streams pelo NATS JetStream, ou adicionar um envio via SMS (Twilio), eu não preciso alterar nenhuma linha da regra de negócio ou do orquestrador de envio — basta criar um novo Adapter que implemente a interface do domínio."

