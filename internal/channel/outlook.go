package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"notification-engine/internal/domain"
)

// microsoftLoginBase e microsoftGraphBase são hosts fixos da Microsoft —
// nunca vêm do usuário, sem superfície de SSRF (diferente de webhook/discord).
const (
	microsoftLoginBase = "https://login.microsoftonline.com"
	microsoftGraphBase = "https://graph.microsoft.com/v1.0"
)

// tokenRefreshMargin antecipa a renovação do token de acesso em relação ao
// seu vencimento real, evitando usar (ou tentar usar) um token que expira
// no meio de uma requisição em andamento.
const tokenRefreshMargin = 60 * time.Second

// OutlookConfig agrupa as credenciais do Azure AD (aplicativo registrado com
// permissão de aplicativo "Mail.Send", consentida por um admin do tenant) e
// a caixa de e-mail usada como remetente.
type OutlookConfig struct {
	TenantID     string
	ClientID     string
	ClientSecret string
	SenderEmail  string
}

// OutlookSender envia e-mails via Microsoft Graph API (fluxo OAuth2
// client credentials — autenticação de aplicativo, sem usuário interativo).
// Docs: https://learn.microsoft.com/graph/api/user-sendmail
type OutlookSender struct {
	client       *http.Client
	loginBase    string
	graphBase    string
	tenantID     string
	clientID     string
	clientSecret string
	senderEmail  string

	mu          sync.Mutex
	cachedToken string
	tokenExpiry time.Time
}

func NewOutlookSender(client *http.Client, cfg OutlookConfig) *OutlookSender {
	return &OutlookSender{
		client:       client,
		loginBase:    microsoftLoginBase,
		graphBase:    microsoftGraphBase,
		tenantID:     cfg.TenantID,
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		senderEmail:  cfg.SenderEmail,
	}
}

func (s *OutlookSender) Channel() domain.ChannelType {
	return domain.ChannelOutlook
}

func (s *OutlookSender) Send(ctx context.Context, n *domain.Notification) error {
	token, err := s.getToken(ctx)
	if err != nil {
		return fmt.Errorf("outlook: falha ao obter token: %w", err)
	}

	payload := map[string]any{
		"message": map[string]any{
			"subject": n.Subject,
			"body": map[string]any{
				"contentType": "Text",
				"content":     n.Message,
			},
			"toRecipients": []map[string]any{
				{"emailAddress": map[string]any{"address": n.Target}},
			},
			"attachments": buildOutlookAttachments(n.Attachments),
		},
		"saveToSentItems": false,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("outlook: falha ao serializar payload: %w", err)
	}

	sendURL := fmt.Sprintf("%s/users/%s/sendMail", s.graphBase, url.PathEscape(s.senderEmail))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sendURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("outlook: falha ao criar requisição: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("outlook: falha na requisição HTTP: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("outlook: resposta com status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// buildOutlookAttachments converte os anexos (já em base64) para o formato
// fileAttachment do Graph API, que também espera base64 — repasse direto.
func buildOutlookAttachments(attachments []domain.Attachment) []map[string]any {
	if len(attachments) == 0 {
		return nil
	}

	result := make([]map[string]any, len(attachments))
	for i, att := range attachments {
		result[i] = map[string]any{
			"@odata.type":  "#microsoft.graph.fileAttachment",
			"name":         att.Filename,
			"contentType":  att.ContentType,
			"contentBytes": att.Data,
		}
	}
	return result
}

// getToken retorna um token de acesso válido, obtendo um novo via OAuth2
// client credentials quando o cache está vazio ou perto de expirar. Chamadas
// concorrentes disputam o mesmo mutex — só uma de fato busca um token novo,
// as demais reaproveitam o que acabou de ser cacheado.
func (s *OutlookSender) getToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cachedToken != "" && time.Now().Before(s.tokenExpiry) {
		return s.cachedToken, nil
	}

	form := url.Values{}
	form.Set("client_id", s.clientID)
	form.Set("client_secret", s.clientSecret)
	form.Set("scope", "https://graph.microsoft.com/.default")
	form.Set("grant_type", "client_credentials")

	tokenURL := fmt.Sprintf("%s/%s/oauth2/v2.0/token", s.loginBase, s.tenantID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("resposta com status %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("resposta de token inválida: %w", err)
	}
	if parsed.AccessToken == "" {
		return "", fmt.Errorf("resposta de token sem access_token: %s", string(respBody))
	}

	s.cachedToken = parsed.AccessToken
	s.tokenExpiry = time.Now().Add(time.Duration(parsed.ExpiresIn)*time.Second - tokenRefreshMargin)

	return s.cachedToken, nil
}
