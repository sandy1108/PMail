package http_server

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Jinnrry/pmail/config"
	"github.com/Jinnrry/pmail/controllers"
	"github.com/Jinnrry/pmail/db"
	"github.com/Jinnrry/pmail/dto/response"
	"github.com/Jinnrry/pmail/i18n"
	"github.com/Jinnrry/pmail/models"
	"github.com/Jinnrry/pmail/session"
	"github.com/Jinnrry/pmail/utils/context"
	"github.com/Jinnrry/pmail/utils/id"
	olog "log"
	"net/http"
	"net/url"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cast"
)

//go:embed dist/*
var local embed.FS

var httpsServer *http.Server

type nullWrite struct {
}

func (w *nullWrite) Write(p []byte) (int, error) {
	return len(p), nil
}

func HttpsStart() {

	mux := http.NewServeMux()

	router(mux)

	// go http server会打一堆没用的日志，写一个空的日志处理器，屏蔽掉日志输出
	nullLog := olog.New(&nullWrite{}, "", olog.Ldate)

	HttpsPort := 443
	if config.Instance.HttpsPort > 0 {
		HttpsPort = config.Instance.HttpsPort
	}

	if config.Instance.HttpsEnabled != 2 {
		log.Infof("Https Server Start On Port :%d", HttpsPort)
		httpsServer = &http.Server{
			Addr:         config.Instance.HTTPAddress(HttpsPort),
			Handler:      session.Instance.LoadAndSave(mux),
			ReadTimeout:  time.Second * 90,
			WriteTimeout: time.Second * 90,
			ErrorLog:     nullLog,
		}
		err := httpsServer.ListenAndServeTLS(config.Instance.SSLPublicKeyPath, config.Instance.SSLPrivateKeyPath)
		if err != nil {
			if errors.Is(err, http.ErrServerClosed) {
				// 正常关闭（重启或停机）
				log.Infof("Https Server closed normally on port :%d", HttpsPort)
			} else {
				// 异常错误仍然打印并退出
				log.Errorf("Https Server error on port :%d, %+v", HttpsPort, err)
				panic(err)
			}
		}
	}
}

func HttpsStop() {
	if httpsServer != nil {
		httpsServer.Close()
	}
}

// 新增：分类处理关闭错误

// 注入context
func contextIterceptor(h controllers.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/json")
		}

		if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/ping" && !validWriteOrigin(r) {
			http.Error(w, "Forbidden request origin or content type", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 40<<20)
		ctx := &context.Context{}
		ctx.Context = r.Context()
		ctx.SetValue(context.LogID, id.GenLogID())
		lang := r.Header.Get("Lang")
		if lang == "" {
			lang = "en"
		}
		ctx.Lang = lang

		if config.IsInit {
			user := cast.ToString(session.Instance.Get(ctx, "user"))
			var userInfo models.User
			if user != "" {
				_ = json.Unmarshal([]byte(user), &userInfo)
			}
			if userInfo.ID > 0 {
				var current models.User
				ok, err := db.Instance.ID(userInfo.ID).Where("disabled=0").Get(&current)
				digest := sha256.Sum256([]byte(current.Password))
				if err != nil || !ok || session.Instance.GetString(ctx, "credential") != hex.EncodeToString(digest[:]) {
					userInfo = models.User{}
					session.Instance.Clear(ctx)
				} else {
					userInfo = current
				}
			}
			if userInfo.ID > 0 {
				ctx.UserID = userInfo.ID
				ctx.UserName = userInfo.Name
				ctx.UserAccount = userInfo.Account
				ctx.IsAdmin = userInfo.IsAdmin == 1
			}

			if userInfo.ID == 0 {
				token := r.Header.Get("Token")
				if token != "" {
					user, err := getLoginInfoByToken(token)
					if user.ID >= 0 && err == nil {
						ctx.UserID = user.ID
						ctx.UserName = user.Name
						ctx.UserAccount = user.Account
						ctx.IsAdmin = user.IsAdmin == 1
					} else {
						response.NewErrorResponse(response.NeedLogin, err.Error(), "").FPrint(w)
						return
					}
				}
			}

			if ctx.UserID == 0 {
				if r.URL.Path != "/api/ping" && r.URL.Path != "/api/login" {
					response.NewErrorResponse(response.NeedLogin, i18n.GetText(ctx.Lang, "login_exp"), "").FPrint(w)
					return
				}
			}
		} else if r.URL.Path != "/api/setup" {
			response.NewErrorResponse(response.NeedSetup, "", "").FPrint(w)
			return
		}
		h(ctx, w, r)
	}
}

// 旧密码衍生Token无法安全兼容新口令存储，明确停用。
func getLoginInfoByToken(token string) (models.User, error) {
	return models.User{}, errors.New("legacy Token authentication disabled; use web session")
}

// 变更请求只允许JSON和已配置的同源网页；不依赖客户端提供的转发头。
func validWriteOrigin(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		switch r.URL.Path {
		case "/api/group", "/api/group/list", "/api/plugin/list":
			return true
		default:
			return false
		}
	}
	if r.Method != http.MethodPost {
		return false
	}
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		ref, err := url.Parse(r.Header.Get("Referer"))
		if err == nil && ref.Host != "" {
			origin = ref.Scheme + "://" + ref.Host
		}
	}
	return config.Instance.WebOrigin != "" && origin == config.Instance.WebOrigin
}
