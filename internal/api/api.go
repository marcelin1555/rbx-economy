// Package api expõe o progresso dos jogadores por HTTP/JSON.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/marcelin1555/rbx-economy/internal/store"
)

// Players é o que a API precisa do banco (facilita os testes).
type Players interface {
	Upsert(ctx context.Context, p store.Player) error
	Get(ctx context.Context, userID int64) (store.Player, error)
	Top(ctx context.Context, stat string, limit int) ([]store.Player, error)
}

// Config reúne as opções do servidor HTTP.
type Config struct {
	APIKey       string // chave que o servidor do jogo manda no cabeçalho X-API-Key
	RatePerMin   int    // máximo de requisições por IP por minuto
	MaxLeaderLen int    // teto do parâmetro limit
}

const (
	maxBody    = 1 << 10 // 1 KiB basta para um jogador
	maxNameLen = 20      // limite de nome de usuário do Roblox
)

// NewHandler monta as rotas.
func NewHandler(p Players, cfg Config, log *slog.Logger) http.Handler {
	s := &server{players: p, cfg: cfg, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /v1/leaderboard", s.leaderboard)
	mux.HandleFunc("GET /v1/players/{id}", s.getPlayer)
	mux.Handle("PUT /v1/players/{id}", s.requireKey(http.HandlerFunc(s.putPlayer)))

	return logging(log, newLimiter(cfg.RatePerMin).middleware(mux))
}

type server struct {
	players Players
	cfg     Config
	log     *slog.Logger
}

type putBody struct {
	Name     string `json:"name"`
	Cash     int64  `json:"cash"`
	Rebirths int64  `json:"rebirths"`
}

func (s *server) putPlayer(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var b putBody
	if err := dec.Decode(&b); err != nil {
		writeError(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if b.Name == "" || len(b.Name) > maxNameLen {
		writeError(w, http.StatusBadRequest, "name deve ter de 1 a 20 caracteres")
		return
	}
	if b.Cash < 0 || b.Rebirths < 0 {
		writeError(w, http.StatusBadRequest, "cash e rebirths não podem ser negativos")
		return
	}
	err := s.players.Upsert(r.Context(), store.Player{
		UserID: id, Name: b.Name, Cash: b.Cash, Rebirths: b.Rebirths,
	})
	if err != nil {
		s.log.Error("salvar jogador", "err", err, "userId", id)
		writeError(w, http.StatusInternalServerError, "erro interno")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) getPlayer(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	p, err := s.players.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "jogador não encontrado")
		return
	}
	if err != nil {
		s.log.Error("buscar jogador", "err", err, "userId", id)
		writeError(w, http.StatusInternalServerError, "erro interno")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *server) leaderboard(w http.ResponseWriter, r *http.Request) {
	stat := r.URL.Query().Get("stat")
	if stat == "" {
		stat = "rebirths"
	}
	if !store.ValidStat(stat) {
		writeError(w, http.StatusBadRequest, "stat deve ser cash ou rebirths")
		return
	}
	limit := 10
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "limit inválido")
			return
		}
		limit = min(n, s.cfg.MaxLeaderLen)
	}
	top, err := s.players.Top(r.Context(), stat, limit)
	if err != nil {
		s.log.Error("ranking", "err", err)
		writeError(w, http.StatusInternalServerError, "erro interno")
		return
	}
	if top == nil {
		top = []store.Player{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"stat": stat, "players": top})
}

func parseID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "id inválido")
		return 0, false
	}
	return id, true
}

// requireKey só deixa passar quem manda a chave certa. A comparação é em
// tempo constante para não vazar a chave pelo tempo de resposta.
func (s *server) requireKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("X-API-Key"))
		if subtle.ConstantTimeCompare(got, []byte(s.cfg.APIKey)) != 1 {
			writeError(w, http.StatusUnauthorized, "chave inválida")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func logging(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Info("requisição", "método", r.Method, "caminho", r.URL.Path,
			"status", rec.status, "duração", time.Since(start).String())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// limiter é um limite simples por IP, em janela de um minuto.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Time
	seen   map[string]int
}

func newLimiter(maxPerMin int) *limiter {
	return &limiter{max: maxPerMin, seen: map[string]int{}}
}

func (l *limiter) allow(ip string) bool {
	if l.max <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if now := time.Now(); now.Sub(l.window) >= time.Minute {
		l.window = now
		l.seen = map[string]int{}
	}
	l.seen[ip]++
	return l.seen[ip] <= l.max
}

func (l *limiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		if i := lastColon(ip); i > 0 {
			ip = ip[:i]
		}
		if !l.allow(ip) {
			writeError(w, http.StatusTooManyRequests, "muitas requisições")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func lastColon(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ':' {
			return i
		}
	}
	return -1
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
