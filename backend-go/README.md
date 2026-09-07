# Go 后端骨架

这是旅行助手迁移阶段 1/2/3 的独立 Go module。HTTP 层和编排层只使用标准库，planner 支持 fake 模式和 OpenAI-compatible/Anthropic HTTP Provider；高德 REST、POI 补全和安全图片/静态地图代理已迁移，追踪持久化仍由 Python 服务提供。

## 本地运行

本目录需要 Go 1.22 或更高版本。请在自己的开发机上手动安装并配置 Go，不要修改 Codex 运行环境。配置完成后：

```powershell
cd backend-go
go test ./...
go vet ./...
go run ./cmd/server
```

先用 `go version` 确认安装结果；本项目不自动安装 Go、调整 PATH 或写入系统配置。

默认监听 `http://localhost:8001`，可用 `TRAVEL_AGENT_GO_ADDR=:9001` 修改地址。设置 `TRAVEL_AGENT_MODE=fake` 和 `TRAVEL_AGENT_FAKE_STAGE_DELAY_MS=50` 可观察流式阶段和取消行为。

## 高德地图配置

高德功能是可选的。未设置 `AMAP_API_KEY` 时，计划生成仍可运行，只是不返回 POI 坐标、图片和地图。手动设置 Key 后，Go 服务会通过高德 REST API 补全 POI，并使用自身的代理 URL 返回媒体：Key 和原始图片地址不会进入浏览器。

```powershell
$env:AMAP_API_KEY="由你手动设置的服务端 Key"
# 部署时设置外部访问地址；本地默认 http://localhost:8001
$env:TRAVEL_AGENT_PUBLIC_BASE_URL="https://travel.example.com"
go run ./cmd/server
```

`/api/poi-photo` 和 `/api/maps/static` 只接受 Go 服务签发的短期 `token`，不再接受任意 `url` 参数。图片源限制为 HTTPS 的高德域名，代理拒绝私网地址、非图片响应、超大响应和超过两次的重定向。

## LLM 配置

当 `TRAVEL_AGENT_MODE` 未设置时，没有 `MODEL_API_KEY` 或 `MODEL_ID` 会自动使用 fake 模式；两者都存在时使用 real 模式。也可以显式设置 `TRAVEL_AGENT_MODE=real`。

```powershell
$env:TRAVEL_AGENT_MODE="real"
$env:MODEL_PROVIDER="deepseek"
$env:MODEL_ID="deepseek-chat"
$env:MODEL_API_KEY="由你手动设置的密钥"
# 可选：MODEL_BASE_URL、MODEL_TIMEOUT_SECONDS、MODEL_MAX_OUTPUT_TOKENS
go run ./cmd/server
```

支持的 Provider：

- `openai`：默认 `https://api.openai.com/v1`
- `deepseek`：默认 `https://api.deepseek.com/v1`
- `anthropic`：默认 `https://api.anthropic.com`

密钥只从进程环境读取，不会写入仓库、响应、trace 或普通日志。当前 real 模式使用一次请求生成一个模块，Phase 1 的天气、目的地和住宿并行，行程和预算按依赖顺序执行。

## 当前兼容接口

- `GET /health`
- `POST /api/plan`
- `POST /api/plan/stream`
- `POST /api/plan/regenerate`
- `GET /api/images?query=城市`
- `GET /api/poi-photo?token=...`
- `GET /api/maps/static?token=...`

请求字段、中文枚举和 stream 的 `data: <JSON>\n\n` / `data: [DONE]\n\n` framing 与现有 Python API 保持兼容。`/api/traces` 和 `/api/traces/{id}` 仍由 Python 服务提供，因此在 trace 迁移完成前，前端不能将全部 `/api/*` 请求统一切到 Go。

服务端拒绝未知 JSON 字段、空值、非法日期、超过 31 天的行程、超长文本及超过 64 KiB 的请求体。错误响应使用稳定的 `success=false`、`code`、脱敏 `error`，以及校验失败时的 `fields` 字段。
