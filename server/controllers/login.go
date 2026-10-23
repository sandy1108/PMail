package controllers

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"github.com/Jinnrry/pmail/config"
	"github.com/Jinnrry/pmail/db"
	"github.com/Jinnrry/pmail/dto/response"
	"github.com/Jinnrry/pmail/i18n"
	"github.com/Jinnrry/pmail/models"
	"github.com/Jinnrry/pmail/session"
	"github.com/Jinnrry/pmail/utils/array"
	"github.com/Jinnrry/pmail/utils/context"
	"github.com/Jinnrry/pmail/utils/errors"
	"github.com/Jinnrry/pmail/utils/password"
	log "github.com/sirupsen/logrus"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type loginRequest struct {
	Account  string `json:"account"`
	Password string `json:"password"`
}

// 单用户部署采用全局小桶限制口令计算，避免伪造IP绕过限制或增长内存。
var loginRate = struct {
	sync.Mutex
	tokens float64
	last   time.Time
}{tokens: 10, last: time.Now()}

func allowLogin() bool {
	loginRate.Lock()
	defer loginRate.Unlock()
	now := time.Now()
	loginRate.tokens += now.Sub(loginRate.last).Seconds() / 3
	loginRate.last = now
	if loginRate.tokens > 10 {
		loginRate.tokens = 10
	}
	if loginRate.tokens < 1 {
		return false
	}
	loginRate.tokens--
	return true
}

func Login(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if !allowLogin() {
		http.Error(w, "Too many login attempts", http.StatusTooManyRequests)
		return
	}
	req.Body = http.MaxBytesReader(w, req.Body, 16384)

	reqBytes, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, "Invalid login request", http.StatusBadRequest)
		return
	}
	var reqData loginRequest
	err = json.Unmarshal(reqBytes, &reqData)
	if err != nil {
		http.Error(w, "Invalid login request", http.StatusBadRequest)
		return
	}

	reqData.Account = strings.Split(strings.ToLower(strings.TrimSpace(reqData.Account)), "@")[0]
	var user models.User
	_, err = db.Instance.Where("account =? and disabled=0", reqData.Account).Get(&user)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		log.Errorf("%+v", err)
	}

	if user.ID != 0 && password.Verify(user.Password, reqData.Password) {
		if err := session.Instance.RenewToken(req.Context()); err != nil {
			http.Error(w, "session error", 500)
			return
		}
		userStr, _ := json.Marshal(user)
		digest := sha256.Sum256([]byte(user.Password))
		session.Instance.Put(req.Context(), "credential", hex.EncodeToString(digest[:]))
		session.Instance.Put(req.Context(), "user", string(userStr))

		domains := config.Instance.Domains
		domains = array.Difference(domains, []string{config.Instance.Domain})
		domains = append([]string{config.Instance.Domain}, domains...)

		response.NewSuccessResponse(map[string]any{
			"account":  user.Account,
			"name":     user.Name,
			"is_admin": user.IsAdmin,
			"domains":  domains,
		}).FPrint(w)
	} else {
		response.NewErrorResponse(response.ParamsError, i18n.GetText(ctx.Lang, "aperror"), "").FPrint(w)
	}
}

func Logout(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	session.Instance.Clear(ctx.Context)
	response.NewSuccessResponse("Success").FPrint(w)
}
