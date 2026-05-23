package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/shadow/meme/internal/core"
	"github.com/shadow/meme/internal/sources"
)

// SearchMemeArgs search_meme 工具的参数（聚合搜索）
type SearchMemeArgs struct {
	Keyword string   `json:"keyword"`
	Sources []string `json:"sources,omitempty"`
	Page    int      `json:"page,omitempty"`
	Limit   int      `json:"limit,omitempty"`
	// Timeout 单源超时（秒）。0 = 走 core 默认 10s。建议在 deal/Raycast
	// 这类有自身硬上限的客户端把 timeout 压到 2-3 秒。
	Timeout int `json:"timeout,omitempty"`
}

// SearchSourceArgs 单源 search_<sourceID> 工具的参数
type SearchSourceArgs struct {
	Keyword string `json:"keyword"`
	Page    int    `json:"page,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	Timeout int    `json:"timeout,omitempty"`
}

// optsFrom 把通用参数变成 SearchOptions，应用默认值 + clamp。
func optsFrom(page, limit, timeoutSec int) core.SearchOptions {
	opts := core.DefaultSearchOptions()
	if page > 0 {
		opts.Page = page
	}
	if limit > 0 {
		opts.Limit = limit
	}
	if timeoutSec > 0 {
		// 上限 30s，下限 1s，避免极端配置卡死或超时太短直接没结果。
		if timeoutSec > 30 {
			timeoutSec = 30
		}
		opts.Timeout = time.Duration(timeoutSec) * time.Second
	}
	return opts
}

// encodeResult 序列化 SearchResult 为 MCP 文本结果（禁用 HTML 转义）。
func encodeResult(result core.SearchResult) (*mcp.CallToolResult, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("结果序列化失败: %v", err)), nil
	}
	return mcp.NewToolResultText(buf.String()), nil
}

// NewSearchMemeTool 创建 search_meme MCP Tool（聚合搜索）
func NewSearchMemeTool() mcp.Tool {
	return mcp.NewTool(
		"search_meme",
		mcp.WithDescription("聚合搜索表情包，并发请求多个源后去重返回。"),
		mcp.WithString("keyword",
			mcp.Required(),
			mcp.Description("搜索关键词，如：猫、狗、开心、难过等"),
		),
		mcp.WithArray("sources",
			mcp.Description("可选，指定搜索的源ID列表。不指定则搜索所有源。可用源：qudoutu, doutula, pdan, sougou, douyin, doutub"),
		),
		mcp.WithNumber("page", mcp.Description("页码，默认为 1")),
		mcp.WithNumber("limit", mcp.Description("每个源返回的最大数量，默认为 20")),
		mcp.WithNumber("timeout", mcp.Description("单源超时（秒），默认 10。建议受限客户端压到 2-3。")),
	)
}

// HandleSearchMeme 处理 search_meme 请求
func HandleSearchMeme(registry *core.Registry) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		fmt.Fprintf(os.Stderr, "[SearchMeme] Request received. Params: %+v\n", request.Params.Arguments)

		var args SearchMemeArgs
		argsBytes, err := json.Marshal(request.Params.Arguments)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("参数解析失败: %v", err)), nil
		}
		if err := json.Unmarshal(argsBytes, &args); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("参数解析失败: %v", err)), nil
		}
		if args.Keyword == "" {
			return mcp.NewToolResultError("keyword 参数不能为空"), nil
		}

		opts := optsFrom(args.Page, args.Limit, args.Timeout)
		fmt.Fprintf(os.Stderr, "[SearchMeme] keyword=%s sources=%v page=%d limit=%d timeout=%v\n",
			args.Keyword, args.Sources, opts.Page, opts.Limit, opts.Timeout)

		var result core.SearchResult
		if len(args.Sources) > 0 {
			result = registry.SearchSources(ctx, args.Keyword, args.Sources, opts)
		} else {
			result = registry.SearchAll(ctx, args.Keyword, opts)
		}
		fmt.Fprintf(os.Stderr, "[SearchMeme] done items=%d duration=%dms\n", len(result.Memes), result.DurationMs)

		return encodeResult(result)
	}
}

// NewSearchSourceTool 为某个具体源创建独立 MCP tool（search_<id>）。
// 这样客户端能跳过聚合层、只查一个源，避免被慢源拖累整体响应时间。
func NewSearchSourceTool(id, name, desc string) mcp.Tool {
	return mcp.NewTool(
		"search_"+id,
		mcp.WithDescription(fmt.Sprintf("仅从 %s（%s）搜索表情包。%s", name, id, desc)),
		mcp.WithString("keyword",
			mcp.Required(),
			mcp.Description("搜索关键词，如：猫、狗、开心、难过等"),
		),
		mcp.WithNumber("page", mcp.Description("页码，默认为 1")),
		mcp.WithNumber("limit", mcp.Description("返回数量，默认 20")),
		mcp.WithNumber("timeout", mcp.Description("超时（秒），默认 10")),
	)
}

// HandleSearchSource 处理单源搜索：直接走 registry.SearchSources 限定 1 个源。
func HandleSearchSource(registry *core.Registry, sourceID string) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args SearchSourceArgs
		argsBytes, err := json.Marshal(request.Params.Arguments)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("参数解析失败: %v", err)), nil
		}
		if err := json.Unmarshal(argsBytes, &args); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("参数解析失败: %v", err)), nil
		}
		if args.Keyword == "" {
			return mcp.NewToolResultError("keyword 参数不能为空"), nil
		}
		opts := optsFrom(args.Page, args.Limit, args.Timeout)
		fmt.Fprintf(os.Stderr, "[Search:%s] keyword=%s limit=%d timeout=%v\n", sourceID, args.Keyword, opts.Limit, opts.Timeout)

		result := registry.SearchSources(ctx, args.Keyword, []string{sourceID}, opts)
		fmt.Fprintf(os.Stderr, "[Search:%s] done items=%d duration=%dms\n", sourceID, len(result.Memes), result.DurationMs)
		return encodeResult(result)
	}
}

// NewListSourcesTool 创建 list_sources MCP Tool
func NewListSourcesTool() mcp.Tool {
	return mcp.NewTool(
		"list_sources",
		mcp.WithDescription("列出所有可用的表情包数据源及其信息"),
	)
}

// HandleListSources 处理 list_sources 请求
func HandleListSources(registry *core.Registry) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		infos := sources.GetAllSourceInfo(registry)
		resultJSON, err := json.MarshalIndent(infos, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("结果序列化失败: %v", err)), nil
		}
		return mcp.NewToolResultText(string(resultJSON)), nil
	}
}

// RegisterAll 把所有 tool（聚合 + 每源 + list）注册到给定 MCP server。
// HTTP 模式下 internal/httpapi 复用同一份注册逻辑。
func RegisterAll(s *server.MCPServer, registry *core.Registry) {
	s.AddTool(NewSearchMemeTool(), HandleSearchMeme(registry))
	s.AddTool(NewListSourcesTool(), HandleListSources(registry))
	for _, info := range sources.GetAllSourceInfo(registry) {
		s.AddTool(
			NewSearchSourceTool(info.ID, info.Name, info.Description),
			HandleSearchSource(registry, info.ID),
		)
	}
}
