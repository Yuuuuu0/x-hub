// Package web 内嵌前端构建产物。
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist 返回前端构建产物根目录。
func Dist() fs.FS {
	sub, _ := fs.Sub(dist, "dist")
	return sub
}
