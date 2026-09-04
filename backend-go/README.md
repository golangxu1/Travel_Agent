# Go 后端骨架

这是旅行助手迁移阶段 1/2 的独立 Go module。HTTP 层和编排层只使用标准库，planner 支持 fake 模式和 OpenAI-compatible/Anthropic HTTP Provider；高德、追踪持久化和媒体代理仍未迁移，Python 服务仍是完整生产/回滚基线。

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

请求字段、中文枚举和 stream 的 `data: <JSON>\n\n` / `data: [DONE]\n\n` framing 与现有 Python API 保持兼容。尚未迁移的 `/api/images`、`/api/traces` 和 `/api/poi-photo` 继续由 Python 服务提供。

服务端拒绝未知 JSON 字段、空值、非法日期、超过 31 天的行程、超长文本及超过 64 KiB 的请求体。错误响应使用稳定的 `success=false`、`code`、脱敏 `error`，以及校验失败时的 `fields` 字段。
