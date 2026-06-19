# eks-server MCP AI 使用手册

本文档面向使用 `eks-server` 的 AI 客户端，描述当前已部署在 `mh` 主机上的 MCP 服务连接方式、工具列表、参数 schema、返回格式和调用约束。

## 服务定位

`eks-server` 是一个 EKS 运维 MCP 服务。AI 通过它可以经 JumpServer 进入已配置的 EKS 运维资产机，并执行 `kubectl`、`helm` 等单行命令。

当前已部署实例：

| 项 | 值 |
| --- | --- |
| 部署主机 | `mh` |
| MCP 入口 | Unix socket `/run/eks-server/mcp.sock` |
| MCP 协议 | JSON-RPC 2.0，line-delimited JSON |
| MCP protocolVersion | `2024-11-05` |
| serverInfo.name | `oculus-eks-server` |
| serverInfo.version | `v0.1.0` |
| 逻辑集群 | `jumpserver-sg` |
| 可用环境 | `dev`、`test` |

AI 不需要启动服务，也不需要管理 systemd。只需要连接 `mh` 上的 Unix socket 并发送 MCP JSON-RPC 消息。

## 连接方式

如果 AI 的执行环境可以 SSH 到 `mh`，可以在 `mh` 上通过 Unix socket 发送请求。

最小 initialize 请求：

```bash
ssh mh "printf '%s\n' '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{}}' | nc -U -w 5 /run/eks-server/mcp.sock"
```

成功响应示例：

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "capabilities": {
      "tools": {}
    },
    "protocolVersion": "2024-11-05",
    "serverInfo": {
      "name": "oculus-eks-server",
      "version": "v0.1.0"
    }
  }
}
```

请求必须以换行符结束。一个 socket 连接里可以连续发送多条 JSON-RPC 请求，服务会逐行读取并逐行返回响应。

## 工具总览

| 工具 | 用途 | 是否变更集群状态 |
| --- | --- | --- |
| `eks_list_clusters` | 查看逻辑集群、环境和连接池状态 | 否 |
| `eks_query` | 执行查询类命令，走 query pool | 取决于传入命令 |
| `eks_deploy` | 执行部署/变更类命令，走 deploy pool | 取决于传入命令 |

重要：服务端不做命令前缀白名单限制，只校验命令非空且为单行。AI 必须自行判断命令风险。查询类任务优先使用 `eks_query`；变更类任务只有在用户明确要求时才使用 `eks_deploy`。

## 工具详情

### `eks_list_clusters`

用途：获取当前配置的逻辑集群、环境、query pool 和 deploy pool 状态。

参数：空对象 `{}`。

请求：

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "eks_list_clusters",
    "arguments": {}
  }
}
```

返回内容：`result.content[0].text` 是 JSON 字符串，解析后为数组。

典型结构：

```json
[
  {
    "name": "jumpserver-sg",
    "envs": [
      {
        "name": "dev",
        "query_pool": {
          "min_size": 0,
          "max_size": 1,
          "idle": 0,
          "active": 0,
          "total": 0
        },
        "deploy_pool": {
          "min_size": 0,
          "max_size": 3,
          "idle": 0,
          "active": 0,
          "total": 0
        }
      }
    ]
  }
]
```

### `eks_query`

用途：执行查询、排查、读取状态类命令。

参数 schema：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `cluster` | string | 是 | 逻辑集群名。当前使用 `jumpserver-sg` |
| `env` | string | 是 | 环境名。当前可用 `dev`、`test` |
| `command` | string | 是 | 单行 shell 命令 |
| `timeout_seconds` | number | 否 | 命令超时时间，秒 |

推荐用途：

- `kubectl get ...`
- `kubectl describe ...`
- `kubectl logs ...`
- `kubectl top ...`
- `helm status ...`
- `helm history ...`

请求示例：查询 dev namespace pods。

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/call",
  "params": {
    "name": "eks_query",
    "arguments": {
      "cluster": "jumpserver-sg",
      "env": "dev",
      "command": "kubectl -n scloud-common-dev get pods -o wide",
      "timeout_seconds": 120
    }
  }
}
```

请求示例：查询 test 服务日志。

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "eks_query",
    "arguments": {
      "cluster": "jumpserver-sg",
      "env": "test",
      "command": "kubectl -n scloud-common-test logs deploy/ward-api --tail=200",
      "timeout_seconds": 120
    }
  }
}
```

### `eks_deploy`

用途：执行部署、回滚、apply、rollout 等可能改变集群状态的命令。仅当用户明确要求变更时使用。

参数 schema 与 `eks_query` 相同。

适用命令示例：

- `helm upgrade ...`
- `helm rollback ...`
- `kubectl apply ...`
- `kubectl rollout ...`

请求示例：查看 rollout 状态。这条命令本身通常只读，但放在 deploy pool 也可以。

```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "method": "tools/call",
  "params": {
    "name": "eks_deploy",
    "arguments": {
      "cluster": "jumpserver-sg",
      "env": "test",
      "command": "kubectl -n scloud-common-test rollout status deploy/ward-api",
      "timeout_seconds": 300
    }
  }
}
```

## 返回格式

所有工具调用都会返回标准 MCP `tools/call` 结果。

成功响应外层：

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "content": [
      {
        "type": "text",
        "text": "{...JSON string...}"
      }
    ],
    "isError": false
  }
}
```

对 `eks_query` 和 `eks_deploy`，`content[0].text` 需要再按 JSON 解析为 `CommandResult`：

```json
{
  "cluster": "jumpserver-sg",
  "env": "dev",
  "pool": "query",
  "command": "kubectl -n scloud-common-dev get pods -o wide",
  "output": "NAME ...",
  "exit_code": 0,
  "duration_ms": 1234,
  "truncated": false
}
```

字段说明：

| 字段 | 说明 |
| --- | --- |
| `cluster` | 实际执行的逻辑集群 |
| `env` | 实际执行的环境 |
| `pool` | `query` 或 `deploy` |
| `command` | 实际执行命令 |
| `output` | 命令 stdout/stderr 合并输出 |
| `exit_code` | shell 命令退出码；`0` 表示成功 |
| `duration_ms` | 执行耗时，毫秒 |
| `truncated` | 输出是否被服务端截断 |

如果命令执行失败但已有结果，`isError=true`，`content[0].text` 解析后通常是：

```json
{
  "error": "context deadline exceeded",
  "result": {
    "cluster": "jumpserver-sg",
    "env": "dev",
    "pool": "query",
    "command": "...",
    "output": "...",
    "exit_code": -1,
    "duration_ms": 45000,
    "truncated": false
  }
}
```

如果错误发生在执行前，例如参数缺失、未知集群、未知环境、连接失败，则 `content[0].text` 可能是普通错误字符串。

## AI 调用建议

1. 优先调用 `eks_list_clusters` 确认可用 `cluster` 和 `env`。
2. 查询状态、日志、事件时使用 `eks_query`。
3. 只有用户明确要求部署、回滚、apply、rollout restart 等变更操作时，才使用 `eks_deploy`。
4. 命令必须是单行；不要发送包含换行的脚本。
5. 对可能输出很多内容的命令使用过滤条件，例如 namespace、label selector、`--tail`。
6. 如果 `truncated=true`，不要假设输出完整；应缩小查询范围后重试。
7. 如果 `isError=true`，先解析 `content[0].text`；如果其中包含 `result.output`，优先把该输出作为诊断依据。
8. 不要向用户承诺变更成功，除非 `exit_code=0` 且输出内容支持该结论。

## 当前常用 namespace 示例

| 环境 | namespace 示例 |
| --- | --- |
| `dev` | `scloud-common-dev` |
| `test` | `scloud-common-test` |

查询 pods：

```json
{
  "cluster": "jumpserver-sg",
  "env": "dev",
  "command": "kubectl -n scloud-common-dev get pods -o wide",
  "timeout_seconds": 120
}
```

查询事件：

```json
{
  "cluster": "jumpserver-sg",
  "env": "test",
  "command": "kubectl -n scloud-common-test get events --sort-by=.lastTimestamp",
  "timeout_seconds": 120
}
```

查询指定 deployment：

```json
{
  "cluster": "jumpserver-sg",
  "env": "test",
  "command": "kubectl -n scloud-common-test get deploy ward-api -o wide",
  "timeout_seconds": 120
}
```

## 约束和风险

- 服务端不维护命令白名单。
- `eks_query` 和 `eks_deploy` 都会执行传入的单行 shell 命令。
- AI 必须自行判断命令是否只读或有副作用。
- `kubectl delete`、`kubectl apply`、`helm upgrade`、`helm rollback`、`kubectl rollout restart` 等命令可能改变线上状态。
- 当前服务通过 JumpServer 连接 EKS 运维资产机；连接失败时，错误通常会出现在 `content[0].text` 中。
