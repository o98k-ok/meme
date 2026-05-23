package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/server"
	"github.com/shadow/meme/internal/core"
	"github.com/shadow/meme/internal/httpapi"
	"github.com/shadow/meme/internal/sources"
	"github.com/shadow/meme/internal/tools"
)

func main() {
	// --http :18080 → expose tools as HTTP REST (for clients that can't speak
	// stdio MCP, e.g. shell scripts behind the deal launcher's plugin
	// runtime). Empty (default) keeps the original stdio MCP transport.
	// Env override MEME_HTTP_LISTEN wins if the flag is left blank.
	httpAddr := flag.String("http", "", "listen on <addr> as an HTTP REST server (e.g. :18080); blank = stdio MCP")
	flag.Parse()
	if *httpAddr == "" {
		*httpAddr = os.Getenv("MEME_HTTP_LISTEN")
	}

	registry := core.NewRegistry()
	config := &sources.Config{
		DouyinCookie:  os.Getenv("DOUYIN_COOKIE"),
		ImageProxyURL: os.Getenv("IMAGE_PROXY_URL"),
	}
	sources.RegisterAllSources(registry, config)

	if *httpAddr != "" {
		if err := httpapi.Serve(*httpAddr, registry); err != nil {
			fmt.Fprintf(os.Stderr, "http server error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Stdio MCP path (unchanged transport, just the new tool set).
	s := server.NewMCPServer(
		"meme-server",
		"1.1.0",
		server.WithToolCapabilities(true),
	)
	tools.RegisterAll(s, registry)
	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "stdio server error: %v\n", err)
		os.Exit(1)
	}
}
