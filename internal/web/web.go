package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed index.html
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
