package controllers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/Jinnrry/pmail/dto/response"
	"github.com/Jinnrry/pmail/services/accesscode"
	"github.com/Jinnrry/pmail/session"
	"github.com/Jinnrry/pmail/utils/context"
)

// 授权码管理只允许网页会话调用；Bearer入口白名单不包含这些路由。
func decodeAccessCodeRequest(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if dec.Decode(v) != nil || dec.Decode(new(any)) != io.EOF {
		response.NewErrorResponse(response.ParamsError, "Invalid request", "").FPrint(w)
		return false
	}
	return true
}
func accessCodeError(w http.ResponseWriter, err error) {
	if errors.Is(err, accesscode.ErrParams) || errors.Is(err, accesscode.ErrInvalid) {
		response.NewErrorResponse(response.ParamsError, "Invalid access code or parameters", "").FPrint(w)
		return
	}
	response.NewErrorResponse(response.ServerError, "Access code operation failed", "").FPrint(w)
}
func CreateAccessCode(ctx *context.Context, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var req struct {
		Name      string `json:"name"`
		ExpiresAt int64  `json:"expires_at"`
	}
	if !decodeAccessCodeRequest(w, r, &req) {
		return
	}
	code, token, err := accesscode.Create(ctx.UserID, req.Name, req.ExpiresAt, session.Instance.GetString(ctx, "credential"))
	if err != nil {
		accessCodeError(w, err)
		return
	}
	response.NewSuccessResponse(struct {
		ID    int64  `json:"id"`
		Token string `json:"token"`
	}{code.ID, token}).FPrint(w)
}
func ListAccessCodes(ctx *context.Context, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var req struct{}
	if !decodeAccessCodeRequest(w, r, &req) {
		return
	}
	codes, err := accesscode.List(ctx.UserID)
	if err != nil {
		accessCodeError(w, err)
		return
	}
	response.NewSuccessResponse(codes).FPrint(w)
}
func RevokeAccessCode(ctx *context.Context, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var req struct {
		ID int64 `json:"id"`
	}
	if !decodeAccessCodeRequest(w, r, &req) {
		return
	}
	if err := accesscode.Revoke(ctx.UserID, req.ID); err != nil {
		accessCodeError(w, err)
		return
	}
	response.NewSuccessResponse(true).FPrint(w)
}
