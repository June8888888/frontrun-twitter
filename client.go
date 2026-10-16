package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	http *http.Client
}

func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: RequestTimeout}}
}

type APIError struct {
	Status    int
	Code      string
	Message   string
	RateLimit bool
	NoCredit  bool
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("HTTP %d %s %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("HTTP %d %s", e.Status, e.Code)
}

func (c *Client) BatchProfiles(names []string) ([]map[string]any, error) {
	body, err := json.Marshal(map[string]any{"twitterUsernames": names})
	if err != nil {
		return nil, err
	}
	raw, err := c.request(http.MethodPost, BaseURL+"/api/v2/twitter/info/batch-query-light", body)
	if err != nil {
		return nil, err
	}
	return decodeList(raw)
}

func (c *Client) SmartFollowers(name, twitterID string) ([]map[string]any, int, bool, error) {
	q := url.Values{}
	q.Set("all", "true")
	if twitterID != "" {
		q.Set("twitterId", twitterID)
	}
	path := "/api/v1/twitter/" + url.PathEscape(name) + "/smart-followers"
	u := BaseURL + path
	if enc := q.Encode(); enc != "" {
		u += "?" + enc
	}
	raw, err := c.request(http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, false, err
	}
	return decodeSmart(raw)
}

func (c *Client) request(method, rawURL string, body []byte) (json.RawMessage, error) {
	var last error
	for attempt := 0; attempt <= RateLimitRetries; attempt++ {
		raw, err := c.once(method, rawURL, body)
		if err == nil {
			return raw, nil
		}
		last = err
		api, ok := err.(*APIError)
		if !ok || !api.RateLimit || attempt == RateLimitRetries {
			return nil, err
		}
		fmt.Printf("被限流，%s 后重试 %s\n", RateLimitWait, rawURL)
		time.Sleep(RateLimitWait)
	}
	return nil, last
}

func (c *Client) once(method, rawURL string, body []byte) (json.RawMessage, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, rawURL, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "zh-CN")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "PostmanRuntime-ApipostRuntime/1.1.0")
	req.Header.Set("X-Copilot-Client-Version", ClientVersion)
	req.Header.Set("X-Copilot-Client-Language", ClientLanguage)
	req.Header.Set("Cookie", cookieHeader())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	buf, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode >= 400 && looksRateLimited(buf)) {
		return nil, &APIError{Status: resp.StatusCode, Code: "429", Message: clip(buf), RateLimit: true}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		code, msg := errorFields(buf)
		api := &APIError{Status: resp.StatusCode, Code: code, Message: msg}
		if code == "CREDIT_LIMIT_EXCEEDED" {
			api.NoCredit = true
		}
		return nil, api
	}
	data, code, err := unwrap(buf)
	if err != nil {
		api := &APIError{Status: resp.StatusCode, Code: code, Message: err.Error()}
		if code == "CREDIT_LIMIT_EXCEEDED" {
			api.NoCredit = true
		}
		return nil, api
	}
	return data, nil
}

func cookieHeader() string {
	token := strings.TrimSpace(SessionToken)
	if strings.HasPrefix(token, SessionCookie+"=") {
		return token
	}
	return SessionCookie + "=" + token
}

func unwrap(body []byte) (json.RawMessage, string, error) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, "", fmt.Errorf("空响应")
	}
	if body[0] != '{' {
		return body, "", nil
	}
	var env struct {
		Data      json.RawMessage `json:"data"`
		ErrorCode string          `json:"errorCode"`
		Message   string          `json:"message"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return body, "", nil
	}
	if env.ErrorCode == "CREDIT_LIMIT_EXCEEDED" {
		return nil, env.ErrorCode, fmt.Errorf("额度用完：%s", env.Message)
	}
	if len(env.Data) > 0 && string(env.Data) != "null" {
		return env.Data, env.ErrorCode, nil
	}
	return body, env.ErrorCode, nil
}

func decodeList(raw json.RawMessage) ([]map[string]any, error) {
	var list []map[string]any
	if err := decodeJSON(raw, &list); err == nil {
		return list, nil
	}
	var wrap struct {
		Items    []map[string]any `json:"items"`
		List     []map[string]any `json:"list"`
		Profiles []map[string]any `json:"profiles"`
		Users    []map[string]any `json:"users"`
	}
	if err := decodeJSON(raw, &wrap); err != nil {
		return nil, fmt.Errorf("无法解析用户信息：%s", clip(raw))
	}
	switch {
	case wrap.Profiles != nil:
		return wrap.Profiles, nil
	case wrap.Users != nil:
		return wrap.Users, nil
	case wrap.List != nil:
		return wrap.List, nil
	case wrap.Items != nil:
		return wrap.Items, nil
	default:
		return nil, fmt.Errorf("用户信息里没有列表：%s", clip(raw))
	}
}

func decodeSmart(raw json.RawMessage) ([]map[string]any, int, bool, error) {
	var list []map[string]any
	if err := decodeJSON(raw, &list); err == nil {
		return list, len(list), true, nil
	}
	var wrap struct {
		TotalCount     json.Number      `json:"totalCount"`
		SmartFollowers []map[string]any `json:"smartFollowers"`
	}
	if err := decodeJSON(raw, &wrap); err != nil {
		return nil, 0, false, fmt.Errorf("无法解析聪明关注：%s", clip(raw))
	}
	if wrap.SmartFollowers == nil {
		return nil, 0, false, fmt.Errorf("聪明关注里没有列表：%s", clip(raw))
	}
	if wrap.TotalCount == "" {
		return wrap.SmartFollowers, len(wrap.SmartFollowers), true, nil
	}
	n, err := wrap.TotalCount.Int64()
	if err != nil {
		return wrap.SmartFollowers, len(wrap.SmartFollowers), true, nil
	}
	return wrap.SmartFollowers, int(n), true, nil
}

func decodeJSON(raw []byte, dest any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(dest)
}

func errorFields(body []byte) (string, string) {
	var env struct {
		ErrorCode string `json:"errorCode"`
		Message   string `json:"message"`
	}
	_ = json.Unmarshal(body, &env)
	if env.Message == "" {
		env.Message = clip(body)
	}
	return env.ErrorCode, env.Message
}

func looksRateLimited(body []byte) bool {
	s := strings.ToLower(string(body))
	return strings.Contains(s, "rate_limit") || strings.Contains(s, "too many request")
}

func clip(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		return s[:300]
	}
	return s
}
