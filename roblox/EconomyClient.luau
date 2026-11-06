--!strict
-- Módulo de SERVIDOR (ServerScriptService). Nunca coloque a chave num LocalScript:
-- tudo que está no cliente pode ser lido pelo jogador.
--
-- Uso:
--   local Economy = require(script.Parent.EconomyClient)
--   Economy.save(player, { cash = 120, rebirths = 2 })
--   local top = Economy.leaderboard("rebirths", 10)

local HttpService = game:GetService("HttpService")

local BASE_URL = "https://SEU-DOMINIO-AQUI" -- URL pública da API
local API_KEY = "TROQUE-POR-UMA-CHAVE-GRANDE" -- a mesma da variável API_KEY do servidor Go

local Economy = {}

export type Progress = { cash: number, rebirths: number }
export type Entry = { userId: number, name: string, cash: number, rebirths: number }

-- Tenta de novo em falhas de rede, com espera crescente.
local function request(options: { [string]: any }, attempts: number): (boolean, any)
	local lastError: any = nil
	for i = 1, attempts do
		local ok, result = pcall(HttpService.RequestAsync, HttpService, options)
		if ok and result.Success then
			return true, result
		end
		lastError = if ok then `HTTP {result.StatusCode}` else result
		-- 4xx não adianta repetir: o pedido está errado.
		if ok and result.StatusCode >= 400 and result.StatusCode < 500 then
			break
		end
		task.wait(2 ^ i)
	end
	return false, lastError
end

function Economy.save(player: Player, progress: Progress): boolean
	local ok, err = request({
		Url = `{BASE_URL}/v1/players/{player.UserId}`,
		Method = "PUT",
		Headers = {
			["Content-Type"] = "application/json",
			["X-API-Key"] = API_KEY,
		},
		Body = HttpService:JSONEncode({
			name = player.Name,
			cash = math.floor(progress.cash),
			rebirths = math.floor(progress.rebirths),
		}),
	}, 3)
	if not ok then
		warn(`[Economy] falha ao salvar {player.Name}: {err}`)
	end
	return ok
end

function Economy.leaderboard(stat: "cash" | "rebirths", limit: number): { Entry }
	local ok, result = request({
		Url = `{BASE_URL}/v1/leaderboard?stat={stat}&limit={limit}`,
		Method = "GET",
	}, 2)
	if not ok then
		warn(`[Economy] falha no ranking: {result}`)
		return {}
	end
	local data = HttpService:JSONDecode(result.Body)
	return data.players
end

return Economy
