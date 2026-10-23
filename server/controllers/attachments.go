package controllers

import (
	"github.com/Jinnrry/pmail/dto/response"
	"github.com/Jinnrry/pmail/services/attachments"
	"github.com/Jinnrry/pmail/utils/context"
	"github.com/spf13/cast"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

func GetAttachments(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	urlInfos := strings.Split(req.URL.Path, "/")
	if len(urlInfos) != 4 {
		response.NewErrorResponse(response.ParamsError, "", "").FPrint(w)
		return
	}
	emailId := cast.ToInt(urlInfos[2])
	cid := urlInfos[3]

	contentType, content := attachments.GetAttachments(ctx, emailId, cid)

	if len(content) == 0 {
		response.NewErrorResponse(response.ParamsError, "", "").FPrint(w)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Write(content)
}

func Download(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	urlInfos := strings.Split(req.URL.Path, "/")
	if len(urlInfos) != 5 {
		response.NewErrorResponse(response.ParamsError, "", "").FPrint(w)
		return
	}
	emailId, idErr := strconv.Atoi(urlInfos[3])
	index, indexErr := strconv.Atoi(urlInfos[4])
	if idErr != nil || indexErr != nil || emailId <= 0 || index < 0 {
		response.NewErrorResponse(response.ParamsError, "Invalid attachment", "").FPrint(w)
		return
	}

	fileName, content := attachments.GetAttachmentsByIndex(ctx, emailId, index)

	if content == nil {
		response.NewErrorResponse(response.ParamsError, "", "").FPrint(w)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": fileName}))
	w.Write(content)
}
