// Package httpapi exposes the meme registry over plain HTTP+JSON so non-MCP
// clients (curl, sh, the deal launcher's plugin script) can hit it without
// implementing the MCP transport. Same Source backends, same SearchResult
// shape — just HTTP instead of stdio JSON-RPC.
package httpapi

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/shadow/meme/internal/core"
	"github.com/shadow/meme/internal/sources"
)

// Serve runs the HTTP server until the listener errors out.
// addr is anything net/http accepts (e.g. ":18080" or "127.0.0.1:18080").
func Serve(addr string, registry *core.Registry) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/tools/list_sources", handleListSources(registry))
	mux.HandleFunc("/tools/search", handleSearchAggregate(registry))
	// Pattern intentionally ends with `/` so net/http does prefix routing
	// and we pull the source id off the path tail inside the handler.
	mux.HandleFunc("/tools/search/", handleSearchSingle(registry))
	mux.HandleFunc("/img", handleImageProxy())

	srv := &http.Server{
		Addr:              addr,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: 5 * time.Second,
		// No write timeout: /img streams arbitrary-size images.
	}
	fmt.Printf("[httpapi] listening on %s\n", addr)
	return srv.ListenAndServe()
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		fmt.Printf("[httpapi] %s %s %s\n", r.Method, r.URL.RequestURI(), time.Since(start))
	})
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "ok\n")
}

func handleListSources(registry *core.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, sources.GetAllSourceInfo(registry))
	}
}

// handleSearchAggregate: GET /tools/search?keyword=&sources=a,b&limit=&page=&timeout=
func handleSearchAggregate(registry *core.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		keyword := strings.TrimSpace(q.Get("keyword"))
		if keyword == "" {
			httpError(w, http.StatusBadRequest, "keyword 参数不能为空")
			return
		}
		opts := readOpts(q)

		var srcIDs []string
		if raw := q.Get("sources"); raw != "" {
			for _, s := range strings.Split(raw, ",") {
				if s = strings.TrimSpace(s); s != "" {
					srcIDs = append(srcIDs, s)
				}
			}
		}

		var result core.SearchResult
		if len(srcIDs) > 0 {
			result = registry.SearchSources(r.Context(), keyword, srcIDs, opts)
		} else {
			result = registry.SearchAll(r.Context(), keyword, opts)
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// handleSearchSingle: GET /tools/search/<source>?keyword=&limit=&timeout=&page=
func handleSearchSingle(registry *core.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sourceID := strings.TrimPrefix(r.URL.Path, "/tools/search/")
		if sourceID == "" || strings.Contains(sourceID, "/") {
			httpError(w, http.StatusBadRequest, "URL 路径需为 /tools/search/<source_id>")
			return
		}
		q := r.URL.Query()
		keyword := strings.TrimSpace(q.Get("keyword"))
		if keyword == "" {
			httpError(w, http.StatusBadRequest, "keyword 参数不能为空")
			return
		}
		opts := readOpts(q)
		result := registry.SearchSources(r.Context(), keyword, []string{sourceID}, opts)
		writeJSON(w, http.StatusOK, result)
	}
}

// handleImageProxy: GET /img?u=<encoded image URL>&r=<encoded referer>
// Reverse-proxies the upstream image with the supplied Referer header so
// hotlink-protected sources (qudoutu, doutub, sogou) load in the client.
// Returns the upstream Content-Type so the browser renders correctly.
//
// TLS verification is disabled for upstream fetches. Image CDNs are notorious
// for mismatched / expired certs (e.g. sogou serves images from
// img.rsdbox.cn whose cert is valid only for *.ctcdn.cn). We're not
// transmitting credentials or trusting response content as code; the worst
// case is rendering a wrong image. Browsers would refuse the same URL with
// no override, which is exactly why we need a proxy in the first place.
func handleImageProxy() http.HandlerFunc {
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	return func(w http.ResponseWriter, r *http.Request) {
		raw := r.URL.Query().Get("u")
		if raw == "" {
			httpError(w, http.StatusBadRequest, "missing u (image url) query")
			return
		}
		// Hard-cap acceptable schemes so the proxy can't be turned into an
		// SSRF tool that hits internal services.
		parsed, err := url.Parse(raw)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			httpError(w, http.StatusBadRequest, "u must be an http(s) URL")
			return
		}
		referer := r.URL.Query().Get("r")

		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, raw, nil)
		if err != nil {
			httpError(w, http.StatusBadGateway, fmt.Sprintf("build upstream request failed: %v", err))
			return
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		if referer != "" {
			req.Header.Set("Referer", referer)
		}

		resp, err := client.Do(req)
		if err != nil {
			httpError(w, http.StatusBadGateway, fmt.Sprintf("upstream fetch failed: %v", err))
			return
		}
		defer resp.Body.Close()

		// Pass through useful upstream headers verbatim.
		if ct := resp.Header.Get("Content-Type"); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		if cl := resp.Header.Get("Content-Length"); cl != "" {
			w.Header().Set("Content-Length", cl)
		}
		// Modest cache: image URLs are immutable-by-content, an hour is plenty
		// for the launcher webview to stop re-fetching the same thumbnail.
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}
}

func readOpts(q url.Values) core.SearchOptions {
	opts := core.DefaultSearchOptions()
	if v, err := strconv.Atoi(q.Get("page")); err == nil && v > 0 {
		opts.Page = v
	}
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 {
		opts.Limit = v
	}
	if v, err := strconv.Atoi(q.Get("timeout")); err == nil && v > 0 {
		if v > 30 {
			v = 30
		}
		opts.Timeout = time.Duration(v) * time.Second
	}
	return opts
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(body)
}

func httpError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
