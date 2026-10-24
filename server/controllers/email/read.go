package email

import (
	"encoding/json"
	"github.com/Jinnrry/pmail/dto/response"
	"github.com/Jinnrry/pmail/services/detail"
	"github.com/Jinnrry/pmail/utils/context"
	"io"
	"net/http"
)

type markReadRequest struct {
	IDs []int `json:"ids"`
}

func MarkRead(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	var reqData markReadRequest
	err := json.NewDecoder(io.LimitReader(req.Body, 65536)).Decode(&reqData)
	if err != nil {
		response.NewErrorResponse(response.ParamsError, "Invalid request", "").FPrint(w)
		return
	}

	if len(reqData.IDs) <= 0 {
		response.NewErrorResponse(response.ParamsError, "ID错误", "").FPrint(w)
		return
	}

	for _, id := range reqData.IDs {
		_, err = detail.GetEmailDetail(ctx, id, true)
		if err != nil {
			break
		}
	}

	if err != nil {
		response.NewErrorResponse(response.ServerError, err.Error(), "").FPrint(w)
		return
	}
	response.NewSuccessResponse("success").FPrint(w)

}
