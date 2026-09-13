package domain

import (
	"errors"
	"strings"
	"testing"
)

func validNotification() *Notification {
	return &Notification{
		Channel: ChannelWebhook,
		Target:  "https://example.com/hook",
		Message: "olá",
	}
}

func TestValidate_Success(t *testing.T) {
	n := validNotification()
	if err := n.Validate(); err != nil {
		t.Fatalf("esperava validação bem-sucedida, obteve: %v", err)
	}
}

func TestValidate_InvalidChannel(t *testing.T) {
	n := validNotification()
	n.Channel = "sms" // canal não suportado

	err := n.Validate()
	if !errors.Is(err, ErrInvalidChannel) {
		t.Fatalf("esperava ErrInvalidChannel, obteve: %v", err)
	}
}

func TestValidate_EmptyTarget(t *testing.T) {
	n := validNotification()
	n.Target = ""

	if err := n.Validate(); !errors.Is(err, ErrEmptyTarget) {
		t.Fatalf("esperava ErrEmptyTarget, obteve: %v", err)
	}
}

func TestValidate_EmptyMessageWithoutPayloadOrAttachment(t *testing.T) {
	n := validNotification()
	n.Message = ""

	if err := n.Validate(); !errors.Is(err, ErrEmptyMessage) {
		t.Fatalf("esperava ErrEmptyMessage, obteve: %v", err)
	}
}

func TestValidate_EmptyMessageAllowedWithPayload(t *testing.T) {
	n := validNotification()
	n.Message = ""
	n.Payload = map[string]any{"custom": "body"}

	if err := n.Validate(); err != nil {
		t.Fatalf("mensagem vazia deveria ser aceita quando payload é informado, obteve: %v", err)
	}
}

func TestValidate_EmptyMessageAllowedWithAttachment(t *testing.T) {
	n := validNotification()
	n.Message = ""
	n.Attachments = []Attachment{{Filename: "a.png", ContentType: "image/png", Data: "aGVsbG8="}}

	if err := n.Validate(); err != nil {
		t.Fatalf("mensagem vazia deveria ser aceita quando há anexo, obteve: %v", err)
	}
}

func TestValidate_TooManyAttachments(t *testing.T) {
	n := validNotification()
	for i := 0; i < MaxAttachments+1; i++ {
		n.Attachments = append(n.Attachments, Attachment{Filename: "a.png", Data: "aGVsbG8="})
	}

	if err := n.Validate(); !errors.Is(err, ErrTooManyAttachments) {
		t.Fatalf("esperava ErrTooManyAttachments, obteve: %v", err)
	}
}

func TestValidate_AttachmentTooLarge(t *testing.T) {
	n := validNotification()
	// gera uma string base64 grande o suficiente para decodificar > 8MB
	huge := strings.Repeat("A", (MaxAttachmentBytes+1024)/3*4)
	n.Attachments = []Attachment{{Filename: "big.bin", Data: huge}}

	if err := n.Validate(); !errors.Is(err, ErrAttachmentTooLarge) {
		t.Fatalf("esperava ErrAttachmentTooLarge, obteve: %v", err)
	}
}

func TestValidate_StripsDataURIPrefixFromAttachment(t *testing.T) {
	n := validNotification()
	n.Attachments = []Attachment{{
		Filename: "pixel.png",
		Data:     "data:image/png;base64,aGVsbG8=",
	}}

	if err := n.Validate(); err != nil {
		t.Fatalf("validação inesperadamente falhou: %v", err)
	}
	if n.Attachments[0].Data != "aGVsbG8=" {
		t.Fatalf("esperava prefixo data URI removido, obteve: %q", n.Attachments[0].Data)
	}
}

func TestIsValidChannel(t *testing.T) {
	valid := []ChannelType{ChannelDiscord, ChannelTelegram, ChannelWebhook, ChannelEmail, ChannelWhatsApp, ChannelOutlook}
	for _, c := range valid {
		if !IsValidChannel(c) {
			t.Errorf("esperava %q ser um canal válido", c)
		}
	}

	if IsValidChannel("sms") {
		t.Error("esperava 'sms' ser um canal inválido")
	}
}

func TestValidate_WhatsAppTemplateSatisfiesEmptyMessage(t *testing.T) {
	n := &Notification{
		Channel:      ChannelWhatsApp,
		Target:       "+5511999999999",
		TemplateName: "payment-approved",
		TemplateParams: []string{
			"João", "149,90",
		},
	}

	if err := n.Validate(); err != nil {
		t.Fatalf("mensagem de template sem 'message' deveria ser aceita, obteve: %v", err)
	}
}

func TestValidate_WhatsAppWithoutMessageOrTemplateFails(t *testing.T) {
	n := &Notification{
		Channel: ChannelWhatsApp,
		Target:  "+5511999999999",
	}

	if err := n.Validate(); !errors.Is(err, ErrEmptyMessage) {
		t.Fatalf("esperava ErrEmptyMessage sem message nem template, obteve: %v", err)
	}
}

func TestValidate_OutlookRequiresTarget(t *testing.T) {
	n := &Notification{
		Channel: ChannelOutlook,
		Subject: "Assunto",
		Message: "corpo",
	}

	if err := n.Validate(); !errors.Is(err, ErrEmptyTarget) {
		t.Fatalf("esperava ErrEmptyTarget, obteve: %v", err)
	}
}

func TestTable_FormatMonospace(t *testing.T) {
	table := &Table{
		Headers: []string{"Nome", "Status"},
		Rows: [][]string{
			{"João", "Aprovado"},
			{"Maria", "Pendente"},
		},
	}

	out := table.FormatMonospace()

	if !strings.Contains(out, "Nome") || !strings.Contains(out, "Status") {
		t.Fatalf("esperava cabeçalho na saída, obteve:\n%s", out)
	}
	if !strings.Contains(out, "João") || !strings.Contains(out, "Maria") {
		t.Fatalf("esperava linhas na saída, obteve:\n%s", out)
	}
	lines := strings.Split(out, "\n")
	if len(lines) != 4 { // header + separator + 2 rows
		t.Fatalf("esperava 4 linhas (header, separador, 2 linhas), obteve %d:\n%s", len(lines), out)
	}
}

func TestTable_FormatMonospace_NilOrEmpty(t *testing.T) {
	var nilTable *Table
	if got := nilTable.FormatMonospace(); got != "" {
		t.Errorf("esperava string vazia para tabela nil, obteve %q", got)
	}

	empty := &Table{}
	if got := empty.FormatMonospace(); got != "" {
		t.Errorf("esperava string vazia para tabela sem linhas, obteve %q", got)
	}
}

func TestTable_FormatMonospace_UnevenRows(t *testing.T) {
	table := &Table{
		Rows: [][]string{
			{"a", "b", "c"},
			{"x"},
		},
	}

	// não deveria entrar em pânico com linhas de tamanhos diferentes
	out := table.FormatMonospace()
	if !strings.Contains(out, "a") || !strings.Contains(out, "x") {
		t.Fatalf("saída inesperada: %q", out)
	}
}

func TestTable_FormatHTML(t *testing.T) {
	table := &Table{
		Headers: []string{"Nome", "<script>"},
		Rows: [][]string{
			{"João", "Aprovado"},
		},
	}

	out := table.FormatHTML()

	if !strings.Contains(out, "<table") {
		t.Fatalf("esperava tag <table>, obteve: %s", out)
	}
	if strings.Contains(out, "<script>") {
		t.Fatal("esperava conteúdo escapado (XSS), mas <script> apareceu cru na saída")
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Fatalf("esperava conteúdo escapado como &lt;script&gt;, obteve: %s", out)
	}
}

func TestValidate_TableSatisfiesEmptyMessage(t *testing.T) {
	n := &Notification{
		Channel: ChannelTeams,
		Target:  "https://prod-00.westus.logic.azure.com/workflows/abc/triggers/manual/paths/invoke",
		Table:   &Table{Rows: [][]string{{"a", "b"}}},
	}

	if err := n.Validate(); err != nil {
		t.Fatalf("tabela sem message deveria ser aceita, obteve: %v", err)
	}
}
