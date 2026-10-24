package http_server

import (
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/Jinnrry/pmail/config"
	"github.com/Jinnrry/pmail/dto/response"
	"github.com/Jinnrry/pmail/services/accesscode"
	"github.com/Jinnrry/pmail/utils/context"
)

// 授权码只按显式路径和方法放行；未来新增接口默认不可访问。
func allowedAccessCodeRequest(r *http.Request) bool {
	return allowedScopedRequest(r, false)
}
func allowedScopedRequest(r *http.Request, write bool) bool {
	if write && r.Method == http.MethodPost {
		switch r.URL.Path {
		case "/api/email/read", "/api/email/move", "/api/email/del", "/api/group/add", "/api/group/del":
			return true
		}
	}
	switch r.URL.Path {
	case "/api/group", "/api/group/list":
		return r.Method == http.MethodGet || r.Method == http.MethodPost
	case "/api/email/list", "/api/email/detail":
		return r.Method == http.MethodPost
	}
	if r.Method != http.MethodGet {
		return false
	}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) == 5 && parts[1] == "attachments" && parts[2] == "download" {
		id, e1 := strconv.Atoi(parts[3])
		index, e2 := strconv.Atoi(parts[4])
		return e1 == nil && e2 == nil && id > 0 && index >= 0
	}
	if len(parts) == 4 && parts[1] == "attachments" && parts[2] != "download" {
		id, err := strconv.Atoi(parts[2])
		return err == nil && id > 0 && parts[3] != ""
	}
	return false
}

// 任何Authorization头都优先处理；无效码不能回落到现有Cookie身份。
func authenticateAccessCode(w http.ResponseWriter, r *http.Request, ctx *context.Context) bool {
	w.Header().Set("Cache-Control", "no-store")
	fail := func(status int, message string) bool {
		if status == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", `Bearer realm="PMail"`)
		}
		w.WriteHeader(status)
		response.NewErrorResponse(status, message, "").FPrint(w)
		return false
	}
	values := r.Header.Values("Authorization")
	if !config.IsInit || len(values) != 1 {
		return fail(http.StatusUnauthorized, "Invalid access code")
	}
	fields := strings.Fields(values[0])
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return fail(http.StatusUnauthorized, "Invalid access code")
	}
	grant, err := accesscode.AuthenticateFor(fields[1], "http")
	user := grant.User
	if err != nil {
		return fail(http.StatusUnauthorized, "Invalid or inactive access code")
	}
	if !allowedScopedRequest(r, grant.HTTPWrite) {
		return fail(http.StatusForbidden, "Access code does not permit this operation")
	}
	// Bearer不使用Cookie及网页CSRF校验；有浏览器来源时仍拒绝跨站请求。
	if origin := r.Header.Get("Origin"); origin != "" && origin != config.Instance.WebOrigin {
		return fail(http.StatusForbidden, "Forbidden request origin")
	}
	if r.Method == http.MethodPost {
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || !strings.EqualFold(mediaType, "application/json") {
			return fail(http.StatusForbidden, "JSON required")
		}
	}
	ctx.UserID = user.ID
	ctx.UserAccount = user.Account
	ctx.UserName = user.Name
	ctx.IsAdmin = false
	ctx.ReadOnly = !grant.HTTPWrite
	return true
}
