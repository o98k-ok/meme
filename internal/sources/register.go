package sources

import (
	"os"

	"github.com/shadow/meme/internal/core"
)

// RegisterAllSources 注册所有内置源到注册中心。
//
// qudoutu 和 doutub 强制开启了 Referer 防盗链，必须由 server 端做图片代理
// 才能在客户端正常显示。这里有两种"有代理能力"的判定：
//   - config.ImageProxyURL 非空：外部代理模板（老路径，CLI 主要用）
//   - MEME_PUBLIC_URL 非空：HTTP 模式下，server 自带 /img 反代（新路径）
//
// 任一满足就注册这两个源，否则跳过它们以免返回 404 图。
func RegisterAllSources(registry *core.Registry, config *Config) {
	// 注册无需认证的源
	registry.Register(NewDoutula())
	registry.Register(NewPdan())
	registry.Register(NewSougou())

	if config != nil {
		hasProxy := config.ImageProxyURL != "" || os.Getenv("MEME_PUBLIC_URL") != ""
		if hasProxy {
			registry.Register(NewQudoutu())
			registry.Register(NewDoutub())
		}

		// 注册需要认证的源 (如果配置了 Cookie)
		if config.DouyinCookie != "" {
			registry.Register(NewDouyin(config.DouyinCookie))
		}
	}
}

// Config 源配置
type Config struct {
	DouyinCookie  string `json:"douyin_cookie" yaml:"douyin_cookie"`
	ImageProxyURL string `json:"image_proxy_url" yaml:"image_proxy_url"`
}

// GetAllSourceInfo 获取所有源的信息 (用于 list_sources Tool)
func GetAllSourceInfo(registry *core.Registry) []SourceInfo {
	sources := registry.List()
	infos := make([]SourceInfo, 0, len(sources))

	for _, s := range sources {
		infos = append(infos, SourceInfo{
			ID:           s.ID(),
			Name:         s.Name(),
			Description:  s.Description(),
			RequiresAuth: s.RequiresAuth(),
		})
	}

	return infos
}

// SourceInfo 源信息结构
type SourceInfo struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	RequiresAuth bool   `json:"requires_auth"`
}
