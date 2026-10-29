package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Yuuuuu0/x-hub/internal/master/auth"
)

func postLogin(h *Handler, body map[string]string) (*httptest.ResponseRecorder, map[string]any) {
	b, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/panel1234/api/login", bytes.NewReader(b)))
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

// TestLoginWithTOTP 覆盖开启两步验证后的登录：缺少验证码提示、错误验证码拒绝、正确验证码通过。
func TestLoginWithTOTP(t *testing.T) {
	h := newTestHandler(t)
	h.Store.CreateAdmin("admin", auth.HashPassword("pass1234"))
	a, _ := h.Store.AdminByUsername("admin")
	secret := auth.NewTOTPSecret()
	h.Store.SetAdminTOTP(a.ID, secret)

	if rec, out := postLogin(h, map[string]string{"username": "admin", "password": "pass1234"}); rec.Code != 401 || out["needTotp"] != true {
		t.Fatalf("missing code: %d %v", rec.Code, out)
	}
	if rec, _ := postLogin(h, map[string]string{"username": "admin", "password": "pass1234", "code": "000000"}); rec.Code != 401 {
		t.Fatalf("wrong code accepted: %d", rec.Code)
	}
	code := totpNow(t, secret)
	if rec, _ := postLogin(h, map[string]string{"username": "admin", "password": "pass1234", "code": code}); rec.Code != 200 {
		t.Fatalf("valid code rejected: %d %s", rec.Code, rec.Body.String())
	}
}

// totpNow 暴力找出当前时间步可通过校验的验证码，避免在测试中复制算法实现。
func totpNow(t *testing.T, secret string) string {
	now := time.Now()
	for i := 0; i < 1000000; i++ {
		c := []byte("000000")
		for j, v := 5, i; j >= 0; j, v = j-1, v/10 {
			c[j] = byte('0' + v%10)
		}
		if auth.VerifyTOTP(secret, string(c), now) {
			return string(c)
		}
	}
	t.Fatal("no code found")
	return ""
}

func TestBackup(t *testing.T) {
	h := newTestHandler(t)
	h.DataDir = t.TempDir()
	h.Store.CreateAdmin("admin", auth.HashPassword("pass1234"))
	rec, _ := postLogin(h, map[string]string{"username": "admin", "password": "pass1234"})
	req := httptest.NewRequest(http.MethodGet, "/panel1234/api/backup", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != 200 || !bytes.HasPrefix(out.Body.Bytes(), []byte("SQLite format 3")) {
		t.Fatalf("backup: %d %q", out.Code, out.Body.String()[:min(40, out.Body.Len())])
	}
}
