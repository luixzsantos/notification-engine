// Package security concentra proteções de borda que não são regra de
// domínio nem de infraestrutura: hoje, mitigação de SSRF (Server-Side
// Request Forgery) para canais que fazem requisições HTTP a uma URL
// fornecida pelo cliente da API (webhook genérico e Discord).
package security

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrBlockedTarget é retornado quando uma URL de destino é rejeitada por
// apontar (ou poder apontar, via DNS) para um endereço privado/interno.
// Tratado pelo handler HTTP como erro de validação (400), não erro interno.
var ErrBlockedTarget = errors.New("destino bloqueado por política de segurança (SSRF)")

// ValidateTargetURL faz uma checagem rápida e síncrona (sem I/O de rede) na
// hora da criação da notificação: rejeita esquemas diferentes de http/https,
// "localhost" e literais de IP privado/loopback/link-local. Não resolve
// hostnames via DNS aqui — isso bloquearia a requisição da API só para
// validar, e ainda assim não fecharia brechas de DNS rebinding (o hostname
// pode resolver para um IP diferente entre a validação e o envio real). A
// proteção definitiva contra isso é o SafeHTTPClient, que resolve e valida
// o IP no momento exato da conexão, quando o worker efetivamente envia.
func ValidateTargetURL(rawURL string, allowPrivateNetworks bool) error {
	if allowPrivateNetworks {
		return nil
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%w: URL malformada: %v", ErrBlockedTarget, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: apenas URLs http/https são permitidas como destino", ErrBlockedTarget)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("%w: URL sem host", ErrBlockedTarget)
	}
	if strings.EqualFold(host, "localhost") {
		return fmt.Errorf("%w: localhost não é um destino permitido", ErrBlockedTarget)
	}
	if ip := net.ParseIP(host); ip != nil && isBlockedIP(ip) {
		return fmt.Errorf("%w: endereço IP privado/interno (%s)", ErrBlockedTarget, ip)
	}

	return nil
}

// SafeHTTPClient monta um *http.Client cujo Transport resolve o hostname e
// recusa a conexão se QUALQUER IP resolvido cair numa faixa privada,
// loopback, link-local (o que cobre o endereço de metadados de nuvem
// 169.254.169.254) ou multicast — a checagem acontece no momento exato do
// Dial, então cobre também hostnames cujo DNS aponta (ou passa a apontar,
// em um ataque de DNS rebinding) para dentro da rede interna.
// allowPrivateNetworks desliga a proteção (uso local/dev apenas).
func SafeHTTPClient(timeout time.Duration, allowPrivateNetworks bool) *http.Client {
	dialer := &net.Dialer{Timeout: timeout}

	if allowPrivateNetworks {
		return &http.Client{Timeout: timeout, Transport: &http.Transport{DialContext: dialer.DialContext}}
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("security: endereço inválido %q: %w", addr, err)
			}

			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, fmt.Errorf("security: falha ao resolver %q: %w", host, err)
			}
			if len(ips) == 0 {
				return nil, fmt.Errorf("security: %q não resolveu para nenhum IP", host)
			}

			for _, ip := range ips {
				if isBlockedIP(ip) {
					return nil, fmt.Errorf("%w: %q resolve para endereço privado/interno (%s)", ErrBlockedTarget, host, ip)
				}
			}

			// Conecta explicitamente no IP já validado (em vez de deixar o
			// dialer padrão resolver de novo), fechando a janela entre a
			// checagem acima e a conexão real.
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
		},
	}

	return &http.Client{Timeout: timeout, Transport: transport}
}

func isBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() ||
		ip.IsPrivate() ||
		ip.IsMulticast()
}
