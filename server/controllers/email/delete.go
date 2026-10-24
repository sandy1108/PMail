package email

import (
	"encoding/json"
	"github.com/Jinnrry/pmail/dto/response"
	"github.com/Jinnrry/pmail/services/del_email"
	"github.com/Jinnrry/pmail/utils/context"
	"io"
	"net/http"
)

type emailDeleteRequest struct {
	IDs       []int `json:"ids"`
	ForcedDel bool  `json:"forcedDel"`
}

func EmailDelete(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	var reqData emailDeleteRequest
	err := json.NewDecoder(io.LimitReader(req.Body, 65536)).Decode(&reqData)
	if err != nil {
		response.NewErrorResponse(response.ParamsError, "Invalid request", "").FPrint(w)
		return
	}

	if len(reqData.IDs) <= 0 {
		response.NewErrorResponse(response.ParamsError, "ID错误", "").FPrint(w)
		return
	}

	err = del_email.DelEmail(ctx, reqData.IDs, reqData.ForcedDel)
	if err != nil {
		response.NewErrorResponse(response.ServerError, err.Error(), "").FPrint(w)
		return
	}
	response.NewSuccessResponse("success").FPrint(w)

}
