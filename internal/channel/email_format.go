package channel

import (
	"html"
	"strings"

	"notification-engine/internal/domain"
)

// emailBody decide o Content-Type e o corpo do e-mail: texto simples (como
// sempre foi) quando não há tabela nem título, ou HTML com título(s) de
// verdade (<h1>/<h2>/<h3>) e/ou a tabela renderizada de verdade
// (domain.Table.FormatHTML) quando há — usado por Gmail e Outlook, os dois
// canais que enviam e-mail.
func emailBody(n *domain.Notification) (contentType string, body string) {
	messageHTML, hasHeadingLines := emailMessageHTML(n.Message)
	tableHTML := n.Table.FormatHTML()

	if !hasHeadingLines && tableHTML == "" {
		return "text/plain", n.Message
	}

	var b strings.Builder
	b.WriteString(messageHTML)
	b.WriteString(tableHTML)

	return "text/html", b.String()
}

// emailMessageHTML converte linhas de título ("#"/"##"/"###") em tags
// <h1>/<h2>/<h3> reais e o restante do texto em parágrafos <p>, escapando o
// conteúdo. Sem nenhum título, apenas envolve a mensagem inteira num único
// <p> (comportamento anterior) e reporta hasHeadingLines=false, para que o
// chamador saiba que não precisa forçar o corpo pra HTML só por causa disso.
func emailMessageHTML(message string) (htmlOut string, hasHeadingLines bool) {
	if message == "" {
		return "", false
	}

	lines := headingLines(message)
	if !hasHeading(lines) {
		return "<p>" + html.EscapeString(message) + "</p>", false
	}

	var b strings.Builder
	var paragraph []string
	flush := func() {
		if len(paragraph) == 0 {
			return
		}
		b.WriteString("<p>" + html.EscapeString(strings.Join(paragraph, "\n")) + "</p>")
		paragraph = nil
	}

	for _, l := range lines {
		if l.Level == 0 {
			paragraph = append(paragraph, l.Text)
			continue
		}
		flush()
		tag := "h3"
		switch l.Level {
		case 1:
			tag = "h1"
		case 2:
			tag = "h2"
		}
		b.WriteString("<" + tag + ">" + html.EscapeString(l.Text) + "</" + tag + ">")
	}
	flush()

	return b.String(), true
}
