# CPA 隐私脱敏插件

[English](README.md) | **简体中文**

`privacy-redactor` 是用于 CLIProxyAPI（CPA）的本地请求规范化插件，当前版本为 `0.1.0`。它在 CPA 进程内运行，处理发往 Claude 的请求正文，也适用于原生 Claude Code，不依赖 Cloak。

插件仅使用 Go 标准库，不读取凭据，不访问文件或网络，不记录请求内容。已验证的宿主为 CPA `8.0.32`（`911f49cb`），使用 C ABI `1` 和 JSON schema `6`。

## 脱敏范围

| 数据 | 处理方式 |
| --- | --- |
| 自动环境信息中的地区版系统名 | 例如将 `Windows 11 Home China` 简化为 `Windows 11` |
| 自动环境信息中的工作目录、home、用户名、主机名 | 替换为 `.`、`~`、`user`、`localhost` 等通用值 |
| 自动环境信息中的时区、locale、国家、地区、IP | 隐去已知字段的值 |
| metadata、JSON 格式的 user_id、diagnostics | 递归移除已知位置、主机和路径字段 |
| 原生 WebSearch 的 user_location | 移除该可选参数，保留搜索查询和域名过滤 |

保留平台、Shell、日期、用户正文、代码、普通工具输出、工具参数及 thinking 签名。设备、账户、会话 ID 的协议结构保持不变。

本插件不提供完整匿名化。用户主动发送的内容和工具结果仍可能包含个人信息；账户和会话标识仍可用于关联请求。插件只处理上述请求正文，不修改 HTTP 请求头，也不改变网络出口 IP。禁用、加载失败或绕过插件的请求不受这些规则保护。

## 安装

从仓库的 `v0.1.0` Release 下载 `privacy-redactor.so` 和 `SHA256SUMS`。当前预编译文件仅适用于 **Linux ARM64**；其他平台需要自行构建，并确认 CPA 宿主支持原生插件和对应 ABI。

```sh
sha256sum --ignore-missing -c SHA256SUMS
mkdir -p /path/to/cpa/plugins
cp privacy-redactor.so /path/to/cpa/plugins/privacy-redactor.so
```

先备份 CPA 配置，再将以下设置合并到现有配置中；保留已有插件配置。`dir` 使用你的实际插件目录，配置文件中只保留一个 `plugins` 节点。

```yaml
plugins:
  enabled: true
  dir: /path/to/cpa/plugins
  configs:
    privacy-redactor:
      enabled: true
      priority: -10000
```

按宿主的插件加载流程加载插件。如首次加载没有生效，在维护时段重启 CPA。管理界面应显示 `Claude privacy redactor`。

状态接口为 `GET /v0/management/privacy-redactor/status`，需要 CPA 管理凭据，只返回处理次数，不返回请求内容。停用时在管理界面关闭插件，或将 `plugins.configs.privacy-redactor.enabled` 设为 `false` 并让宿主应用配置。

## 构建

需要 Go `1.23` 或更新版本，以及与目标平台匹配的 C 编译器。在 Linux 上为本机架构构建：

```sh
mkdir -p dist
CGO_ENABLED=1 go build -trimpath -buildmode=c-shared -ldflags='-s -w' -o dist/privacy-redactor.so .
```

在 Linux AMD64 上交叉构建 ARM64，需要 `aarch64-linux-gnu-gcc`：

```sh
mkdir -p dist/linux-arm64
GOOS=linux GOARCH=arm64 CGO_ENABLED=1 CC=aarch64-linux-gnu-gcc \
  go build -trimpath -buildmode=c-shared -ldflags='-s -w' \
  -o dist/linux-arm64/privacy-redactor.so .
```

`build_linux.sh` 保留初次部署使用的隔离构建流程，需要传入 Linux AMD64 Go 工具链归档和项目目录，并预先安装 `gcc`、ARM64 交叉编译器。它会运行 race 检查和 vet，然后构建 ARM64 共享库：

```sh
sh ./build_linux.sh /path/to/go.linux-amd64.tar.gz "$(pwd)"
```

## 验证

```sh
go test ./...
CGO_ENABLED=1 go test -race ./...
go vet ./...
python3 abi_smoke.py ./dist/privacy-redactor.so
```

C ABI 验证必须在可以加载该共享库的目标架构上执行。初次部署已通过单元测试、race 检查、vet、ARM64 C ABI 并发调用、CPA 隔离调用，以及真实 Claude Code 文件读取和 WebSearch 验证。仓库不包含生产凭据、真实请求、服务器信息或部署日志。

Release 中的 ARM64 二进制与初次部署文件一致，其 SHA-256 为：

```text
d556a0081801af3a219ac85a4ec8dd6257e66373eb65f958a86cc58a838d5abb  privacy-redactor.so
```

宿主版本或插件 ABI 升级后需要重新验证。不同工具链产生的二进制哈希可能不同。

## 源码

- `normalizer.go`：请求正文脱敏和状态计数。
- `bridge.go`：语言无关的 C ABI。
- `normalizer_test.go`：协议字段保留、脱敏范围与并发测试。
- `abi_smoke.py`：通过 ctypes 调用共享库的合成请求验证。
- `config.example.yaml`：最小插件配置。
