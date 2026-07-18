package dpi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// adguardQuery 对应 AdGuard Home /control/querylog 的单条记录。
// answer.value 在不同版本中可能是字符串或字符串数组，故用 interface{} 兼容。
type adguardQuery struct {
	Question struct {
		Host string `json:"host"`
	} `json:"question"`
	Answer struct {
		Value interface{} `json:"value"`
	} `json:"answer"`
	Client string `json:"client"`
}

type adguardResponse struct {
	Data []adguardQuery `json:"data"`
}

// dnsRecord 是一条「客户端 IP 查询了某域名、解析到某 IP」的关联记录。
type dnsRecord struct {
	client string
	domain string
	ip     string
}

// fetchAdGuardQueryLog 拉取 AdGuard Home 查询日志，返回 (client, domain, ip) 关联记录。
// 鉴权优先级：静态 token > 用户名密码登录获取 session token。
func fetchAdGuardQueryLog(ctx context.Context, cfg DPIConfig) ([]dnsRecord, error) {
	base := strings.TrimRight(cfg.DNS.AdGuardURL, "/")
	if base == "" {
		return nil, fmt.Errorf("adguard_url 未配置")
	}

	token := cfg.DNS.AdGuardToken
	if token == "" && cfg.DNS.AdGuardUser != "" {
		t, err := adguardLogin(ctx, base, cfg.DNS.AdGuardUser, cfg.DNS.AdGuardPass)
		if err != nil {
			return nil, err
		}
		token = t
	}
	if token == "" {
		return nil, fmt.Errorf("adguard 需要 token 或 username/password")
	}

	url := fmt.Sprintf("%s/control/querylog?token=%s&limit=500", base, token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("adguard 返回 %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var parsed adguardResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}

	var records []dnsRecord
	for _, q := range parsed.Data {
		domain := q.Question.Host
		if domain == "" || q.Client == "" {
			continue
		}
		for _, ip := range normalizeAnswerValue(q.Answer.Value) {
			records = append(records, dnsRecord{client: q.Client, domain: domain, ip: ip})
		}
	}
	return records, nil
}

// normalizeAnswerValue 把 AdGuard 的 answer.value（字符串或数组）统一成字符串切片。
func normalizeAnswerValue(v interface{}) []string {
	switch t := v.(type) {
	case string:
		if t != "" {
			return []string{t}
		}
	case []interface{}:
		var out []string
		for _, e := range t {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return t
	}
	return nil
}

// adguardLogin 用用户名密码换取 session token。
func adguardLogin(ctx context.Context, base, user, pass string) (string, error) {
	payload, _ := json.Marshal(map[string]string{"name": user, "password": pass})
	url := base + "/control/login"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("adguard 登录失败 %d: %s", resp.StatusCode, string(body))
	}

	var r struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", err
	}
	if r.Token == "" {
		return "", fmt.Errorf("adguard 登录未返回 token")
	}
	return r.Token, nil
}
