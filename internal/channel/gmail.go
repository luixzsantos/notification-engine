package channel

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/smtp"
	"strings"

	"notification-engine/internal/domain"
)

// GmailSender envia notificações por e-mail usando o servidor SMTP do
// Gmail. Requer uma "Senha de app" gerada na conta Google (não a senha
// normal da conta) — veja: https://myaccount.google.com/apppasswords
type GmailSender struct {
	host        string
	port        string
	username    string // endereço de e-mail completo, ex: bot@gmail.com
	appPassword string
	fromName    string // nome de exibição do remetente (opcional)
}

func NewGmailSender(host, port, username, appPassword, fromName string) *GmailSender {
	return &GmailSender{
		host:        host,
		port:        port,
		username:    username,
		appPassword: appPassword,
		fromName:    fromName,
	}
}

func (s *GmailSender) Channel() domain.ChannelType {
	return domain.ChannelEmail
}

// Send envia a notificação por e-mail. n.Target deve ser o endereço de
// e-mail do destinatário. n.Subject é opcional (usa um default se vazio).
// Anexos (n.Attachments), quando presentes, são enviados como
// multipart/mixed em base64.
func (s *GmailSender) Send(ctx context.Context, n *domain.Notification) error {
	if s.username == "" || s.appPassword == "" {
		return fmt.Errorf("gmail: GMAIL_USERNAME/GMAIL_APP_PASSWORD não configurados")
	}
	if n.Target == "" {
		return fmt.Errorf("gmail: 'target' (e-mail do destinatário) é obrigatório")
	}

	msg, err := s.buildMessage(n)
	if err != nil {
		return err
	}

	addr := fmt.Sprintf("%s:%s", s.host, s.port)
	auth := smtp.PlainAuth("", s.username, s.appPassword, s.host)

	// net/smtp.SendMail já negocia STARTTLS automaticamente com o Gmail
	// na porta 587, então não é necessário gerenciar TLS manualmente.
	if err := smtp.SendMail(addr, auth, s.username, []string{n.Target}, msg); err != nil {
		return fmt.Errorf("gmail: falha ao enviar e-mail via SMTP: %w", err)
	}

	return nil
}

func (s *GmailSender) buildMessage(n *domain.Notification) ([]byte, error) {
	subject := n.Subject
	if subject == "" {
		subject = "Nova notificação"
	}

	fromHeader := s.username
	if s.fromName != "" {
		fromHeader = fmt.Sprintf("%s <%s>", s.fromName, s.username)
	}

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("From: %s\r\n", fromHeader))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", n.Target))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")

	if len(n.Attachments) == 0 {
		msg.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n\r\n")
		msg.WriteString(n.Message)
		return []byte(msg.String()), nil
	}

	const boundary = "notification-engine-boundary"
	msg.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=%q\r\n\r\n", boundary))

	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n\r\n")
	msg.WriteString(n.Message)
	msg.WriteString("\r\n")

	for _, att := range n.Attachments {
		raw, err := base64.StdEncoding.DecodeString(att.Data)
		if err != nil {
			return nil, fmt.Errorf("gmail: anexo %q com base64 inválido: %w", att.Filename, err)
		}

		contentType := att.ContentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		msg.WriteString(fmt.Sprintf("Content-Type: %s; name=%q\r\n", contentType, att.Filename))
		msg.WriteString("Content-Transfer-Encoding: base64\r\n")
		msg.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=%q\r\n\r\n", att.Filename))
		msg.WriteString(base64.StdEncoding.EncodeToString(raw))
		msg.WriteString("\r\n")
	}

	msg.WriteString(fmt.Sprintf("--%s--\r\n", boundary))

	return []byte(msg.String()), nil
}
