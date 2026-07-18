package rules

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GitUpdateResult 从 Git 更新游戏规则库的结果。
type GitUpdateResult struct {
	Branch string   `json:"branch"`          // 实际拉取的分支（master/main）
	URL    string   `json:"url"`             // 实际使用的下载地址
	Count  int      `json:"count"`           // 成功写入的 .rules 文件数
	Files  []string `json:"files"`           // 写入的文件名（去 rules/ 前缀）
	Errors []string `json:"errors,omitempty"` // 处理过程中的非致命错误
}

// sstapRepo 游戏规则上游仓库（社区维护的 SSTap-Rule）。
const sstapRepo = "FQrabbit/SSTap-Rule"

// UpdateFromGit 从 GitHub 拉取最新 SSTap-Rule 的 rules/ 目录，
// 解压其中的 *.rules 文件写入 targetDir，返回更新结果。
// 适用于 Web 面板「从 Git 更新」按钮：一键把社区最新游戏 IP 库同步到路由器。
func UpdateFromGit(targetDir string) (*GitUpdateResult, error) {
	if targetDir == "" {
		return nil, fmt.Errorf("目标目录为空")
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建目录失败: %w", err)
	}

	// 优先 master，回退 main（不同 fork/时期默认分支可能不同）
	var (
		zipBytes []byte
		branch   string
		usedURL  string
		dlErr    error
	)
	for _, b := range []string{"master", "main"} {
		url := fmt.Sprintf("https://github.com/%s/archive/refs/heads/%s.zip", sstapRepo, b)
		data, err := downloadBytes(url, 180*time.Second)
		if err == nil && len(data) > 0 {
			zipBytes = data
			branch = b
			usedURL = url
			dlErr = nil
			break
		}
		dlErr = err
	}
	if zipBytes == nil {
		return nil, fmt.Errorf("下载 SSTap-Rule 仓库失败: %v", dlErr)
	}

	return extractRulesFromZip(zipBytes, targetDir, branch, usedURL)
}

// extractRulesFromZip 从归档字节中提取 rules/*.rules 写入 dir。
// 抽离为独立函数便于离线单元测试（用合成 zip 验证路径/过滤逻辑）。
func extractRulesFromZip(data []byte, dir, branch, url string) (*GitUpdateResult, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("解压失败: %w", err)
	}

	res := &GitUpdateResult{Branch: branch, URL: url}
	const rulesPrefix = "rules/"

	for _, f := range zr.File {
		name := f.Name
		idx := strings.Index(name, rulesPrefix)
		if idx < 0 {
			continue // 不在 rules/ 下，跳过
		}
		rel := name[idx+len(rulesPrefix):]
		// 仅接受 rules/ 直接子级的 .rules 文件，忽略子目录与无关文件
		if rel == "" || strings.Contains(rel, "/") || !strings.HasSuffix(rel, ".rules") {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("打开 %s 失败: %v", rel, err))
			continue
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("读取 %s 失败: %v", rel, err))
			continue
		}

		dst := filepath.Join(dir, rel)
		if err := os.WriteFile(dst, content, 0o644); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("写入 %s 失败: %v", rel, err))
			continue
		}
		res.Files = append(res.Files, rel)
	}

	res.Count = len(res.Files)
	log.Printf("从 Git 更新游戏规则库: 分支 %s, 共 %d 个 .rules 文件 -> %s", branch, res.Count, dir)
	return res, nil
}

// downloadBytes 带超时下载并返回完整响应体。
func downloadBytes(url string, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
