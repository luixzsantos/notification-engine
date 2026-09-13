package channel

import (
	"html"
	"strings"

	"notification-engine/internal/domain"
)

// emailBody decide o Content-Type e o corpo do e-mail: texto simples (como
// sempre foi) quando não há tabela, ou HTML com a tabela renderizada de
// verdade (domain.Table.FormatHTML) quando há — usado por Gmail e Outlook,
// os dois canais que enviam e-mail.
func emailBody(n *domain.Notification) (contentType string, body string) {
	tableHTML := n.Table.FormatHTML()
	if tableHTML == "" {
		return "text/plain", n.Message
	}

	var b strings.Builder
	if n.Message != "" {
		b.WriteString("<p>" + html.EscapeString(n.Message) + "</p>")
	}
	b.WriteString(tableHTML)

	return "text/html", b.String()
}
