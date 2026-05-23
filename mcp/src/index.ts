#!/usr/bin/env node
/**
 * meme-mcp — stdio MCP server that forwards tool calls to the meme HTTP API.
 *
 * The HTTP backend (Go server reachable at MEME_API_BASE, default
 * http://localhost:18080) does all the real work: source fan-out, dedup,
 * image proxying. This package exists so any MCP-aware client (Claude
 * Desktop, Cursor, Raycast's MCP integrations, the deal launcher's plugin
 * runtime) can hit a single stdio binary without re-implementing the HTTP
 * shape.
 *
 * Set MEME_API_BASE to wherever you've deployed the Go backend.
 *
 * Tools registered:
 *   - search_meme              (aggregate, mirrors the original Go MCP tool)
 *   - search_<source_id>       (one per discovered source)
 *   - list_sources
 *
 * The source list is fetched once at startup so the tools surfaced over
 * stdio always match what the backend can actually serve.
 */

import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
  type Tool,
} from "@modelcontextprotocol/sdk/types.js";

const DEFAULT_BASE = "http://localhost:18080";
const API_BASE = (process.env.MEME_API_BASE ?? DEFAULT_BASE).replace(/\/+$/, "");
const FETCH_TIMEOUT_MS = parseInt(process.env.MEME_FETCH_TIMEOUT_MS ?? "8000", 10);

interface SourceInfo {
  id: string;
  name: string;
  description: string;
  requires_auth: boolean;
}

function log(msg: string): void {
  // MCP stdio reserves stdout for JSON-RPC. Diagnostics go to stderr; the
  // hosting client surfaces them in its log pane.
  process.stderr.write(`[meme-mcp] ${msg}\n`);
}

async function httpGet<T>(path: string): Promise<T> {
  const url = API_BASE + path;
  const ctl = new AbortController();
  const timer = setTimeout(() => ctl.abort(), FETCH_TIMEOUT_MS);
  try {
    const resp = await fetch(url, { signal: ctl.signal });
    if (!resp.ok) {
      throw new Error(`upstream ${resp.status}: ${await resp.text()}`);
    }
    return (await resp.json()) as T;
  } finally {
    clearTimeout(timer);
  }
}

function asText(obj: unknown): { content: { type: "text"; text: string }[] } {
  return {
    content: [
      { type: "text", text: JSON.stringify(obj, null, 2) },
    ],
  };
}

function asError(message: string): {
  content: { type: "text"; text: string }[];
  isError: true;
} {
  return {
    content: [{ type: "text", text: message }],
    isError: true,
  };
}

/** Standard arg schema slice reused by both aggregate and per-source tools. */
const commonProps = {
  keyword: {
    type: "string",
    description: "搜索关键词，如：猫、狗、开心、难过等",
  },
  limit: {
    type: "number",
    description: "返回数量，默认 20",
  },
  timeout: {
    type: "number",
    description: "单源超时（秒），默认 10，上限 30",
  },
  page: {
    type: "number",
    description: "页码，默认 1",
  },
} as const;

function aggregateTool(): Tool {
  return {
    name: "search_meme",
    description:
      "聚合搜索表情包（并发请求所有可用源后去重）。受最慢源拖累；若客户端有总耗时上限，优先使用 search_<source_id>。",
    inputSchema: {
      type: "object",
      properties: {
        ...commonProps,
        sources: {
          type: "array",
          items: { type: "string" },
          description: "可选，指定搜索源ID列表。",
        },
      },
      required: ["keyword"],
    },
  };
}

function perSourceTool(info: SourceInfo): Tool {
  return {
    name: `search_${info.id}`,
    description: `仅从 ${info.name}（${info.id}）搜索表情包。${info.description}`,
    inputSchema: {
      type: "object",
      properties: commonProps,
      required: ["keyword"],
    },
  };
}

function listTool(): Tool {
  return {
    name: "list_sources",
    description: "列出后端当前已加载的所有表情包数据源。",
    inputSchema: { type: "object", properties: {} },
  };
}

/** Build the URL query string from MCP tool arguments. */
function buildQuery(args: Record<string, unknown>): string {
  const params = new URLSearchParams();
  const keyword = String(args.keyword ?? "").trim();
  if (!keyword) throw new Error("keyword 参数不能为空");
  params.set("keyword", keyword);
  if (typeof args.limit === "number") params.set("limit", String(args.limit));
  if (typeof args.page === "number") params.set("page", String(args.page));
  if (typeof args.timeout === "number") params.set("timeout", String(args.timeout));
  if (Array.isArray(args.sources) && args.sources.length > 0) {
    params.set("sources", args.sources.map(String).join(","));
  }
  return params.toString();
}

async function main(): Promise<void> {
  log(`forwarding to ${API_BASE} (timeout ${FETCH_TIMEOUT_MS}ms)`);

  // Discover available sources up front so the tools we advertise actually
  // resolve. A 5 s fail-fast: if the backend is unreachable we fall back to
  // the aggregate tool only and let individual calls surface the real error.
  let sources: SourceInfo[] = [];
  try {
    sources = await httpGet<SourceInfo[]>("/tools/list_sources");
    log(`discovered ${sources.length} sources: ${sources.map((s) => s.id).join(", ")}`);
  } catch (err) {
    log(`could not list sources at startup: ${(err as Error).message}`);
  }

  const server = new Server(
    { name: "meme-mcp", version: "0.1.0" },
    { capabilities: { tools: {} } },
  );

  const tools: Tool[] = [aggregateTool(), listTool(), ...sources.map(perSourceTool)];

  server.setRequestHandler(ListToolsRequestSchema, async () => ({ tools }));

  server.setRequestHandler(CallToolRequestSchema, async (req) => {
    const name = req.params.name;
    const args = (req.params.arguments ?? {}) as Record<string, unknown>;
    try {
      if (name === "list_sources") {
        const data = await httpGet<SourceInfo[]>("/tools/list_sources");
        return asText(data);
      }
      if (name === "search_meme") {
        const q = buildQuery(args);
        const data = await httpGet<unknown>(`/tools/search?${q}`);
        return asText(data);
      }
      const m = /^search_([a-zA-Z0-9_-]+)$/.exec(name);
      if (m) {
        const sourceId = m[1];
        const q = buildQuery(args);
        const data = await httpGet<unknown>(`/tools/search/${encodeURIComponent(sourceId)}?${q}`);
        return asText(data);
      }
      return asError(`unknown tool: ${name}`);
    } catch (err) {
      return asError(`tool ${name} failed: ${(err as Error).message}`);
    }
  });

  const transport = new StdioServerTransport();
  await server.connect(transport);
  log("ready (stdio)");
}

main().catch((err) => {
  log(`fatal: ${(err as Error).stack ?? err}`);
  process.exit(1);
});
