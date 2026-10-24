package config

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/Jinnrry/pmail/utils/context"
	"github.com/Jinnrry/pmail/utils/errors"
	"github.com/Jinnrry/pmail/utils/file"
	log "github.com/sirupsen/logrus"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var IsInit bool

type Config struct {
	IMAPBind             string   `json:"imapBind"`
	IMAPPort             int      `json:"imapPort"`
	POP3Bind             string   `json:"pop3Bind"`
	POP3Port             int      `json:"pop3Port"`
	HTTPBind             string   `json:"httpBind"`
	SMTPBind             string   `json:"smtpBind"`
	SMTPPort             int      `json:"smtpPort"`
	SMTPHostname         string   `json:"smtpHostname"`
	DisableSMTP          bool     `json:"disableSMTP"`
	EnableSubmission     bool     `json:"enableSubmission"`
	EnablePOP3           bool     `json:"enablePOP3"`
	EnableIMAP           bool     `json:"enableIMAP"`
	SMTPAuthEnabled      bool     `json:"smtpAuthEnabled"`
	ProxyRequired        bool     `json:"proxyRequired"`
	ProxyTrustedIPs      []string `json:"proxyTrustedIPs"`
	WebOrigin            string   `json:"webOrigin"`
	InsecureLocalCookies bool     `json:"insecureLocalCookies"`

	LogLevel             string            `json:"logLevel"` // 日志级别
	Domain               string            `json:"domain"`
	Domains              []string          `json:"domains"` //多域名设置，把所有收信域名都填进去
	WebDomain            string            `json:"webDomain"`
	DkimPrivateKeyPath   string            `json:"dkimPrivateKeyPath"`
	SSLType              string            `json:"sslType"` // 0表示自动生成证书，HTTP挑战模式，1表示用户上传证书，2表示自动-DNS挑战模式
	SSLPrivateKeyPath    string            `json:"SSLPrivateKeyPath"`
	SSLPublicKeyPath     string            `json:"SSLPublicKeyPath"`
	DbDSN                string            `json:"dbDSN"`
	DbType               string            `json:"dbType"`
	HttpsEnabled         int               `json:"httpsEnabled"`    //后台页面是否启用https，0默认（启用），1启用，2不启用
	SpamFilterLevel      int               `json:"spamFilterLevel"` //垃圾邮件过滤级别，0不过滤、1 spf dkim 校验均失败时过滤，2 spf校验不通过时过滤 3,dkim 校验不过的时候过滤
	HttpPort             int               `json:"httpPort"`        //http服务端口设置，默认80
	HttpsPort            int               `json:"httpsPort"`       //https服务端口，默认443
	WeChatPushAppId      string            `json:"weChatPushAppId"`
	WeChatPushSecret     string            `json:"weChatPushSecret"`
	WeChatPushTemplateId string            `json:"weChatPushTemplateId"`
	WeChatPushUserId     string            `json:"weChatPushUserId"`
	TgBotToken           string            `json:"tgBotToken"`
	TgChatId             string            `json:"tgChatId"`
	IsInit               bool              `json:"isInit"`
	WebPushUrl           string            `json:"webPushUrl"`
	WebPushToken         string            `json:"webPushToken"`
	Tables               map[string]string `json:"-"`
	TablesInitData       map[string]string `json:"-"`
	setupPort            int               // 初始化阶段端口
}

var ROOT_PATH = ""

func init() {
	envs := os.Environ()
	for _, env := range envs {
		if strings.HasPrefix(env, "PMail_ROOT=") {
			ROOT_PATH = strings.TrimSpace(strings.ReplaceAll(env, "PMail_ROOT=", ""))
			if !strings.HasSuffix(ROOT_PATH, "/") {
				ROOT_PATH += "/"
			}

			fmt.Println("Env Root Path:", ROOT_PATH)
			return
		}
	}

	ex, err := os.Executable()
	if err != nil {
		panic(err)
	}
	exPath := filepath.Dir(ex)
	realPath, err := filepath.EvalSymlinks(exPath)
	if err != nil {
		panic(err)
	}
	// 如果是Goland运行，不修改根路径
	if strings.Contains(realPath, "GoLand") && strings.Contains(realPath, "JetBrains") {
		return
	}

	if !strings.HasSuffix(realPath, "/") {
		realPath += "/"
	}
	ROOT_PATH = realPath
	fmt.Println("Root Path:", ROOT_PATH)
}

func (c *Config) GetSetupPort() int {
	return c.setupPort
}

func (c *Config) SetSetupPort(setupPort int) {
	c.setupPort = setupPort
}

const DBTypeMySQL = "mysql"
const DBTypeSQLite = "sqlite"
const DBTypePostgres = "postgres"
const SSLTypeAutoHTTP = "0" //自动生成证书
const SSLTypeAutoDNS = "2"  //自动生成证书，DNS api验证
const SSLTypeUser = "1"     //用户上传证书

var DBTypes []string = []string{DBTypeMySQL, DBTypeSQLite, DBTypePostgres}

var Instance *Config = &Config{}

type logFormatter struct {
}

// Format 定义日志输出格式
func (l *logFormatter) Format(entry *log.Entry) ([]byte, error) {
	b := bytes.Buffer{}

	b.WriteString(fmt.Sprintf("[%s]", entry.Level.String()))
	b.WriteString(fmt.Sprintf("[%s]", entry.Time.Format("2006-01-02 15:04:05")))
	if entry.Context != nil {
		ctx := entry.Context.(*context.Context)
		if ctx != nil {
			b.WriteString(fmt.Sprintf("[%s]", ctx.GetValue(context.LogID)))
		}
	}
	b.WriteString(fmt.Sprintf("[%s:%d]", entry.Caller.File, entry.Caller.Line))
	b.WriteString(entry.Message)

	b.WriteString("\n")
	return b.Bytes(), nil
}
func Init() {
	var cfgData []byte
	var err error
	args := os.Args

	if len(args) >= 2 && args[len(args)-1] == "dev" {
		cfgData, err = os.ReadFile(ROOT_PATH + "./config/config.dev.json")
		if err != nil {
			return
		}
	} else {
		cfgData, err = os.ReadFile(ROOT_PATH + "./config/config.json")
		if err != nil {
			log.Errorf("config file not found,%s", err.Error())
			return
		}
	}

	err = json.Unmarshal(cfgData, &Instance)
	if err != nil {
		panic(err)
	}
	Instance.fixPath()
	if err := Instance.Validate(); err != nil {
		panic(err)
	}

	if len(Instance.Domains) == 0 && Instance.Domain != "" {
		Instance.Domains = []string{Instance.Domain}
	}

	if Instance.Domain != "" && Instance.IsInit {
		IsInit = true
	}

	// 设置日志格式为json格式
	log.SetFormatter(&logFormatter{})
	log.SetReportCaller(true)

	// 设置将日志输出到标准输出（默认的输出为stderr,标准错误）
	// 日志消息输出可以是任意的io.writer类型
	log.SetOutput(os.Stdout)

	var cstZone = time.FixedZone("CST", 8*3600)
	time.Local = cstZone
	if Instance != nil {
		switch Instance.LogLevel {
		case "":
			log.SetLevel(log.InfoLevel)
		case "debug":
			log.SetLevel(log.DebugLevel)
		case "info":
			log.SetLevel(log.InfoLevel)
		case "warn":
			log.SetLevel(log.WarnLevel)
		case "error":
			log.SetLevel(log.ErrorLevel)
		default:
			log.SetLevel(log.InfoLevel)
		}
	} else {
		log.SetLevel(log.InfoLevel)
	}

}

func ReadPrivateKey() (*ecdsa.PrivateKey, bool) {
	key, err := os.ReadFile(ROOT_PATH + "./config/ssl/account_private.pem")
	if err != nil {
		return createNewPrivateKey(), true
	}

	block, _ := pem.Decode(key)
	x509Encoded := block.Bytes
	privateKey, _ := x509.ParseECPrivateKey(x509Encoded)

	return privateKey, false
}

func createNewPrivateKey() *ecdsa.PrivateKey {
	// Create a user. New accounts need an email and private key to start.
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	x509Encoded, _ := x509.MarshalECPrivateKey(privateKey)

	// 将ec 密钥写入到 pem文件里
	keypem, _ := os.OpenFile(ROOT_PATH+"./config/ssl/account_private.pem", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	err = pem.Encode(keypem, &pem.Block{Type: "EC PRIVATE KEY", Bytes: x509Encoded})
	if err != nil {
		panic(err)
	}
	return privateKey
}

func WriteConfig(cfg *Config) error {
	bytes, _ := json.Marshal(cfg)
	_ = os.MkdirAll(ROOT_PATH+"/config/", 0700)
	err := os.WriteFile(ROOT_PATH+"./config/config.json", bytes, 0600)
	if err != nil {
		return errors.Wrap(err)
	}
	return nil
}

func ReadConfig() (*Config, error) {
	configData := Config{
		DkimPrivateKeyPath: ROOT_PATH + "config/dkim/dkim.priv",
		SSLPrivateKeyPath:  ROOT_PATH + "config/ssl/private.key",
		SSLPublicKeyPath:   ROOT_PATH + "config/ssl/public.crt",
	}
	if !file.PathExist(ROOT_PATH + "./config/config.json") {
		bytes, _ := json.Marshal(configData)
		_ = os.MkdirAll(ROOT_PATH+"/config/", 0700)
		err := os.WriteFile(ROOT_PATH+"./config/config.json", bytes, 0600)
		if err != nil {
			log.Errorf("Write Config Error:%s", err.Error())
			return nil, errors.Wrap(err)
		}
	} else {
		cfgData, err := os.ReadFile(ROOT_PATH + "./config/config.json")
		if err != nil {
			log.Errorf("Read Config Error:%s", err.Error())
			return nil, errors.Wrap(err)
		}

		err = json.Unmarshal(cfgData, &configData)
		configData.fixPath()
		if err != nil {
			log.Errorf("Read Config Unmarshal Error:%s", err.Error())
			return nil, errors.Wrap(err)
		}
	}
	return &configData, nil
}

func (c *Config) fixPath() {
	if c.DbType == DBTypeSQLite && !strings.HasPrefix(c.DbDSN, "/") {
		c.DbDSN = ROOT_PATH + c.DbDSN
	}
	if !strings.HasPrefix(c.SSLPublicKeyPath, "/") {
		c.SSLPublicKeyPath = ROOT_PATH + c.SSLPublicKeyPath
	}
	if !strings.HasPrefix(c.SSLPrivateKeyPath, "/") {
		c.SSLPrivateKeyPath = ROOT_PATH + c.SSLPrivateKeyPath
	}
}

// Validate 在启动前拒绝无效监听与代理配置，防止静默回退到全网卡。
func (c *Config) Validate() error {
	if c.IMAPBind == "" {
		c.IMAPBind = "127.0.0.1"
	}
	if c.POP3Bind == "" {
		c.POP3Bind = "127.0.0.1"
	}
	if c.IMAPPort == 0 {
		c.IMAPPort = 1993
	}
	if c.POP3Port == 0 {
		c.POP3Port = 1995
	}
	if net.ParseIP(c.IMAPBind) == nil || net.ParseIP(c.POP3Bind) == nil || c.IMAPPort < 1024 || c.IMAPPort > 65535 || c.POP3Port < 1024 || c.POP3Port > 65535 {
		return fmt.Errorf("invalid client TLS listener; use explicit IP and unprivileged port")
	}
	if c.EnableIMAP && c.EnablePOP3 && c.IMAPBind == c.POP3Bind && c.IMAPPort == c.POP3Port {
		return fmt.Errorf("client TLS listener conflict")
	}
	if c.HTTPBind == "" {
		c.HTTPBind = "127.0.0.1"
	}
	if c.SMTPBind == "" {
		c.SMTPBind = "127.0.0.1"
	}
	if c.SMTPPort == 0 {
		c.SMTPPort = 2525
	}
	if c.HttpPort == 0 {
		c.HttpPort = 18080
	}
	for _, addr := range []string{c.HTTPBind, c.SMTPBind} {
		if net.ParseIP(addr) == nil {
			return fmt.Errorf("listener requires an IP address")
		}
	}
	for _, port := range []int{c.HttpPort, c.SMTPPort} {
		if port < 1 || port > 65535 {
			return fmt.Errorf("invalid listener port")
		}
	}
	// 对同一地址或通配地址的端口复用提前失败，避免只启动部分服务。
	type endpoint struct {
		bind    string
		port    int
		enabled bool
	}
	endpoints := []endpoint{{c.HTTPBind, c.HttpPort, true}, {c.SMTPBind, c.SMTPPort, !c.DisableSMTP}, {c.IMAPBind, c.IMAPPort, c.EnableIMAP}, {c.POP3Bind, c.POP3Port, c.EnablePOP3}}
	for i, a := range endpoints {
		for _, b := range endpoints[i+1:] {
			if a.enabled && b.enabled && a.port == b.port && (a.bind == b.bind || net.ParseIP(a.bind).IsUnspecified() || net.ParseIP(b.bind).IsUnspecified()) {
				return fmt.Errorf("listener port conflict")
			}
		}
	}
	if c.ProxyRequired && len(c.ProxyTrustedIPs) == 0 {
		return fmt.Errorf("PROXY requires explicit trusted IPs")
	}
	for _, ip := range c.ProxyTrustedIPs {
		if net.ParseIP(ip) == nil {
			return fmt.Errorf("invalid trusted PROXY IP")
		}
	}
	if c.IsInit {
		origin, err := url.Parse(c.WebOrigin)
		if err != nil || origin.Host == "" || (origin.Scheme != "https" && origin.Scheme != "http") || origin.Path != "" || origin.RawQuery != "" || origin.User != nil || origin.Fragment != "" {
			return fmt.Errorf("webOrigin must be an explicit origin without path")
		}
		if c.InsecureLocalCookies {
			ip := net.ParseIP(origin.Hostname())
			if !net.ParseIP(c.HTTPBind).IsLoopback() || ip == nil || !ip.IsLoopback() || origin.Scheme != "http" {
				return fmt.Errorf("insecure cookies only allowed for loopback HTTP testing")
			}
		} else if origin.Scheme != "https" {
			return fmt.Errorf("secure deployment requires HTTPS origin")
		}
	}
	if c.SMTPHostname == "" {
		c.SMTPHostname = c.WebDomain
	}
	if c.SMTPHostname == "" {
		c.SMTPHostname = c.Domain
	}
	return nil
}
func (c *Config) HTTPAddress(port int) string {
	return net.JoinHostPort(c.HTTPBind, strconv.Itoa(port))
}
