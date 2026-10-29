package api

import (
	"bytes"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// serveSPA 提供前端静态资源；非资源路径回退到 index.html，并把 <base href> 改写为面板路径。
func (h *Handler) serveSPA(w http.ResponseWriter, r *http.Request, base, name string) {
	name = path.Clean("/" + name)[1:]
	if name != "" && name != "index.html" {
		if st, err := fs.Stat(h.Web, name); err == nil && !st.IsDir() {
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			http.ServeFileFS(w, r, h.Web, name)
			return
		}
		if strings.HasPrefix(name, "assets/") {
			http.NotFound(w, r)
			return
		}
	}
	index, err := fs.ReadFile(h.Web, "index.html")
	if err != nil {
		http.Error(w, "前端资源未构建", http.StatusNotFound)
		return
	}
	index = bytes.Replace(index, []byte(`<base href="/">`), []byte(`<base href="`+base+`">`), 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(index)
}
