// Package share 渲染订阅分享页（用量、到期信息与各格式订阅内容的查看、下载入口）与订阅内容的查看页。
package share

import (
	_ "embed"
	"fmt"
	"html/template"
	"io"
	"strings"

	"rsc.io/qr"
)

//go:embed page.html
var pageHTML string

//go:embed viewer.html
var viewerHTML string

var (
	tmpl       = template.Must(template.New("share").Parse(pageHTML))
	viewerTmpl = template.Must(template.New("viewer").Parse(viewerHTML))
)

// Format 为分享页中的一种订阅内容格式。
type Format struct {
	Name     string
	Clients  string // 适用的客户端
	Note     string // 使用提示，可为空
	Link     string // 客户端使用的订阅链接
	View     string // 在浏览器中查看
	Download string // 下载文件
}

// Formats 返回各格式的订阅内容入口，subURL 为不带参数的订阅链接。
func Formats(subURL string) []Format {
	f := func(name, format, clients, note string) Format {
		link := subURL + "?format=" + format
		return Format{Name: name, Clients: clients, Note: note, Link: link, View: link + "&view=1", Download: link + "&download=1"}
	}
	return []Format{
		f("Mihomo", "mihomo", "Clash Verge Rev、Mihomo Party、Clash Meta for Android、FlClash、Stash", ""),
		f("sing-box", "singbox", "sing-box 官方客户端（SFI / SFA / SFM），需 1.12 及以上", ""),
		f("通用", "base64", "Shadowrocket、v2rayN、v2rayNG、NekoBox 等", "Base64 编码的节点分享链接，查看时会解码为每行一条。"),
		f("Shadowrocket 分流配置", "shadowrocket", "Shadowrocket", "只含分组与规则，不含节点：在「配置」页添加，节点仍需在首页用通用链接订阅。"),
	}
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
	Formats     []Format
	Nodes       []NodeItem
	UpdateHours int
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

// Viewer 为订阅内容查看页数据。
type Viewer struct {
	Name     string // 订阅名称
	Format   string // 格式名称
	Content  string
	Lines    int
	Download string
	Back     string // 返回分享页
}

// RenderViewer 输出订阅内容查看页。
func RenderViewer(w io.Writer, v *Viewer) error {
	v.Content = strings.TrimRight(v.Content, "\n")
	v.Lines = strings.Count(v.Content, "\n") + 1
	return viewerTmpl.Execute(w, v)
}
