package main

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"
)

type User struct {
	Username         string   `json:"username"`
	TwitterID        string   `json:"twitterId,omitempty"`
	DisplayName      string   `json:"displayName,omitempty"`
	Bio              string   `json:"bio,omitempty"`
	Avatar           string   `json:"avatar,omitempty"`
	FollowersCount   int      `json:"followersCount"`
	FollowingCount   int      `json:"followingCount"`
	SmartCount       int      `json:"smartFollowersCount"`
	PrimaryLabel     string   `json:"primaryLabel,omitempty"`
	UserDefinedLabel string   `json:"userDefinedLabel,omitempty"`
	Position         string   `json:"position,omitempty"`
	CompactLabel     string   `json:"compactLabel,omitempty"`
	Labels           []string `json:"labels,omitempty"`
	ProfileUpdatedAt string   `json:"profileUpdatedAt,omitempty"`
	Note             string   `json:"note,omitempty"`
}

type Store struct {
	UpdatedAt string `json:"updatedAt"`
	Users     []User `json:"users"`
}

func nowText() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

func normalizeName(s string) string {
	return strings.TrimPrefix(strings.TrimSpace(s), "@")
}

func nameKey(s string) string {
	return strings.ToLower(normalizeName(s))
}

func loadNames(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name := normalizeName(line)
		k := nameKey(name)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, name)
	}
	return out, nil
}

func loadStore(path string) (*Store, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Store{Users: []User{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var raw struct {
		UpdatedAt string            `json:"updatedAt"`
		Users     []json.RawMessage `json:"users"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	doc := &Store{UpdatedAt: raw.UpdatedAt, Users: []User{}}
	for _, item := range raw.Users {
		var u User
		if err := json.Unmarshal(item, &u); err != nil {
			return nil, err
		}
		if u.Username != "" {
			doc.absorb(u)
		}
		var nested struct {
			SmartFollowers []User `json:"smartFollowers"`
		}
		if err := json.Unmarshal(item, &nested); err != nil {
			return nil, err
		}
		for _, follower := range nested.SmartFollowers {
			if follower.Username != "" {
				doc.absorb(follower)
			}
		}
	}
	return doc, nil
}

func saveStore(path string, doc *Store) error {
	doc.UpdatedAt = nowText()
	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return err
	}
	_ = os.Remove(path)
	return os.Rename(tmp, path)
}

func (doc *Store) user(name string) *User {
	k := nameKey(name)
	for i := range doc.Users {
		if nameKey(doc.Users[i].Username) == k {
			if doc.Users[i].Username == "" {
				doc.Users[i].Username = name
			}
			return &doc.Users[i]
		}
	}
	doc.Users = append(doc.Users, User{Username: name})
	return &doc.Users[len(doc.Users)-1]
}

func (doc *Store) absorb(in User) {
	u := doc.user(in.Username)
	u.TwitterID = mergeString(u.TwitterID, in.TwitterID)
	u.DisplayName = mergeString(u.DisplayName, in.DisplayName)
	u.Bio = mergeString(u.Bio, in.Bio)
	u.Avatar = mergeString(u.Avatar, in.Avatar)
	u.FollowersCount = mergeInt(u.FollowersCount, in.FollowersCount)
	u.FollowingCount = mergeInt(u.FollowingCount, in.FollowingCount)
	u.SmartCount = mergeInt(u.SmartCount, in.SmartCount)
	u.PrimaryLabel = mergeString(u.PrimaryLabel, in.PrimaryLabel)
	u.UserDefinedLabel = mergeString(u.UserDefinedLabel, in.UserDefinedLabel)
	u.Position = mergeString(u.Position, in.Position)
	u.CompactLabel = mergeString(u.CompactLabel, in.CompactLabel)
	u.Labels = mergeLabels(u.Labels, in.Labels)
	u.ProfileUpdatedAt = mergeString(u.ProfileUpdatedAt, in.ProfileUpdatedAt)
	if strings.TrimSpace(in.Note) != "" {
		u.Note = mergeString(u.Note, in.Note)
	}
}

func (u *User) applyProfile(m map[string]any, now string) {
	if name := usernameOf(m); name != "" {
		u.Username = mergeString(u.Username, name)
	}
	if v, ok := textOf(m, "twitterId", "restId"); ok {
		u.TwitterID = mergeString(u.TwitterID, v)
	}
	if v, ok := textOf(m, "name", "displayName"); ok {
		u.DisplayName = mergeString(u.DisplayName, v)
	}
	if v, ok := textOf(m, "bio", "description"); ok {
		u.Bio = mergeString(u.Bio, v)
	}
	if v, ok := textOf(m, "profilePhoto", "profileImageUrl", "avatar"); ok {
		u.Avatar = mergeString(u.Avatar, v)
	}
	if v, ok := intOf(m, "followersCount", "followers"); ok {
		u.FollowersCount = mergeInt(u.FollowersCount, v)
	}
	if v, ok := intOf(m, "followingsCount", "followingCount", "friendsCount"); ok {
		u.FollowingCount = mergeInt(u.FollowingCount, v)
	}
	if v, ok := intOf(m, "smartFollowersCount"); ok {
		u.SmartCount = mergeInt(u.SmartCount, v)
	}
	if v, ok := chineseText(m, "primaryLabelCn", "officialLabelCn"); ok && strings.TrimSpace(v) != "" {
		u.PrimaryLabel = mergeString(u.PrimaryLabel, v)
	} else if v, ok := textOf(m, "primaryLabel", "publicLabel", "officialLabel"); ok {
		u.PrimaryLabel = mergeString(u.PrimaryLabel, v)
	}
	if v, ok := textOf(m, "userDefinedLabel"); ok {
		u.UserDefinedLabel = mergeString(u.UserDefinedLabel, v)
	}
	if v, ok := localizedValue(m, "allPositions", "positionCn", "position"); ok {
		u.Position = mergeString(u.Position, v)
	}
	if v, ok := localizedValue(m, "allCompactedLabels", "compactLabelCn", "compactLabel"); ok {
		u.CompactLabel = mergeString(u.CompactLabel, v)
	}
	if labs, ok := localizedList(m, "allCompactedLabels"); ok && len(labs) > 0 {
		u.Labels = mergeLabels(u.Labels, labs)
	}
	u.ProfileUpdatedAt = now
	u.Note = ""
}

func mergeString(old, new string) string {
	old, new = strings.TrimSpace(old), strings.TrimSpace(new)
	if old == "" {
		return new
	}
	if new == "" {
		return old
	}
	if len([]rune(new)) > len([]rune(old)) {
		return new
	}
	return old
}

func mergeInt(old, new int) int {
	if old == 0 {
		return new
	}
	if new == 0 {
		return old
	}
	if new > old {
		return new
	}
	return old
}

func mergeLabels(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range append(append([]string{}, a...), b...) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func usernameOf(m map[string]any) string {
	v, ok := textOf(m, "twitterUsername", "twitter", "username", "screenName", "twitterHandle")
	if !ok {
		return ""
	}
	return normalizeName(v)
}

func textOf(m map[string]any, keys ...string) (string, bool) {
	for _, k := range keys {
		raw, ok := m[k]
		if !ok || raw == nil {
			continue
		}
		switch t := raw.(type) {
		case string:
			return t, true
		case json.Number:
			return t.String(), true
		case float64:
			return strconv.FormatInt(int64(t), 10), true
		case map[string]any:
			if s, ok := textOf(t, "text", "name", "label", "value", ClientLanguage); ok {
				return s, true
			}
		}
	}
	return "", false
}

func chineseText(m map[string]any, keys ...string) (string, bool) {
	if v, ok := textOf(m, keys...); ok && strings.TrimSpace(v) != "" {
		return v, true
	}
	for _, key := range []string{"authorPublicLabels", "publicLabels"} {
		raw, ok := m[key]
		obj, isMap := raw.(map[string]any)
		if !ok || !isMap {
			continue
		}
		if v, ok := textOf(obj, ClientLanguage, "zh", "ZH_CN"); ok && strings.TrimSpace(v) != "" {
			return v, true
		}
	}
	return "", false
}

func localizedValue(m map[string]any, listKey, cnKey, fallback string) (string, bool) {
	if labs, ok := localizedList(m, listKey); ok && len(labs) > 0 {
		return labs[0], true
	}
	if v, ok := textOf(m, cnKey); ok && strings.TrimSpace(v) != "" {
		return v, true
	}
	return textOf(m, fallback)
}

func localizedList(m map[string]any, key string) ([]string, bool) {
	raw, ok := m[key]
	if !ok || raw == nil {
		return nil, false
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, false
	}
	var zh []string
	var rest []string
	for _, item := range arr {
		switch t := item.(type) {
		case string:
			if strings.TrimSpace(t) != "" {
				rest = append(rest, t)
			}
		case map[string]any:
			lang, _ := textOf(t, "language", "lang")
			val, _ := textOf(t, "value", "label", "text")
			if strings.TrimSpace(val) == "" {
				continue
			}
			if strings.EqualFold(lang, ClientLanguage) || strings.EqualFold(lang, "zh") {
				zh = append(zh, val)
				continue
			}
			if lang != "" {
				rest = append(rest, val)
			}
		}
	}
	if len(zh) > 0 {
		return zh, true
	}
	return rest, true
}

func intOf(m map[string]any, keys ...string) (int, bool) {
	for _, k := range keys {
		raw, ok := m[k]
		if !ok || raw == nil {
			continue
		}
		switch t := raw.(type) {
		case float64:
			return int(t), true
		case json.Number:
			n, err := t.Int64()
			if err != nil {
				continue
			}
			return int(n), true
		case string:
			n, err := strconv.Atoi(strings.TrimSpace(t))
			if err == nil {
				return n, true
			}
		}
	}
	return 0, false
}
