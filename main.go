package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

func main() {
	if strings.TrimSpace(SessionToken) == "" {
		fmt.Println("请先在 config.go 里填写 SessionToken。")
		os.Exit(1)
	}

	names, err := loadNames(UsernameFile)
	if err != nil {
		fmt.Printf("读名单失败：%v\n", err)
		os.Exit(1)
	}
	if len(names) == 0 {
		fmt.Printf("%s 里没有用户名。一行一个。\n", UsernameFile)
		os.Exit(1)
	}

	doc, err := loadStore(OutputFile)
	if err != nil {
		fmt.Printf("读已有结果失败，为避免覆盖损坏文件已停止：%v\n", err)
		os.Exit(1)
	}

	fmt.Printf("名单 %d 人，语言 %s。批量间隔 %s，聪明关注间隔 %s，限流等待 %s。\n",
		len(names), ClientLanguage, BatchInterval, SmartInterval, RateLimitWait)
	fmt.Printf("结果写入 %s，重复运行会按用户名合并。\n", OutputFile)

	cli := NewClient()
	if err := fetchProfiles(cli, doc, names); err != nil {
		fmt.Printf("用户信息中断：%v\n", err)
		os.Exit(1)
	}
	if err := fetchSmart(cli, doc, names); err != nil {
		fmt.Printf("聪明关注中断：%v\n", err)
		os.Exit(1)
	}
	fmt.Printf("完成，共 %d 人，文件 %s\n", len(doc.Users), OutputFile)
}

func fetchProfiles(cli *Client, doc *Store, names []string) error {
	batches := chunks(names, BatchSize)
	for i, batch := range batches {
		fmt.Printf("用户信息 第 %d/%d 批，%d 人\n", i+1, len(batches), len(batch))
		list, err := cli.BatchProfiles(batch)
		if err != nil {
			markFailed(doc, batch, "用户信息失败："+err.Error())
			if err := saveStore(OutputFile, doc); err != nil {
				return err
			}
			if stopNow(err) {
				return err
			}
			fmt.Printf("这一批失败，已记到 note：%v\n", err)
		} else {
			now := nowText()
			got := map[string]bool{}
			for _, item := range list {
				name := usernameOf(item)
				if name == "" {
					continue
				}
				got[nameKey(name)] = true
				doc.user(name).applyProfile(item, now)
			}
			var miss []string
			for _, name := range batch {
				if !got[nameKey(name)] {
					miss = append(miss, name)
				}
			}
			if len(miss) > 0 {
				markFailed(doc, miss, "这一批没有返回该用户")
			}
			if err := saveStore(OutputFile, doc); err != nil {
				return err
			}
			fmt.Printf("本批写入 %d 人\n", len(got))
		}
		if i != len(batches)-1 {
			time.Sleep(BatchInterval)
		}
	}
	return nil
}

func fetchSmart(cli *Client, doc *Store, names []string) error {
	for i, name := range names {
		u := doc.user(name)
		fmt.Printf("聪明关注 %d/%d：%s\n", i+1, len(names), u.Username)
		seedName := u.Username
		twitterID := u.TwitterID
		items, total, totalOK, err := cli.SmartFollowers(seedName, twitterID)
		if err != nil {
			doc.user(seedName).Note = "聪明关注失败：" + err.Error()
			if err := saveStore(OutputFile, doc); err != nil {
				return err
			}
			if stopNow(err) {
				return err
			}
			fmt.Printf("%s 失败，已记到 note：%v\n", seedName, err)
		} else {
			now := nowText()
			added := 0
			for _, item := range items {
				follower := usernameOf(item)
				if follower == "" {
					continue
				}
				doc.user(follower).applyProfile(item, now)
				added++
			}
			seed := doc.user(seedName)
			if totalOK {
				seed.SmartCount = mergeInt(seed.SmartCount, total)
			}
			seed.Note = ""
			if err := saveStore(OutputFile, doc); err != nil {
				return err
			}
			fmt.Printf("%s 并入聪明关注 %d 人，名单共 %d 人\n", seedName, added, len(doc.Users))
		}
		if i != len(names)-1 {
			time.Sleep(SmartInterval)
		}
	}
	return nil
}

func chunks(names []string, size int) [][]string {
	if size <= 0 {
		size = 30
	}
	var out [][]string
	for len(names) > 0 {
		n := size
		if n > len(names) {
			n = len(names)
		}
		out = append(out, names[:n])
		names = names[n:]
	}
	return out
}

func markFailed(doc *Store, names []string, msg string) {
	for _, name := range names {
		doc.user(name).Note = msg
	}
}

func stopNow(err error) bool {
	var api *APIError
	if errors.As(err, &api) {
		return api.NoCredit || api.Status == 401
	}
	return false
}
