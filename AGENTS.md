# AGENTS.md

本文件用于让后续开发者快速掌握本仓库的技术栈、目录结构、Proto 生成链路与 `task` 工具链使用方式。

## 1. 项目框架与技术栈

- 语言与模块：
  - Go `1.25.5`
  - 模块名：`github.com/jeffinity/oculus`
- 服务框架：
  - `go-kratos/kratos/v2`（同时提供 gRPC + HTTP）
  - `cobra` 作为 CLI 入口（每个 app 都有 `cmd/main.go`）
  - `google/wire` 作为依赖注入生成工具
- 协议与通信：
  - Protobuf + gRPC
  - `google.api.http` 注解生成 HTTP 映射（`protoc-gen-go-http`）
- 常见中间件（按当前实现）：
  - Recovery
  - Metrics（Prometheus）
  - Proto Validate
  - 自定义日志中间件（`kratosx.ServerLogger`）

## 2. 目录速览（当前仓库）

- `app/`：业务应用（当前含 `bookkeeping`、`mh`）
  - 典型结构：`cmd/`、`internal/{conf,data,service,server,...}`、`configs/`
- `api/`：对外业务 Proto 定义（源文件）
  - 当前示例：`api/bookkeeping/v1/bookkeeping.proto`
- `proto/`：由 `task api` 生成的 Go 代码输出目录
  - 当前示例：`proto/bookkeeping/v1/*.pb.go`
- `pkg/`：通用包（含共享 proto，如健康检查）
- `third_party/`：第三方 proto 与构建工具链
  - `third_party/build-tools/`：Task include 的通用脚本和任务定义
- `target/`：构建产物输出目录（按 app/os/arch 分层）
- `web/`：与 app 对应，编写各种 app 对应的 web 前端的相关代码实现，与对应 app 同名

## 3. Proto 与代码生成规则

### 3.1 API Proto（`api/*` -> `proto/*`）

- 源：`api/**/*.proto`
- 命令：`task api -- [path]`
  - 不带参数默认处理 `api`
  - 可选传具体路径，如：`task api -- api/bookkeeping/v1`
- 实际流程：
  1. `buf generate --template buf.api.gen.yaml --path ...`
  2. 生成落盘到 `proto/`
  3. 若出现 `proto/api/...`，会自动平铺到 `proto/...` 并删除 `proto/api`
- 生成插件（见 `buf.api.gen.yaml`）：
  - `protoc-gen-go`
  - `protoc-gen-go-grpc`
  - `protoc-gen-go-http`

### 3.2 配置 Proto（`app/*/internal/conf/conf.proto`）

- 源：各 app 的 `internal/conf/*.proto`
- 命令：`task conf -- app/<name>`
- 实际流程：
  - `third_party/build-tools/scripts/build_config.sh` 会遍历目标目录下 `conf.proto`
  - 调用 `protoc --go_out=paths=source_relative:.` 就地生成 `conf.pb.go`
  - `--proto_path` 包含模块 `third_party` 目录

## 4. Task 工具链（核心）

根 `Taskfile.yml` 主要是转发到 `third_party/build-tools/Taskfile.yml`。

### 4.1 常用任务

- `task check`：环境检查与工具安装
- `task format`：全仓库格式化 Go 代码
- `task format-app -- app/<name>`：仅格式化指定 app
- `task lint`：全仓库 lint（`golangci-lint`）
- `task conf -- app/<name>`：生成配置 proto
- `task api -- [api/path]`：生成 API proto 代码
- `task wire -- app/<name>`：生成指定 app 的 wire 代码并格式化
- `task wire-all`：生成所有 app 的 wire 代码并格式化
- `task build -- <app|all> [-f]`：构建本机架构
- `task build-linux-amd64 -- <app|all> [-f]`：构建 linux/amd64
- `task build-linux-arm64 -- <app|all> [-f]`：构建 linux/arm64
- `task deploy -- <app> <host[,host2]>`：构建并部署到远端（依赖 ssh/scp）

### 4.2 工具安装与路径约定

- `task check` 会确保以下命令可用，不存在则自动安装：
  - `protoc`
  - `buf`
  - `wire`
  - `protoc-gen-go`
  - `protoc-gen-go-grpc`
  - `protoc-gen-go-http`
  - `golangci-lint`
  - `gci`
  - `golines`
  - 以及若干 proto/openapi 相关插件
- 默认安装目录：
  - `$(repo)/.build/.bin`
- `task` 脚本会优先使用本地工具，再回退系统 PATH。

## 5. 建议开发流程（本仓库）

1. 首次进入仓库：
   - `task check`
2. 修改 `api/*.proto` 后：
   - `task api -- <具体目录或文件上层目录>`
3. 修改 `app/*/internal/conf/conf.proto` 后：
   - `task conf -- app/<name>`
4. 修改依赖注入相关文件（provider/wire）后：
   - `task wire -- app/<name>`
5. 提交前建议：
   - `task format`
   - `task lint`
6. 本地或目标平台构建：
   - `task build -- app/<name>`
   - 或 `task build-linux-amd64 -- app/<name>`

## 6. 额外说明

- 当前仓库是多 app 单仓（`app/*`），多数任务支持传 `app/<name>` 或简写 `<name>`。
- `build` 脚本带 checksum 机制，源码未变化会跳过构建；加 `-f` 可强制重建。
- 构建产物路径为：`target/<app>/<os>/<arch>/`。
