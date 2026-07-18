package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed index.html assets/*.svg
var content embed.FS

// Handler 返回 Web 界面的 HTTP 处理器
func Handler() http.Handler {
	// 从 embed.FS 获取文件系统
	fsys, err := fs.Sub(content, ".")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(fsys))
}

// IndexHTML 返回 index.html 的内容
func IndexHTML() ([]byte, error) {
	return content.ReadFile("index.html")
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
