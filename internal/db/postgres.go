package db

import (
	"database/sql"
	"embed"
	"fmt"

	_ "github.com/lib/pq"
)

//go:embed schema.sql
var schemaFS embed.FS

// Connect abre e testa a conexão com o PostgreSQL. dsn segue o formato
// "postgres://user:senha@host:porta/banco?sslmode=disable".
func Connect(dsn string) (*sql.DB, error) {
	conn, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: falha ao abrir conexão: %w", err)
	}

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
