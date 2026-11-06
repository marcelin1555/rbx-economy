package api_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelin1555/rbx-economy/internal/api"
	"github.com/marcelin1555/rbx-economy/internal/store"
)

const key = "segredo-de-teste"

func newServer(t *testing.T, rate int) *httptest.Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := api.NewHandler(st, api.Config{APIKey: key, RatePerMin: rate, MaxLeaderLen: 50}, log)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func do(t *testing.T, method, url, body, apiKey string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestPutRequiresKey(t *testing.T) {
	srv := newServer(t, 0)
	resp := do(t, "PUT", srv.URL+"/v1/players/1", `{"name":"ana","cash":1,"rebirths":0}`, "errada")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("esperava 401, veio %d", resp.StatusCode)
	}
}

func TestPutThenGet(t *testing.T) {
	srv := newServer(t, 0)
	resp := do(t, "PUT", srv.URL+"/v1/players/7", `{"name":"ana","cash":120,"rebirths":3}`, key)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT: esperava 204, veio %d", resp.StatusCode)
	}

	resp = do(t, "GET", srv.URL+"/v1/players/7", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET: esperava 200, veio %d", resp.StatusCode)
	}
	var p store.Player
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}
	if p.Name != "ana" || p.Cash != 120 || p.Rebirths != 3 {
		t.Fatalf("dados errados: %+v", p)
	}
}

func TestGetUnknownPlayer(t *testing.T) {
	srv := newServer(t, 0)
	if resp := do(t, "GET", srv.URL+"/v1/players/404", "", ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("esperava 404, veio %d", resp.StatusCode)
	}
}

func TestPutValidation(t *testing.T) {
	srv := newServer(t, 0)
	casos := map[string]string{
		"nome vazio":         `{"name":"","cash":1,"rebirths":0}`,
		"cash negativo":      `{"name":"a","cash":-5,"rebirths":0}`,
		"campo desconhecido": `{"name":"a","cash":1,"rebirths":0,"admin":true}`,
		"json quebrado":      `{"name":`,
	}
	for nome, corpo := range casos {
		t.Run(nome, func(t *testing.T) {
			resp := do(t, "PUT", srv.URL+"/v1/players/1", corpo, key)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("esperava 400, veio %d", resp.StatusCode)
			}
		})
	}
}

func TestLeaderboard(t *testing.T) {
	srv := newServer(t, 0)
	do(t, "PUT", srv.URL+"/v1/players/1", `{"name":"a","cash":10,"rebirths":9}`, key)
	do(t, "PUT", srv.URL+"/v1/players/2", `{"name":"b","cash":99,"rebirths":1}`, key)

	resp := do(t, "GET", srv.URL+"/v1/leaderboard?stat=cash&limit=1", "", "")
	var out struct {
		Stat    string         `json:"stat"`
		Players []store.Player `json:"players"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Players) != 1 || out.Players[0].UserID != 2 {
		t.Fatalf("ranking errado: %+v", out)
	}

	if resp := do(t, "GET", srv.URL+"/v1/leaderboard?stat=hack", "", ""); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("stat inválido deveria dar 400, veio %d", resp.StatusCode)
	}
}

func TestRateLimit(t *testing.T) {
	srv := newServer(t, 3)
	var last int
	for i := 0; i < 5; i++ {
		last = do(t, "GET", srv.URL+"/healthz", "", "").StatusCode
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("esperava 429 depois do limite, veio %d", last)
	}
}
