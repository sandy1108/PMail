package controllers

import (
	"errors"
	"github.com/Jinnrry/pmail/dto/response"
	"github.com/Jinnrry/pmail/services/mailaddress"
	"github.com/Jinnrry/pmail/utils/context"
	"net/http"
)

// 别名管理使用网页会话，归属始终取自认证上下文，不接收客户端user_id。
func aliasError(w http.ResponseWriter, err error) {
	code, message := response.ServerError, "Alias operation failed"
	switch {
	case errors.Is(err, mailaddress.ErrConflict):
		code, message = response.ParamsError, "Address already reserved"
	case errors.Is(err, mailaddress.ErrParams):
		code, message = response.ParamsError, "Invalid alias parameters"
	case errors.Is(err, mailaddress.ErrNotFound):
		code, message = response.ParamsError, "Alias unavailable"
	}
	response.NewErrorResponse(code, message, "").FPrint(w)
}
func CreateEmailAlias(ctx *context.Context, w http.ResponseWriter, r *http.Request) {
	var req struct {
		LocalPart string `json:"local_part"`
		Domain    string `json:"domain"`
		Remark    string `json:"remark"`
	}
	if !decodeAccessCodeRequest(w, r, &req) {
		return
	}
	alias, err := mailaddress.Create(ctx.UserID, req.LocalPart, req.Domain, req.Remark)
	if err != nil {
		aliasError(w, err)
		return
	}
	response.NewSuccessResponse(alias).FPrint(w)
}
func ListEmailAliases(ctx *context.Context, w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentPage int    `json:"current_page"`
		PageSize    int    `json:"page_size"`
		Keyword     string `json:"keyword"`
	}
	if !decodeAccessCodeRequest(w, r, &req) {
		return
	}
	page, err := mailaddress.List(ctx.UserID, req.CurrentPage, req.PageSize, req.Keyword)
	if err != nil {
		aliasError(w, err)
		return
	}
	response.NewSuccessResponse(page).FPrint(w)
}
func UpdateEmailAlias(ctx *context.Context, w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      int64   `json:"id"`
		Remark  *string `json:"remark"`
		Enabled *bool   `json:"enabled"`
	}
	if !decodeAccessCodeRequest(w, r, &req) {
		return
	}
	if req.Remark == nil || req.Enabled == nil {
		aliasError(w, mailaddress.ErrParams)
		return
	}
	if err := mailaddress.Update(ctx.UserID, req.ID, *req.Remark, *req.Enabled); err != nil {
		aliasError(w, err)
		return
	}
	response.NewSuccessResponse(true).FPrint(w)
}
func DeleteEmailAlias(ctx *context.Context, w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID int64 `json:"id"`
	}
	if !decodeAccessCodeRequest(w, r, &req) {
		return
	}
	if err := mailaddress.Delete(ctx.UserID, req.ID); err != nil {
		aliasError(w, err)
		return
	}
	response.NewSuccessResponse(true).FPrint(w)
}
