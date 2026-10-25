# 构建与部署说明

本分支基于PMail v2.9.7（a6049bf0523cabb33a28c73e08bccf46f90b4e5f），补充可配置监听、可信PROXYv2、Web会话和口令安全修正。前端内嵌Go可执行文件，SQLite持久化。

## 构建macOS版本

```sh
GO_BIN=/path/to/go1.26.10/bin/go sh scripts/build-macos.sh
```

脚本要求Go1.26.10和Yarn，构建darwin/amd64、GOAMD64=v1、CGO_ENABLED=0，输出`output/pmail-darwin-amd64`。目标机器无需安装Go、Node或Yarn。产物版本为`v2.9.7-local.2`，提交号自动取当前Git HEAD；工作区存在未提交内容时追加`+dirty`，正式发布应从干净工作区构建。工具链升级前应核查最低操作系统要求并在目标平台验证。

前端使用Vite构建；成功打包不代表通过TypeScript类型检查。

## 运行目录与进程管理

运行目录使用`bin/pmail`、`config/config.json`、`config/ssl/`、`config/dkim/`、`data/pmail.db`、`logs/`和`runtime/`。将`scripts/mailctl.py`复制到运行目录根部后执行：

```sh
python3 mailctl.py status
python3 mailctl.py start
python3 mailctl.py stop
```

脚本只管理本目录记录且身份匹配的PID，启动前检查端口，不杀占用者，不提供强制停止。使用普通用户运行，文件权限掩码为077。`status`停止时也返回0，应根据输出`running pid=...`或`stopped`判断；它不提供健康检查或自动恢复。

默认`GOMAXPROCS=2`、`GOMEMLIMIT=128MiB`，可通过调用者环境覆盖；后者为Go软内存目标，不是RSS硬限制。该脚本使用macOS的`ps`格式，其他平台使用前需适配验证。

## 配置字段

以下端口仅为默认值和示例，按部署环境设置：

- `httpBind`、`smtpBind`：显式IP，默认127.0.0.1。
- `httpPort`、`smtpPort`：默认18080、2525。
- `smtpHostname`：SMTP招呼名，与邮箱后缀`domain`/`domains`分别配置。
- `proxyRequired=true`及`proxyTrustedIPs`：强制PROXYv2/TCP并限制直接来源；仅在可信代理确实发送该头时启用，普通SMTP客户端无法直连此入口。
- `httpsEnabled=2`：内置HTTP，可配合HTTPS反向代理使用。实际监听范围和入口由部署者决定。
- `enableSubmission`：仅启用独立隐式TLS的SMTP客户端提交，不再捆绑587。`submissionBind`/`submissionPort`默认127.0.0.1:1465，要求显式IP及非特权端口；公网465可经可信TCP转发，不能添加入站SMTP专用的PROXY头。
- `enablePOP3`、`enableIMAP`：控制对应隐式TLS监听。`smtpAuthEnabled`为旧兼容字段，入站AUTH始终禁用；SMTP提交只接受SMTP用途授权码，不能通过此字段开启网页登录密码认证。
- `clientHost`及`clientIMAPPort`/`clientPOP3Port`/`clientSMTPPort`：网页教程使用的对外参数，端口默认993/995/465。主机名未设时沿用smtpHostname、webDomain或domain；这些字段只展示教程，不创建监听或转发。非标准端口部署必须显式填写对外参数。
- 入站策略只接收本域真实账号及启用别名；SMTP提交要求授权码认证、账号发件身份和生命周期复验。不要使用入站25作为客户端发送端口。
- `webOrigin`：精确匹配浏览器origin，包括非默认端口。POST要求同源JSON。
- `insecureLocalCookies=false`：Cookie使用Secure/HttpOnly/SameSite=Strict；仅回环HTTP调试允许true，且`webOrigin`必须为回环IP的HTTP地址。
- `sslType="1"`：使用证书文件，不启用ACME签发。替换证书后应受控重启并分别验证HTTPS和SMTP证书；不要依赖定时证书检查及时重载。

## 安全与兼容性

口令使用Argon2id v19、32MiB、3轮、1线程及16字节随机盐，最多2个并发计算；Web登录桶初始10次、每3秒补1次。登录轮换会话；API请求复验数据库角色、禁用状态和口令版本，改密撤销旧会话。旧MD5口令、APOP和Token头认证不兼容，应使用全新初始化账号或另行设计迁移。

SMTP仅接收已启用的本域账号及已登记启用别名，未知地址拒收且不转交管理员，RSET清空事务信封；PROXY头读取超时3秒，TLS最低1.2。出站隐式TLS验证服务器证书。协议开关及本地回归不等于完整安全审计或外发送达率验收。

凭据、证书私钥、真实运行配置、数据库及部署报告不应提交。上游历史自带示例私钥，不能用于生产。

## 本地回归

先在`fe`目录运行`yarn install --frozen-lockfile --non-interactive`，再使用固定Go工具链运行：

```sh
node scripts/test-empty-post.cjs
cd server
go test ./config ./utils/password ./listen/http_server ./utils/smtp
go test ./listen/smtp_server -run 'Test(Proxy|InboundRecipientAndReset|Outgoing|IncomingAudit|AcceptedDelivery|TemporaryDelivery|PermanentDelivery|SameMessageID|SendAfterHook|ReceiveHooksAndDatabase|SaveSentEmail|DownstreamDelivery|DeliverySMTP)'
```

回归使用临时SQLite、回环监听和合成内容。上游部分集成测试依赖真实配置、DNS或外发环境，运行前应检查副作用，不能直接将`go test ./...`视为离线检查。

通用问题说明见[开发说明](development-notes.md)。

邮箱别名使用方法、接口和升级边界见[邮箱别名说明](email-aliases.md)。

IMAP/POP3的TLS端口、统一用途授权码、证书、转发和升级回退见[客户端协议说明](client-protocols.md)。默认关闭，开启需明确配置；不开放110/143明文监听。


## 客户端与服务器间投递的端口边界

| 服务 | 默认本机监听 | 推荐对外端口 | 加密与认证 |
|---|---|---|---|
| IMAP | 127.0.0.1:1993 | 993 | 隐式TLS；完整邮箱＋IMAP用途码 |
| POP3 | 127.0.0.1:1995 | 995 | 隐式TLS；完整邮箱＋POP3用途码 |
| SMTP提交 | 127.0.0.1:1465 | 465 | 隐式TLS；完整邮箱＋SMTP用途码 |
| SMTP入站 | 127.0.0.1:2525 | 25 | 服务器间投递，支持机会式STARTTLS，不提供AUTH |
| HTTP | 127.0.0.1:18080 | HTTPS反向代理自定 | Web及HTTP API，不直接暴露HTTP |

客户端不提供110/143/587；TLS最低1.2，证书必须覆盖客户端使用的主机名。465与25是不同用途，不能替换MX投递端口25。SMTP提交的信封收件人决定实际投递，To/Cc/Bcc不能隐式添加收件人，发送前先保存审计，沿用原有结果记录和错误返回。

证书替换后按维护流程重启PMail，分别核验全部TLS监听；HTTP健康检查不能替代协议健康检查。开放端口和认证成功不代表外发出口或SPF/DKIM/DMARC已具备，必须另做授权验收。
