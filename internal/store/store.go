// Package store guarda o progresso dos jogadores em SQLite.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound é devolvido quando o jogador não existe.
var ErrNotFound = errors.New("jogador não encontrado")

// Player é o progresso de um jogador do Roblox.
type Player struct {
	UserID    int64     `json:"userId"`
	Name      string    `json:"name"`
	Cash      int64     `json:"cash"`
	Rebirths  int64     `json:"rebirths"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Store é o acesso ao banco.
type Store struct {
	db *sql.DB
}

// stats lista as colunas permitidas no ranking. Nunca montamos SQL com
// texto vindo do cliente: só estes valores fixos entram na consulta.
var stats = map[string]string{
	"cash":     "cash",
	"rebirths": "rebirths",
}

// ValidStat diz se o ranking pedido existe.
func ValidStat(s string) bool {
	_, ok := stats[s]
	return ok
}

// Open abre (e cria, se preciso) o banco no caminho dado.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("abrir banco: %w", err)
	}
	// SQLite aceita uma escrita por vez; uma conexão evita "database is locked".
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close fecha o banco.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS players (
	user_id    INTEGER PRIMARY KEY,
	name       TEXT    NOT NULL,
	cash       INTEGER NOT NULL DEFAULT 0 CHECK (cash >= 0),
	rebirths   INTEGER NOT NULL DEFAULT 0 CHECK (rebirths >= 0),
	updated_at TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_players_rebirths ON players (rebirths DESC);
CREATE INDEX IF NOT EXISTS idx_players_cash     ON players (cash DESC);`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("migração: %w", err)
	}
	return nil
}

// Upsert grava o progresso. Rebirths nunca diminuem: se o jogo mandar um
// valor menor que o salvo, o maior vence (protege contra dados velhos).
func (s *Store) Upsert(ctx context.Context, p Player) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO players (user_id, name, cash, rebirths, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(user_id) DO UPDATE SET
	name       = excluded.name,
	cash       = excluded.cash,
	rebirths   = MAX(players.rebirths, excluded.rebirths),
	updated_at = excluded.updated_at`,
		p.UserID, p.Name, p.Cash, p.Rebirths, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("salvar jogador: %w", err)
	}
	return nil
}

// Get busca um jogador pelo id.
func (s *Store) Get(ctx context.Context, userID int64) (Player, error) {
	var p Player
	var updated string
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, name, cash, rebirths, updated_at FROM players WHERE user_id = ?`, userID).
		Scan(&p.UserID, &p.Name, &p.Cash, &p.Rebirths, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Player{}, ErrNotFound
	}
	if err != nil {
		return Player{}, fmt.Errorf("buscar jogador: %w", err)
	}
	p.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return p, nil
}

// Top devolve os melhores jogadores pelo ranking pedido.
func (s *Store) Top(ctx context.Context, stat string, limit int) ([]Player, error) {
	col, ok := stats[stat]
	if !ok {
		return nil, fmt.Errorf("ranking inválido: %q", stat)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT user_id, name, cash, rebirths, updated_at FROM players
		 ORDER BY `+col+` DESC, user_id ASC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("ranking: %w", err)
	}
	defer rows.Close()

	var out []Player
	for rows.Next() {
		var p Player
		var updated string
		if err := rows.Scan(&p.UserID, &p.Name, &p.Cash, &p.Rebirths, &updated); err != nil {
			return nil, fmt.Errorf("ler linha: %w", err)
		}
		p.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		out = append(out, p)
	}
	return out, rows.Err()
}
