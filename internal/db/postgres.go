package db

import (
	"database/sql"
	"embed"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

//go:embed schema.sql
var schemaFS embed.FS

// connMaxLifetime limita quanto tempo uma conexão fica no pool antes de ser
// reciclada — evita acumular conexões "zumbis" de processos anteriores que
// não encerraram de forma limpa (ex: kill -9 em vez de graceful shutdown).
const connMaxLifetime = 5 * time.Minute

// Connect abre e testa a conexão com o PostgreSQL. dsn segue o formato
// "postgres://user:senha@host:porta/banco?sslmode=disable".
//
// maxOpenConns limita quantas conexões este processo pode abrir
// simultaneamente. Sem esse limite, database/sql abre uma conexão nova para
// cada goroutine que precisa de uma ao mesmo tempo — sob um pico de
// concorrência (muitas notificações chegando/sendo processadas de uma vez),
// isso pode facilmente estourar o max_connections do PostgreSQL (erro "sorry,
// too many clients already"), derrubando updates de status no meio do
// processamento. Com o limite, o driver enfileira a goroutine até uma
// conexão do pool ficar livre, em vez de tentar abrir mais uma.
func Connect(dsn string, maxOpenConns int) (*sql.DB, error) {
	conn, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: falha ao abrir conexão: %w", err)
	}

	if maxOpenConns > 0 {
		conn.SetMaxOpenConns(maxOpenConns)
		conn.SetMaxIdleConns(maxOpenConns)
	}
	conn.SetConnMaxLifetime(connMaxLifetime)

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("db: falha ao conectar: %w", err)
	}

	return conn, nil
}

// EnsureSchema aplica o schema.sql embutido no binário. É idempotente
// (CREATE ... IF NOT EXISTS), então pode ser chamado toda vez que a API ou
// o worker sobem, sem depender de uma ferramenta de migration externa.
func EnsureSchema(conn *sql.DB) error {
	schema, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return fmt.Errorf("db: falha ao ler schema embutido: %w", err)
	}

	if _, err := conn.Exec(string(schema)); err != nil {
		return fmt.Errorf("db: falha ao aplicar schema: %w", err)
	}

	return nil
}
