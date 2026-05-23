# Meme MCP Server

一个基于 Model Context Protocol (MCP) 的高性能表情包搜索服务。

它能够聚合多个表情包源的搜索结果，并通过 MCP 协议直接为 Claude Desktop、Cursor 等 AI 客户端提供服务。

## ✨ 特性

- 🚀 **高性能**: Go 语言实现，并发搜索多个数据源。
- 🔌 **MCP 协议**: 完美支持 Model Context Protocol，可无缝集成到 AI 工作流中。
- 📦 **多源聚合**: 支持 6 个主流表情包源。
- 🛡️ **防盗链支持**: 内置图片代理机制，解决部分源（如趣斗图）的图片 404/防盗链问题。
- 🎯 **智能去重**: 自动识别并去除重复图片。
- ⚡ **即插即用**: 单二进制文件，部署简单。

## 📚 支持的数据源

| ID | 名称 | 说明 | 配置要求 |
|:---|:-----|:-----|:---------|
| `doutula` | 斗图啦 | doutupk.com | 无 |
| `pdan` | 胖哒 | pdan.com.cn | 无 |
| `sougou` | 搜狗表情 | pic.sogou.com | 无 |
| `qudoutu` | 趣斗图 | qudoutu.cn | 需 server 端能反代图片（HTTP 模式自动满足） |
| `doutub` | 表情包API | api.doutub.com | 同上 |
| `douyin` | 抖音 | douyin.com | 🔐 需配置 `DOUYIN_COOKIE` |

> qudoutu / doutub 的图片 CDN 检查 Referer 才放图。**HTTP 模式下 server 自带 `/img` 反代**，自动用对的 Referer 帮客户端拉图，使用者无需配代理 URL。

## 🛠️ 配置

### HTTP 模式（推荐）

只需要一个环境变量：

```bash
# clients 看到的 server 地址（用于把 qudoutu/doutub 图片 URL 改写到 /img）。
# 改成你部署机的实际地址（私网 IP、域名、或本机 loopback 测试时填 127.0.0.1）。
export MEME_PUBLIC_URL="http://<host>:18080"
./build/meme-server --http :18080
```

带上 `MEME_PUBLIC_URL` 后，qudoutu 和 doutub 这两个需要反代的源会自动注册，且返回的图片 URL 形如 `<MEME_PUBLIC_URL>/img?u=...&r=...`，客户端当成普通图片地址用即可。docker-compose.yaml 已默认这样配。

### 抖音 Cookie（可选）

抖音源需要登录态。在浏览器登录 douyin.com 后从 DevTools 复制整段 cookie：

```bash
export DOUYIN_COOKIE="..."
```

### `IMAGE_PROXY_URL`（CLI 用户兜底）

只有在跑老的 stdio CLI / 单文件二进制、又想要 qudoutu/doutub 源、又没法暴露 HTTP 服务时才需要。模板支持 `{URL}` / `{SOURCE_URL}` / `{REFERER}` 占位符。HTTP 模式下应优先用 `MEME_PUBLIC_URL`。

## 🚀 快速开始

### 二进制构建（Go server）

```bash
make build           # 出 build/meme-server (HTTP / stdio MCP 二合一)
make build-cli       # 出 build/meme-cli (本地搜索测试用)
```

### CLI 测试

```bash
./build/meme-cli -k "猫" -l 5         # 基本搜索
./build/meme-cli -list                # 列出当前已加载源
./build/meme-cli -k "狗" -s pdan -l 3 # 限定源
```

### 启动 Server

```bash
# HTTP 模式（推荐）
MEME_PUBLIC_URL="http://127.0.0.1:18080" ./build/meme-server --http :18080

# 或者老的 stdio MCP 模式
./build/meme-server
```

### Docker 一键起

```bash
sudo docker compose up -d --build
```

`docker-compose.yaml` 定义两个 service，默认都绑在 `127.0.0.1` loopback 上（避免无心暴露公网）。需要让别的设备访问时把 `ports:` 改成对应私网 IP（Tailscale / WireGuard / LAN 任选）：

| Service | 端口 | 镜像 | 干嘛 |
| --- | --- | --- | --- |
| `meme` | `:18080` | 本仓库 Dockerfile 编译 | Go HTTP API（搜索 + `/img` 反代）。设置 `MEME_PUBLIC_URL` 让 server 把 qudoutu/doutub 的图片 URL 改写到自家 `/img` |
| `npm-dist` | `:18081` | `caddy:2-alpine` | 静态托管 `./dist-static/*.tgz`。客户端 `npx -y http://<host>:18081/meme-mcp-VERSION.tgz` 拉的就是这里 |

⚠️ **/img 反代是无鉴权的图片代理**，绑到公网前自己掂量；本仓库默认 loopback 就是这个原因。改 IP / 端口都在 `docker-compose.yaml` 顶部 `ports:` 里改一处即可。

`/img` 反代对上游 TLS 故意宽容（`InsecureSkipVerify`）——图片 CDN 经常证书域名不匹配（比如 sogou 的图实际在 `img.rsdbox.cn`，证书是 `*.ctcdn.cn`），且响应是图片字节不是凭据，不引入额外信任面。

## 🤖 AI 客户端集成

### 推荐：`npx` 拉远端 tarball

最省事的方式——不用本地编译、不用 Go 环境。客户端只要有 Node 18+，就能直接：

```jsonc
// Claude Desktop: ~/Library/Application Support/Claude/claude_desktop_config.json
// Replace <host> with the address where you've published the npm-dist tarball
// and the Go backend (they may or may not be the same host / port).
{
  "mcpServers": {
    "meme": {
      "command": "npx",
      "args": ["-y", "http://<host>:18081/meme-mcp-0.1.0.tgz"],
      "env": {
        "MEME_API_BASE": "http://<host>:18080"
      }
    }
  }
}
```

`meme-mcp` 是一个 stdio MCP forwarder（[mcp/](./mcp/)），内部把每个 tool 调用转给 `MEME_API_BASE` 的 HTTP 接口。所有 6 个 tool（`search_meme`, `search_doutula`, `search_pdan`, …）自动从远端发现并暴露。

Cursor 配置同形态：Type=stdio，Command=`npx`，Args=`-y http://<host>:18081/meme-mcp-0.1.0.tgz`，Env 加 `MEME_API_BASE`。

### 备选：本地 stdio 二进制

如果你本机就跑着 Go server，或者完全离线测试：

```jsonc
{
  "mcpServers": {
    "meme": {
      "command": "/absolute/path/to/meme-server",
      "env": {
        "MEME_PUBLIC_URL": "http://127.0.0.1:18080",
        "DOUYIN_COOKIE": ""
      }
    }
  }
}
```

这种模式下需要另外开一个 HTTP server 给 `/img` 用（或者写 `IMAGE_PROXY_URL` 把图片代理外包出去），所以多数场景还是首选上面的 npx 路径。

### 发布新版 `meme-mcp`

[`mcp/`](./mcp/) 是一个独立的 TypeScript Node 项目，构建出 `dist/index.js`（带 shebang，可执行），打成 `.tgz` 后由 `npm-dist` service 暴露给客户端。完整发版流程：

```bash
cd mcp

# 1. 改 src/index.ts 后 bump 版本（patch / minor / major）
npm version patch

# 2. build + pack 到 ../dist-static/
npm run pack:dist
# 产物: ../dist-static/meme-mcp-0.1.1.tgz

# 3. rsync 到部署目标（替换 <user>/<host>/<path> 成你的实际值）
rsync -avz ../dist-static/ <user>@<host>:/path/to/repo/dist-static/

# 4. 客户端配置里把 URL 的版本号同步成新值，例如:
#    "args": ["-y", "http://<host>:18081/meme-mcp-0.1.1.tgz"]
```

`caddy` service 是只读 mount，不需要重启 docker compose。版本号在 URL 路径里，所以客户端 `npx` 缓存按 URL 命中——升级 = 改一行 URL。

## 📦 MCP Tools

### `search_meme`
聚合搜索（并发请求所有源后去重）。

- `keyword` (string, required): 搜索关键词
- `sources` (array, optional): 指定搜索源 ID 列表
- `page` (number, optional, default 1)
- `limit` (number, optional, default 20)
- `timeout` (number, optional, default 10): 单源超时（秒），上限 30。受限客户端（Raycast / deal）建议压到 2-3。

> ⚠️ 注意：聚合接口仍会等待所有被命中的源全部结束才回复，所以**整体响应受最慢源影响**。如果客户端有严格的总耗时上限，请优先使用下面的"单源工具"。

### `search_<source_id>`
为每个数据源单独注册一个 tool（`search_doutula`, `search_pdan`, `search_sougou`, `search_qudoutu`, `search_doutub`, `search_douyin`）。只查一个源，不会被慢源拖累。

- `keyword` (string, required)
- `page` (number, optional, default 1)
- `limit` (number, optional, default 20)
- `timeout` (number, optional, default 10): 上限 30 秒

### `list_sources`
列出当前已加载并可用的数据源。

## 🌐 HTTP 模式

除了 stdio MCP 协议外，server 还能以 HTTP REST 模式启动，方便不能直接走 stdio 的客户端（shell 脚本、deal launcher 的 plugin 等）调用：

```bash
# CLI flag
./build/meme-server --http :18080

# 或环境变量
MEME_HTTP_LISTEN=:18080 ./build/meme-server
```

启动后暴露的 endpoint：

| 路径 | 说明 |
| --- | --- |
| `GET /healthz` | 健康检查，返回 `ok\n` |
| `GET /tools/list_sources` | JSON 列出所有源 |
| `GET /tools/search?keyword=&sources=a,b&limit=&page=&timeout=` | 聚合搜索（同 `search_meme`） |
| `GET /tools/search/<source_id>?keyword=&limit=&timeout=&page=` | 单源搜索 |
| `GET /img?u=<encoded url>&r=<encoded referer>` | 图片反代：用指定 Referer 拉上游图，再把字节流式回给客户端，绕过 qudoutu / doutub 的防盗链 |

### 配合 `MEME_PUBLIC_URL`

HTTP 模式下建议设置 `MEME_PUBLIC_URL` 为该 server 的对外可达地址（例如 `http://<host>:18080` 或本机 `http://127.0.0.1:18080`）。设置后，需要代理的源（qudoutu / doutub）返回的图片 URL 会被改写成 `<MEME_PUBLIC_URL>/img?u=...&r=...`，客户端直接当普通图片 URL 用即可，无需再配第三方代理（即不需要老的 `IMAGE_PROXY_URL`）。

如果两个都设了，`MEME_PUBLIC_URL` 优先。

## 🐳 Docker 部署

仓库根目录提供 `Dockerfile` + `docker-compose.yaml`，默认两个 service 都绑在 `127.0.0.1` loopback 上。**对外暴露前请按需要在 `ports:` 段改成你的私网 IP（Tailscale / WireGuard / LAN）**——`/img` 反代是无鉴权的，不建议直接绑公网。

```bash
# 在目标机器上（已装 docker + compose v2）
git pull   # 或 rsync 过来
sudo docker compose up -d --build

# 验证（默认 loopback；如你改了 ports 绑定就把 127.0.0.1 换成对应 IP）
curl http://127.0.0.1:18080/healthz
curl 'http://127.0.0.1:18080/tools/search/doutula?keyword=猫&limit=5&timeout=3'
```

镜像内置 alpine 的 ca-certificates，可以直接 HTTPS 出口拉源站。镜像通过 `goproxy.cn` 拉 Go 依赖，中国大陆服务器能正常构建。

## 📄 License

MIT
