package channel

import (
	"fmt"
	"net/http"
	"time"

	"notification-engine/internal/domain"
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

// NewRegistry monta o registry com todos os conectores disponíveis na V1.
func NewRegistry(httpTimeout time.Duration, telegramBotToken string, gmailCfg GmailConfig) *Registry {
	client := &http.Client{Timeout: httpTimeout}

	registry := &Registry{
		senders: make(map[domain.ChannelType]Sender),
	}

	registry.register(NewWebhookSender(client))
	registry.register(NewDiscordSender(client))
	registry.register(NewTelegramSender(client, telegramBotToken))
	registry.register(NewGmailSender(
		gmailCfg.Host,
		gmailCfg.Port,
		gmailCfg.Username,
		gmailCfg.AppPassword,
		gmailCfg.FromName,
	))

	return registry
}

func (r *Registry) register(s Sender) {
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
