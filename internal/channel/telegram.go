package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"notification-engine/internal/domain"
)

// TelegramSender dispara mensagens via Telegram Bot API (sendMessage).
// Docs: https://core.telegram.org/bots/api#sendmessage
type TelegramSender struct {
	client *http.Client
	token  string
}

func NewTelegramSender(client *http.Client, token string) *TelegramSender {
	return &TelegramSender{client: client, token: token}
}

func (s *TelegramSender) Channel() domain.ChannelType {
	return domain.ChannelTelegram
}

type telegramPayload struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
}

// Send envia a notificação para o Telegram. n.Target deve ser o chat_id do
// destinatário (obtido, por exemplo, falando com @userinfobot).
func (s *TelegramSender) Send(ctx context.Context, n *domain.Notification) error {
	if s.token == "" {
		return fmt.Errorf("telegram: TELEGRAM_BOT_TOKEN não configurado")
	}

	body, err := json.Marshal(telegramPayload{ChatID: n.Target, Text: n.Message})
	if err != nil {
		return fmt.Errorf("telegram: falha ao serializar payload: %w", err)
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", s.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("telegram: falha ao criar requisição: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: falha na requisição HTTP: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram: resposta com status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
