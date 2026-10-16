package main

import "time"

const (
	BaseURL        = "https://loadbalance.frontrun.pro"
	ClientVersion  = "0.0.430"
	ClientLanguage = "ZH_CN"
	// 登录 Cookie 名是 __Secure-frontrun.session_token，不是 better-auth.session_token。
	SessionCookie = "__Secure-frontrun.session_token"
	SessionToken  = "VQi6kJVSmQ7OvNniqS0h8EQxwLNYLmbZ.EzS+14anslr7r42hmQLfHFexHKootqcft3W8x/mJ1gU="
	UsernameFile  = "usernames.txt"
	OutputFile    = "output.json"
	BatchSize     = 30
)

var (
	// 扩展批量请求之间不休眠，连续打会 429。每批 30 人，建议间隔 1 秒。
	BatchInterval = time.Second
	// smart-followers?all=true 比批量接口重，建议每个账号间隔 2 秒。
	SmartInterval = 2 * time.Second
	// 扩展遇到 429 会停。这里等 15 秒再试，最多 3 次。
	RateLimitWait    = 15 * time.Second
	RateLimitRetries = 3
	RequestTimeout   = 30 * time.Second
)
