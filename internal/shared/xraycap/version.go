// Package xraycap 处理 Xray 版本号解析与比较，以及与版本相关的能力判定。
package xraycap

import (
	"fmt"
	"regexp"
	"strconv"
)

var versionRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

// Version 为 Xray 的三段式版本号，如 v26.7.28。
type Version struct {
	Major, Minor, Patch int
}

// ParseVersion 解析 "v26.7.28" 或 "26.7.28" 形式的版本号。
func ParseVersion(s string) (Version, bool) {
	m := versionRe.FindStringSubmatch(s)
	if m == nil {
		return Version{}, false
	}
	var v Version
	v.Major, _ = strconv.Atoi(m[1])
	v.Minor, _ = strconv.Atoi(m[2])
	v.Patch, _ = strconv.Atoi(m[3])
	return v, true
}

// Compare 返回 -1、0、1 分别表示 v 小于、等于、大于 o。
func (v Version) Compare(o Version) int {
	for _, d := range [3]int{v.Major - o.Major, v.Minor - o.Minor, v.Patch - o.Patch} {
		if d < 0 {
			return -1
		}
		if d > 0 {
			return 1
		}
	}
	return 0
}

// String 返回带 v 前缀的版本号。
func (v Version) String() string {
	return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// IsValidTag 判断字符串是否为合法的 Xray 发布标签，用于防止路径注入。
func IsValidTag(tag string) bool {
	return len(tag) > 0 && tag[0] == 'v' && versionRe.MatchString(tag)
}
