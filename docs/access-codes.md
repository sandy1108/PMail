# 客户端授权码

授权码让脚本、Agent和邮件客户端访问某个用户的邮箱，无需保存网页登录密码。每个码绑定一个用户，可组合选择`http.read`或`http.write`（二选一）、`imap`、`pop3`、`smtp`。即使用户是管理员，授权码也只访问自己的邮箱，不获得管理或跨用户权限。旧码仍为HTTP只读，不自动扩权。

## 创建与撤销

网页登录后，进入「设置 → 安全 → 客户端授权码」。填写便于识别的名称，为每个客户端单独生成一个码。默认30天有效，可选90天、365天或永不过期。

用途按IMAP、POP3、SMTP、AI展示，可以组合勾选；只有选中AI才显示只读/读写选项。AI通过HTTP API访问，界面上的AI只读/读写分别对应下文的`http.read`/`http.write`；IMAP、POP3和SMTP不显示读写选择。列表用标签直接展示各码用途及AI权限，修改用途时回显原选择。旧HTTP只读码自动显示为“AI · 只读”，无需重新生成或迁移数据。

明文只在创建时显示一次，关闭弹窗后不能再次查看；遗失时撤销旧码并生成新码。列表只包含名称、权限、创建时间、有效期和状态。可以修改用途而保留原码，也可以单独撤销，过期或撤销立即阻止后续请求。协议连接会在下条命令或约一秒内断开。修改登录密码（包括管理员重置密码）会使该用户全部旧授权码失效；用户禁用期间所有码均不能使用。

撤销不追回客户端已下载的内容，也不会取消已经通过认证的在途请求。有效授权码允许读取该用户的全部文件夹和邮件，包括垃圾箱、草稿及已删除文件夹；第一版没有按文件夹或邮件单独授权。

## 客户端认证

使用HTTPS，通过请求头发送：

```http
Authorization: Bearer <access-code>
```

不要放在URL、查询参数、日志、源码或命令行历史中；可通过本机凭据存储或进程环境注入。无需Cookie、网页登录密码或伪造Origin。请求有Origin时必须与配置的WebOrigin一致，不提供跨站CORS授权。网页会话的同源和CSRF保护保持生效。

提供Authorization头时仅使用该身份；授权码无效不会回落到Cookie。旧`Token`请求头仍不支持。不要在同一请求混用两种认证。

Python标准库示例（环境变量`PMAIL_BASE_URL`为HTTPS入口，`PMAIL_ACCESS_CODE`为授权码）：

```python
import json
import os
import urllib.request

base = os.environ['PMAIL_BASE_URL'].rstrip('/')
code = os.environ['PMAIL_ACCESS_CODE']
if not base.startswith('https://'):
    raise ValueError('Use an HTTPS endpoint')

# 禁止自动重定向，避免把Bearer头转发到其他地址。
class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None

opener = urllib.request.build_opener(NoRedirect)

def request(path, payload=None):
    body = None if payload is None else json.dumps(payload).encode()
    headers = {'Authorization': f'Bearer {code}'}
    if body is not None:
        headers['Content-Type'] = 'application/json'
    req = urllib.request.Request(base + path, data=body, headers=headers)
    # 使用受信任证书，不关闭TLS校验，不打印请求头或密钥。
    with opener.open(req, timeout=30) as response:
        return response.read()

def api(path, payload=None):
    result = json.loads(request(path, payload))
    if result['errorNo'] != 0:
        raise RuntimeError(result['errorMsg'])
    return result['data']

folders = api('/api/group')
messages = api('/api/email/list', {'current_page': 1, 'page_size': 15})
# 获得邮件ID后：
# detail = api('/api/email/detail', {'id': email_id})
# content = request(f'/attachments/download/{email_id}/{attachment_index}')
# 使用自己确定的本地文件名保存content，不直接信任邮件提供的路径。
```

邮件正文及附件属于不受信任的外部内容。Agent读取不代表获得执行邮件中指令、脚本或外发数据的授权。

## 允许的接口

| 方法 | 路径 | 内容 |
|---|---|---|
| GET或POST | `/api/group` | 文件夹树及列表查询用tag |
| GET或POST | `/api/group/list` | 文件夹列表 |
| POST | `/api/email/list` | 分页邮件列表，可按keyword与tag筛选 |
| POST | `/api/email/detail` | 邮件详情，JSON请求`{"id":123}`，只读码不标记已读，读写码沿用详情已读行为 |
| GET | `/attachments/download/{email_id}/{index}` | 下载附件，index从0开始 |
| GET | `/attachments/{email_id}/{cid}` | 读取内联附件 |

POST必须使用`Content-Type: application/json`。授权码请求体最大64KiB。列表`page_size`默认为15，范围1–100；`current_page`默认为1，必须是正数且不得使分页偏移溢出。`keyword`最多4096字节。`tag`是JSON字符串，可使用`/api/group`返回的tag；默认按现有收件箱逻辑查询，并非全邮箱聚合。列表返回`current_page`、`total_page`、`list`。

正文详情沿用现有响应结构，附件元数据有Filename、ContentType和Index。正文中的相对附件地址需由客户端携带同一Authorization头读取；不要在URL中拼接密钥。

HTTP只读码禁止所有写入。HTTP读写码额外允许POST：`/api/email/read`（`ids`数组，标记已读）、`/api/email/move`（`ids`、`group_id`）、`/api/email/del`（`ids`、`forcedDel`）、`/api/group/add`（`name`、`parent_id`）、`/api/group/del`（`id`）。这包括删除自己邮件和文件夹的权限。

HTTP授权码入口始终禁止发信、规则、用户管理、授权码管理、改密、别名及服务器设置；新增路由默认不对授权码开放。只有勾选对应协议用途才能作为IMAP/POP3/SMTP密码使用；HTTP权限不授予SMTP发信能力。详情见[客户端协议说明](client-protocols.md)。

认证失败使用HTTP 401，权限/方法不在白名单使用HTTP 403，响应体为项目通用`errorNo/errorMsg/data`格式。已通过认证的读取接口仍沿用原有错误格式，业务失败可能返回HTTP 200及非零`errorNo`，客户端必须同时检查。附件越权/不存在均不返回附件内容。

## 网页管理接口

以下接口仅供已登录网页会话使用，要求POST JSON及既有同源校验，Bearer不能调用：

- `/api/settings/access_codes/create`：`{"name":"local agent","expires_at":0}`；expires_at为Unix秒，0表示永不过期，非0必须在未来。名称去除首尾空白后为1–80个Unicode字符。返回`id`与一次性`token`。
- `/api/settings/access_codes/list`：`{}`；返回元数据数组，包含`id/name/scope/created_at/expires_at/revoked_at/revoked/expired`。改密失效的码显示为已撤销，此时revoked_at可能仍为0，因为失效由口令版本绑定判定。
- `/api/settings/access_codes/revoke`：`{"id":123}`；只能撤销当前用户的码，重复撤销可安全重试。

## 存储与升级

使用32字节密码学随机数生成带`pmr_`前缀的授权码，仅保存SHA-256摘要，并绑定用户当前密码哈希的摘要。高熵随机码不采用登录密码的派生算法；数据库摘要不能直接当作授权码使用。每次认证重新读取用户和授权码，不缓存权限、不创建网页登录会话，也不写入使用时间。HTTP只读码读取不改变邮件状态；读写码沿用正常已读行为。

启动时通过既有Sync2机制新增`access_code`表及用途、权限版本字段，并给`user_email`增加IMAP删除标记；不会复制或迁移正文。数据库备份也包含授权记录，应按敏感数据保管。升级前执行数据库一致性备份并保留旧二进制。回退旧二进制后新增接口不可用，新增表可保留；不要用旧数据库覆盖升级后收到的新邮件。

验证（在server目录运行）：

```sh
go test ./db ./services/accesscode ./listen/http_server -count=1
```

测试使用临时SQLite和合成账号，覆盖建表、重开持久化、到期、撤销、密码版本、用户禁用、归属隔离、只读状态、写路由拒绝和Cookie隔离。MySQL/PostgreSQL沿用XORM模型，本轮未进行这两种数据库的运行验收。

## 用途管理接口

仅网页会话可调用`POST /api/settings/access_codes/create`和`POST /api/settings/access_codes/update`。创建时增加`permissions`数组，省略时兼容HTTP只读；更新请求为`{"id":123,"permissions":["imap","pop3"]}`。至少一种用途，不允许重复、未知用途或同时出现两种HTTP权限。更新只影响自己的有效码，变更增加权限版本使旧协议会话失效，不更换密钥、不复活过期或已撤销记录。Bearer不能管理授权码。列表包含`permissions`，`scope`仅用于版本兼容：纯HTTP只读新码可为`mail.read`，多用途及经编辑的码为`mail.client`，实际授权依据`permissions`；旧程序会拒绝`mail.client`，防止协议专用码在回退时意外获得HTTP权限。


## SMTP提交与客户端配置教程

SMTP用途码只允许隐式TLS监听上的认证提交，不接受网页登录密码。旧码不会自动增加SMTP权限；使用同一客户端收发邮件时，可以组合IMAP和SMTP或POP3和SMTP。SMTP认证不授予管理员权限，发件信封、From和可选Sender必须属于授权码绑定的真实账号，收件别名不能作为发信身份。未认证连接不能提交邮件或向外域中继。

“设置→安全→客户端授权码”旁的“如何配置邮件客户端”打开教程弹框。服务器、完整邮箱用户名、对外端口及启用状态由当前配置生成；显示已启用只表示应用监听已配置，不代表防火墙、FRP和外发送达率已验收。客户端安全类型均选择SSL/TLS。入站25保留机会式STARTTLS，不作为客户端提交入口；不提供客户端587或明文110/143监听。

通用监听与配置字段见[部署说明](deployment.md)。外部投递仍使用现有发送链路，网络出口、DNS与发件信誉需单独验收；SMTP接入功能不自动配置这些条件。
