package channel

import (
	"fmt"
	"net/http"
	"time"

	"notification-engine/internal/domain"
	"notification-engine/internal/security"
)

// Sender é reexportado aqui por conveniência para quem importa só o pacote channel.
type Sender = domain.Sender

// Registry mantém o mapeamento entre ChannelType e sua implementação (Sender)
// concreta. É usado pelo worker para rotear cada notificação ao conector correto.
type Registry struct {
	senders map[domain.ChannelType]Sender
}

// GmailConfig agrupa as credenciais SMTP necessárias para o GmailSender.
type GmailConfig struct {
	Host        string
	Port        string
	Username    string
	AppPassword string
	FromName    string
}

// NewRegistry monta o registry com todos os conectores de canal disponíveis.
// Discord e Webhook (os dois que fazem requisição HTTP a uma URL fornecida
// pelo cliente da API) usam um http.Client "hardened" contra SSRF — recusa
// conectar em IPs privados/loopback/link-local, a menos que
// allowPrivateNetworks esteja ligado (uso local/dev apenas). Os demais
// canais (Telegram, Gmail, WhatsApp, Outlook) sempre falam com um host fixo
// do próprio provedor, nunca com uma URL fornecida pelo cliente da API —
// não há superfície de SSRF neles, então usam um client comum.
func NewRegistry(
	httpTimeout time.Duration,
	telegramBotToken string,
	gmailCfg GmailConfig,
	whatsappCfg WhatsAppConfig,
	outlookCfg OutlookConfig,
	allowPrivateNetworks bool,
) *Registry {
	safeClient := security.SafeHTTPClient(httpTimeout, allowPrivateNetworks)
	plainClient := &http.Client{Timeout: httpTimeout}

	registry := NewEmptyRegistry()

	registry.Register(NewWebhookSender(safeClient))
	registry.Register(NewDiscordSender(safeClient))
	registry.Register(NewTelegramSender(plainClient, telegramBotToken))
	registry.Register(NewGmailSender(
		gmailCfg.Host,
		gmailCfg.Port,
		gmailCfg.Username,
		gmailCfg.AppPassword,
		gmailCfg.FromName,
	))
	registry.Register(NewWhatsAppSender(plainClient, whatsappCfg))
	registry.Register(NewOutlookSender(plainClient, outlookCfg))

	return registry
}

// NewEmptyRegistry cria um Registry sem nenhum conector — usado em testes
// para registrar apenas os senders (reais ou fake) que o cenário precisa.
func NewEmptyRegistry() *Registry {
	return &Registry{senders: make(map[domain.ChannelType]Sender)}
}

// Register associa um Sender ao canal que ele implementa (Sender.Channel()).
func (r *Registry) Register(s Sender) {
	r.senders[s.Channel()] = s
}

// Get retorna o Sender responsável por um determinado canal.
func (r *Registry) Get(c domain.ChannelType) (Sender, error) {
	sender, ok := r.senders[c]
	if !ok {
		return nil, fmt.Errorf("nenhum conector registrado para o canal %q", c)
	}
	return sender, nil
}
