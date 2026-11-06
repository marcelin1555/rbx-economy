# rbx-economy

Backend em Go que guarda o progresso e o ranking global de jogos do Roblox.

Nasceu de um problema que tive no meu jogo `tycoon`: o DataStore do Roblox guarda
o progresso de cada jogador, mas não dá um ranking global decente nem um lugar
fora do jogo para consultar os dados. Este serviço resolve isso. O servidor do
jogo manda o progresso por `HttpService`, e qualquer pessoa pode ler o ranking.

## O que faz

- `PUT /v1/players/{id}` grava cash e rebirths de um jogador (exige chave de API).
- `GET /v1/players/{id}` devolve o progresso de um jogador.
- `GET /v1/leaderboard?stat=rebirths|cash&limit=10` devolve o ranking.
- `GET /healthz` diz se o serviço está de pé.

## Decisões que tomei

- **Só a biblioteca padrão do Go no HTTP.** O roteador do Go 1.22 já resolve
  `GET /v1/players/{id}`. Uma dependência a menos para manter.
- **SQLite sem CGO** (`modernc.org/sqlite`). Compila em qualquer máquina e o
  banco é um arquivo só.
- **Rebirths nunca diminuem.** Se o jogo mandar um valor menor que o salvo (dado
  velho, servidor atrasado), o maior vence.
- **Sem SQL montado com texto do cliente.** O parâmetro `stat` passa por uma lista
  fixa de colunas. Há um teste tentando injetar SQL.
- **Chave comparada em tempo constante**, para não vazar por tempo de resposta.
- **Corpo limitado a 1 KiB, campos desconhecidos recusados e limite por IP.**
- **Encerramento limpo:** ao receber Ctrl+C ou SIGTERM, termina as requisições em
  andamento antes de sair.

## Como rodar

Precisa de Go 1.27 (a versão do `go.mod`).

```bash
export API_KEY="uma-chave-com-16-ou-mais-caracteres"
go run ./cmd/server
```

No PowerShell: `$env:API_KEY = "uma-chave-com-16-ou-mais-caracteres"`.

| Variável | Padrão | Para que serve |
|---|---|---|
| `API_KEY` | (obrigatória) | chave que o jogo manda em `X-API-Key` |
| `ADDR` | `:8080` | endereço do servidor |
| `DB_PATH` | `economy.db` | arquivo do SQLite |
| `RATE_PER_MIN` | `120` | requisições por IP por minuto (0 desliga) |

Exemplo:

```bash
curl -X PUT localhost:8080/v1/players/123 \
  -H "X-API-Key: $API_KEY" -H "Content-Type: application/json" \
  -d '{"name":"marcello","cash":500,"rebirths":2}'

curl "localhost:8080/v1/leaderboard?stat=rebirths&limit=5"
```

## Testes

```bash
go test ./... -cover
```

Cobertura atual: cerca de 83% na API e 78% no banco. O `main.go` não tem teste.

## Usando no Roblox

O arquivo [`roblox/EconomyClient.luau`](roblox/EconomyClient.luau) é um módulo
de servidor com repetição automática em caso de falha de rede. Ative
**Game Settings > Security > Allow HTTP Requests** e ponha a URL pública da API.
A chave fica só no servidor do jogo, nunca em LocalScript.

## O que falta

- Publicar o serviço numa hospedagem (hoje só roda localmente).
- Trocar o limite por IP por um limite por chave, já que o Roblox sai de poucos IPs.
- Métricas e migrations versionadas.
