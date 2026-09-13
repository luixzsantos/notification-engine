// Package bot implementa um bot de administração via Telegram (comandos
// /status e /retry), usando long polling puro sobre a API HTTP do
// Telegram — sem SDK externo, no mesmo estilo dos conectores em
// internal/channel.
package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"notification-engine/internal/service"
)

const telegramAPIBase = "https://api.telegram.org/bot"

// TelegramBot escuta comandos administrativos e responde consultando o
// mesmo NotificationService usado pela API HTTP.
type TelegramBot struct {
	token       string
	service     *service.NotificationService
	client      *http.Client
	offset      int
	allowedChat map[int64]bool // vazio/nil = qualquer chat pode usar o bot
}

// New cria o bot. allowedChatIDs restringe quem pode executar comandos
// administrativos (/retry, /status); uma lista vazia desativa a restrição
// (qualquer um que descubra o bot pode usá-lo — recomendado configurar em
// produção).
func New(token string, svc *service.NotificationService, allowedChatIDs []int64) *TelegramBot {
	allowed := make(map[int64]bool, len(allowedChatIDs))
	for _, id := range allowedChatIDs {
		allowed[id] = true
	}

	return &TelegramBot{
		token:       token,
		service:     svc,
		client:      &http.Client{Timeout: 35 * time.Second},
		allowedChat: allowed,
	}
}

// isAuthorized confere se o chat pode executar comandos administrativos.
func (b *TelegramBot) isAuthorized(chatID int64) bool {
	if len(b.allowedChat) == 0 {
		return true
	}
	return b.allowedChat[chatID]
}

// Run entra em loop de long polling (getUpdates) até o contexto ser
// cancelado.
func (b *TelegramBot) Run(ctx context.Context) {
	log.Println("[bot] telegram bot iniciado (long polling)")

	for {
		if ctx.Err() != nil {
			return
		}

		updates, err := b.getUpdates(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("[bot] erro ao buscar updates: %v", err)
			time.Sleep(3 * time.Second)
			continue
		}

		for _, u := range updates {
			b.offset = u.UpdateID + 1
			b.handleMessage(ctx, u.Message)
		}
	}
}

type update struct {
	UpdateID int     `json:"update_id"`
	Message  message `json:"message"`
}

type message struct {
	Chat struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	Text string `json:"text"`
}

type updatesResponse struct {
	OK     bool     `json:"ok"`
	Result []update `json:"result"`
}

func (b *TelegramBot) getUpdates(ctx context.Context) ([]update, error) {
	params := url.Values{}
	params.Set("timeout", "30")
	params.Set("offset", strconv.Itoa(b.offset))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s%s/getUpdates?%s", telegramAPIBase, b.token, params.Encode()), nil)
	if err != nil {
		return nil, err
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var parsed updatesResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("resposta inválida do telegram: %w", err)
	}
	if !parsed.OK {
		return nil, fmt.Errorf("telegram retornou ok=false")
	}

	return parsed.Result, nil
}

func (b *TelegramBot) handleMessage(ctx context.Context, msg message) {
	fields := strings.Fields(msg.Text)
	if len(fields) == 0 {
		return
	}

	switch fields[0] {
	case "/status", "/retry":
		if !b.isAuthorized(msg.Chat.ID) {
			log.Printf("[bot] comando %s negado para chat_id não autorizado: %d", fields[0], msg.Chat.ID)
			b.reply(ctx, msg.Chat.ID, "Você não está autorizado a usar este comando.")
			return
		}
		if fields[0] == "/status" {
			b.handleStatus(ctx, msg.Chat.ID, fields)
		} else {
			b.handleRetry(ctx, msg.Chat.ID, fields)
		}
	case "/start", "/help":
		b.reply(ctx, msg.Chat.ID, "Comandos disponíveis:\n/status <id> — consulta o estado de uma notificação\n/retry <id> — reenfileira uma notificação em retry ou DLQ")
	}
}

func (b *TelegramBot) handleStatus(ctx context.Context, chatID int64, fields []string) {
	if len(fields) < 2 {
		b.reply(ctx, chatID, "uso: /status <id>")
		return
	}

	n, err := b.service.GetByID(ctx, fields[1])
	if err != nil {
		b.reply(ctx, chatID, fmt.Sprintf("notificação não encontrada: %s", fields[1]))
		return
	}

	text := fmt.Sprintf("id: %s\ncanal: %s\nstatus: %s\ntentativas: %d/%d",
		n.ID, n.Channel, n.Status, n.Attempts, n.MaxAttempts)
	if n.LastError != "" {
		text += fmt.Sprintf("\núltimo erro: %s", n.LastError)
	}

	b.reply(ctx, chatID, text)
}

func (b *TelegramBot) handleRetry(ctx context.Context, chatID int64, fields []string) {
	if len(fields) < 2 {
		b.reply(ctx, chatID, "uso: /retry <id>")
		return
	}

	n, err := b.service.Retry(ctx, fields[1])
	if err != nil {
		b.reply(ctx, chatID, fmt.Sprintf("falha ao reenfileirar: %v", err))
		return
	}

	b.reply(ctx, chatID, fmt.Sprintf("notificação %s reenfileirada (canal: %s)", n.ID, n.Channel))
}

func (b *TelegramBot) reply(ctx context.Context, chatID int64, text string) {
	params := url.Values{}
	params.Set("chat_id", strconv.FormatInt(chatID, 10))
	params.Set("text", text)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s%s/sendMessage", telegramAPIBase, b.token), strings.NewReader(params.Encode()))
	if err != nil {
		log.Printf("[bot] falha ao montar resposta: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := b.client.Do(req)
	if err != nil {
		log.Printf("[bot] falha ao enviar resposta: %v", err)
		return
	}
	resp.Body.Close()
}
