// Package share 渲染订阅分享页：使用者在浏览器中打开订阅链接时看到的用量、到期信息与各客户端一键导入入口。
package share

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"io"
	"net/url"
	"strings"

	"rsc.io/qr"
)

//go:embed page.html
var pageHTML string

var tmpl = template.Must(template.New("share").Parse(pageHTML))

// Client 为一个客户端的导入方式。Import 为一键导入的 URL Scheme（为空表示只能复制链接），Link 为复制的订阅链接。
type Client struct {
	Name        string
	Platforms   string
	Import      template.URL
	Link        string
	Steps       string
	Recommended bool
	Extra       *Extra // 额外需要导入的链接（如 Shadowrocket 分流配置）
}

// Extra 为客户端额外需要导入的链接。
type Extra struct {
	Label string
	Link  string
	Steps string
}

// NodeItem 为页面中列出的节点。ClashOnly 表示只出现在 Clash 系与 sing-box 订阅中。
type NodeItem struct {
	Name      string
	ClashOnly bool
}

// Page 为分享页数据。
type Page struct {
	Name        string
	Status      string
	StatusTone  string // ok / warn / bad
	Used        string
	Limit       string // 为空表示不限量
	Percent     float64
	UsageTone   string
	Reset       string
	Expire      string
	ExpireTone  string
	SubURL      string
	QR          template.HTML
	Clients     []Client
	Nodes       []NodeItem
	UpdateHours int
}

// Device 为按 User-Agent 推断的设备类型。
type Device string

const (
	DeviceIOS     Device = "ios"
	DeviceAndroid Device = "android"
	DeviceDesktop Device = "desktop"
)

// DetectDevice 按 User-Agent 推断设备类型，用于推荐客户端。
func DetectDevice(ua string) Device {
	ua = strings.ToLower(ua)
	switch {
	case strings.Contains(ua, "iphone") || strings.Contains(ua, "ipad"):
		return DeviceIOS
	case strings.Contains(ua, "android"):
		return DeviceAndroid
	}
	return DeviceDesktop
}

// Clients 返回各客户端的导入方式，与设备匹配的排在前面并标记为推荐。
func Clients(subURL, name string, device Device) []Client {
	q := url.QueryEscape(subURL)
	n := url.QueryEscape(name)
	all := []struct {
		Client
		devices []Device
	}{
		{Client{Name: "Clash Verge Rev / Mihomo Party", Platforms: "Windows · macOS · Linux",
			Import: template.URL("clash://install-config?url=" + q + "&name=" + n), Link: subURL,
			Steps: "点击「一键导入」，在客户端中确认；或在「订阅 / 配置」中粘贴链接导入。"}, []Device{DeviceDesktop}},
		{Client{Name: "Clash Meta for Android / FlClash", Platforms: "Android",
			Import: template.URL("clash://install-config?url=" + q + "&name=" + n), Link: subURL,
			Steps: "点击「一键导入」；或在「配置」中新建，选择从 URL 导入并粘贴链接。"}, []Device{DeviceAndroid}},
		{Client{Name: "Shadowrocket", Platforms: "iOS",
			Import: template.URL("shadowrocket://add/sub://" + base64.StdEncoding.EncodeToString([]byte(subURL)) + "?remark=" + n), Link: subURL,
			Steps: "点击「一键导入」添加节点订阅；或在首页右上角「+」中选择 Subscribe 并粘贴链接。",
			Extra: &Extra{Label: "分流配置", Link: subURL + "?format=shadowrocket",
				Steps: "需要分流时，在底部「配置」页右上角「+」粘贴此链接并下载，点击它选择「使用配置」，再把首页的「全局路由」设为「配置」。不要加在首页。"}},
			[]Device{DeviceIOS}},
		{Client{Name: "Stash", Platforms: "iOS · macOS",
			Import: template.URL("stash://install-config?url=" + q + "&name=" + n), Link: subURL,
			Steps: "点击「一键导入」；或在「配置」中从 URL 下载配置。"}, []Device{DeviceIOS}},
		{Client{Name: "sing-box", Platforms: "iOS · Android · macOS",
			Import: template.URL("sing-box://import-remote-profile?url=" + q + "#" + url.PathEscape(name)), Link: subURL,
			Steps: "需要 1.12 及以上版本。点击「一键导入」；或在 Profiles 中新建远程配置并粘贴链接。"}, []Device{DeviceIOS, DeviceAndroid, DeviceDesktop}},
		{Client{Name: "v2rayN / v2rayNG / NekoBox", Platforms: "Windows · Android",
			Link: subURL, Steps: "复制链接，在「订阅分组 / 订阅设置」中添加后更新订阅。"}, nil},
	}
	var first, rest []Client
	for _, c := range all {
		matched := false
		for _, d := range c.devices {
			if d == device {
				matched = true
			}
		}
		if matched && len(first) < 2 {
			c.Recommended = true
			first = append(first, c.Client)
		} else {
			rest = append(rest, c.Client)
		}
	}
	return append(first, rest...)
}

// QRCode 把文本编码为二维码 SVG（含 2 格留白）。
func QRCode(text string) template.HTML {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return ""
	}
	const quiet = 2
	size := code.Size + quiet*2
	var path strings.Builder
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			if code.Black(x, y) {
				fmt.Fprintf(&path, "M%d %dh1v1h-1z", x+quiet, y+quiet)
			}
		}
	}
	return template.HTML(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges" role="img" aria-label="订阅链接二维码"><rect width="100%%" height="100%%" fill="#fff"/><path d="%s" fill="#0b0d12"/></svg>`, size, size, path.String()))
}

// HumanBytes 把字节数格式化为 1.2 GB 形式。
func HumanBytes(b int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	v, i := float64(max(b, 0)), 0
	for v >= 1024 && i < len(units)-1 {
		v, i = v/1024, i+1
	}
	if i == 0 {
		return fmt.Sprintf("%d B", int64(v))
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

// Render 输出分享页。
func Render(w io.Writer, p *Page) error {
	return tmpl.Execute(w, p)
}
