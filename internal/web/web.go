package web

import (
	"embed"
	"strings"
	"sync"
)

//go:embed index.html assets/*.svg
var content embed.FS

// IndexHTML 返回 index.html 的内容（启动时缓存一次，避免每次请求重复读取）
var (
	indexHTMLOnce sync.Once
	indexHTMLData []byte
	indexHTMLErr  error
)

func IndexHTML() ([]byte, error) {
	indexHTMLOnce.Do(func() {
		indexHTMLData, indexHTMLErr = content.ReadFile("index.html")
	})
	return indexHTMLData, indexHTMLErr
}

// GetISPSVG 返回运营商 SVG 内容，联通的改为红色
func GetISPSVG(isp string) string {
	filename := ""
	switch isp {
	case "电信":
		filename = "assets/电信.svg"
	case "联通":
		filename = "assets/联通.svg"
	case "移动":
		filename = "assets/移动.svg"
	default:
		return ""
	}

	data, err := content.ReadFile(filename)
	if err != nil {
		return ""
	}

	svg := string(data)

	// 联通的 SVG 是黑色的，改成红色
	if isp == "联通" {
		svg = strings.ReplaceAll(svg, "rgb(0,0,0)", "#cc0000")
	}

	return svg
}
