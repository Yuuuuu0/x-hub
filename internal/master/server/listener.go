package server

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/Yuuuuu0/x-hub/internal/master/settings"
)

const sniffTimeout = 10 * time.Second

// dynamicListener 按当前 TLS 模式分发连接：反向代理模式直接交给 http.Server；
// TLS 模式下先窥探首字节，TLS 握手包装为 *tls.Conn（net/http 据此完成握手并设置 r.TLS），
// 明文 HTTP 则返回中文说明，引导把主控切换为反向代理模式。
// 窥探在独立协程中进行，避免慢连接阻塞 Accept。
type dynamicListener struct {
	net.Listener
	tlsCfg   *tls.Config
	settings *settings.Service

	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
	err   error
}

func newDynamicListener(ln net.Listener, cfg *tls.Config, s *settings.Service) *dynamicListener {
	l := &dynamicListener{Listener: ln, tlsCfg: cfg, settings: s, conns: make(chan net.Conn), done: make(chan struct{})}
	go l.loop()
	return l
}

func (l *dynamicListener) loop() {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				l.once.Do(func() { l.err = err; close(l.done) })
				return
			}
			time.Sleep(100 * time.Millisecond) // 文件描述符耗尽等临时错误
			continue
		}
		go l.classify(c)
	}
}

func (l *dynamicListener) classify(c net.Conn) {
	if l.settings.Access().TLSMode == settings.TLSProxy {
		l.deliver(c)
		return
	}
	c.SetReadDeadline(time.Now().Add(sniffTimeout))
	br := bufio.NewReader(c)
	first, err := br.Peek(1)
	if err != nil {
		c.Close()
		return
	}
	pc := &peekedConn{Conn: c, r: br}
	if first[0] == 0x16 { // TLS 握手记录
		c.SetReadDeadline(time.Time{})
		l.deliver(tls.Server(pc, l.tlsCfg))
		return
	}
	rejectPlainHTTP(pc, br)
}

func (l *dynamicListener) deliver(c net.Conn) {
	select {
	case l.conns <- c:
	case <-l.done:
		c.Close()
	}
}

func (l *dynamicListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, l.err
	}
}

// peekedConn 先读出窥探时缓冲的字节，再读底层连接。
type peekedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *peekedConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// plainHTTPHelp 为 TLS 模式下收到明文 HTTP 请求时的说明。
const plainHTTPHelp = `x-hub 主控当前以 HTTPS 提供服务，但收到了明文 HTTP 请求。

如果主控位于 Nginx / Caddy 等反向代理之后，请把主控切换为反向代理模式：

  Docker 部署：
    docker exec xhub xhub-master settings proxy --public-url https://你的域名
    docker restart xhub

  二进制部署：
    xhub-master settings proxy --public-url https://你的域名 --data <数据目录>
    然后重启主控

如果是直接访问主控，请改用 https:// 开头的地址。
`

func rejectPlainHTTP(c net.Conn, br *bufio.Reader) {
	defer c.Close()
	if req, err := http.ReadRequest(br); err == nil {
		req.Body.Close()
	}
	c.SetWriteDeadline(time.Now().Add(sniffTimeout))
	fmt.Fprintf(c, "HTTP/1.1 400 Bad Request\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
		len(plainHTTPHelp), plainHTTPHelp)
}
