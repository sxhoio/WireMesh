package control

import (
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
)

const agentInstallerScript = `#!/usr/bin/env bash
set -euo pipefail

SERVER=__WIREMESH_SERVER__
TOKEN=""
PROJECT=""
NETWORK=""
NAME=""
LABELS=""
INTERFACES="auto"
REPORT_INTERVAL="10s"
PROBE_INTERVAL="15s"
USE_MTLS=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --server) SERVER="$2"; shift 2 ;;
    --token) TOKEN="$2"; shift 2 ;;
    --project) PROJECT="$2"; shift 2 ;;
    --network) NETWORK="$2"; shift 2 ;;
    --name) NAME="$2"; shift 2 ;;
    --labels) LABELS="$2"; shift 2 ;;
    --interfaces) INTERFACES="$2"; shift 2 ;;
    --report-interval) REPORT_INTERVAL="$2"; shift 2 ;;
    --probe-interval) PROBE_INTERVAL="$2"; shift 2 ;;
    --mtls) USE_MTLS="true"; shift ;;
    --no-mtls) USE_MTLS="false"; shift ;;
    *) echo "未知参数: $1" >&2; exit 2 ;;
  esac
done

if [ "$(id -u)" -ne 0 ]; then
  echo "请以 root 运行安装脚本（推荐通过 sudo bash 执行）" >&2
  exit 1
fi
if [ -z "$TOKEN" ] || [ -z "$NAME" ]; then
  echo "缺少必要参数：--token 和 --name" >&2
  exit 2
fi
if [ -z "$SERVER" ]; then
  echo "安装脚本无法确定 WireMesh 服务地址，请通过 --server 手动指定" >&2
  exit 2
fi
case "$SERVER$NAME$LABELS$PROJECT$NETWORK" in
  *$'\n'*|*$'\r'*) echo "参数中不能包含换行符" >&2; exit 2 ;;
esac
SERVER="${SERVER%/}"
if [ -z "$USE_MTLS" ]; then
  case "$SERVER" in
    https://*) USE_MTLS="true" ;;
    *) USE_MTLS="false" ;;
  esac
fi

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  linux) ;;
  *) echo "一键安装当前仅支持 Linux，其他系统请使用手动安装" >&2; exit 1 ;;
esac

MACHINE="$(uname -m)"
case "$MACHINE" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "不支持的 CPU 架构: $MACHINE" >&2; exit 1 ;;
esac

install_wireguard() {
  if command -v wg >/dev/null 2>&1 && command -v wg-quick >/dev/null 2>&1 && command -v ip >/dev/null 2>&1; then
    echo "WireGuard 工具已安装。"
    return
  fi

  echo "正在安装 WireGuard 工具..."
  if command -v apt-get >/dev/null 2>&1; then
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y wireguard-tools iproute2
  elif command -v dnf >/dev/null 2>&1; then
    dnf install -y wireguard-tools iproute
  elif command -v yum >/dev/null 2>&1; then
    yum install -y wireguard-tools iproute
  elif command -v apk >/dev/null 2>&1; then
    apk add --no-cache wireguard-tools iproute2
  elif command -v pacman >/dev/null 2>&1; then
    pacman -Sy --noconfirm wireguard-tools iproute2
  elif command -v zypper >/dev/null 2>&1; then
    zypper --non-interactive install wireguard-tools iproute2
  else
    echo "无法识别系统包管理器，请先手动安装 wg、wg-quick 和 ip 命令。" >&2
    exit 1
  fi

  for command_name in wg wg-quick ip; do
    if ! command -v "$command_name" >/dev/null 2>&1; then
      echo "WireGuard 安装不完整，缺少命令: $command_name" >&2
      exit 1
    fi
  done
}

install_wireguard
install -d -m 0700 /etc/wireguard

TMP_FILE="$(mktemp)"
trap 'rm -f "$TMP_FILE"' EXIT
curl -fL "$SERVER/agent/download?os=$OS&arch=$ARCH" -o "$TMP_FILE"
install -m 0755 "$TMP_FILE" /usr/local/bin/wiremesh-agent

install -d -m 0700 /var/lib/wiremesh-agent /etc/wiremesh-agent
umask 077
printf '%s' "$TOKEN" > /etc/wiremesh-agent/enrollment-token

escape_env() {
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}
cat > /etc/wiremesh-agent/agent.env <<EOF
WIREMESH_SERVER="$(escape_env "$SERVER")"
WIREMESH_NAME="$(escape_env "$NAME")"
WIREMESH_LABELS="$(escape_env "$LABELS")"
WIREMESH_INTERFACES="$(escape_env "$INTERFACES")"
WIREMESH_REPORT_INTERVAL="$(escape_env "$REPORT_INTERVAL")"
WIREMESH_PROBE_INTERVAL="$(escape_env "$PROBE_INTERVAL")"
WIREMESH_MTLS="$(escape_env "$USE_MTLS")"
EOF
chmod 0600 /etc/wiremesh-agent/agent.env

cat > /etc/systemd/system/wiremesh-agent.service <<'EOF'
[Unit]
Description=WireMesh Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=/etc/wiremesh-agent/agent.env
ExecStart=/usr/local/bin/wiremesh-agent --server "${WIREMESH_SERVER}" --token-file /etc/wiremesh-agent/enrollment-token --state-dir /var/lib/wiremesh-agent --name "${WIREMESH_NAME}" --labels "${WIREMESH_LABELS}" --interfaces "${WIREMESH_INTERFACES}" --report-interval "${WIREMESH_REPORT_INTERVAL}" --probe-interval "${WIREMESH_PROBE_INTERVAL}" --mtls="${WIREMESH_MTLS}"
Restart=on-failure
RestartSec=5s
UMask=0077
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/wiremesh-agent /etc/wiremesh-agent /etc/wireguard

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now wiremesh-agent.service

echo "WireMesh Agent 已安装并启动。"
echo "Agent 将自动上报公网 IP，并由 WireMesh GeoIP 数据库解析地理位置。"
echo "查看状态: systemctl status wiremesh-agent --no-pager"
`

func (a *App) agentInstallScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	script := strings.Replace(agentInstallerScript, "__WIREMESH_SERVER__", shellSingleQuote(agentInstallerServerURL(r)), 1)
	_, _ = io.WriteString(w, script)
}

const agentUninstallerScript = `#!/usr/bin/env bash
set -euo pipefail

systemctl disable --now wiremesh-agent.service 2>/dev/null || true
rm -f /etc/systemd/system/wiremesh-agent.service
systemctl daemon-reload
systemctl reset-failed wiremesh-agent.service 2>/dev/null || true
rm -f /usr/local/bin/wiremesh-agent
rm -rf /var/lib/wiremesh-agent /etc/wiremesh-agent

echo "WireMesh Agent 已卸载"
echo "节点在面板中的历史记录仍会保留，可稍后重新运行接入命令部署"
`

func (a *App) agentUninstallScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, agentUninstallerScript)
}

func agentInstallerServerURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwardedScheme := agentInstallerForwardedScheme(r); forwardedScheme != "" {
		scheme = forwardedScheme
	}
	host := firstForwardedHeaderValue(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	host = strings.ReplaceAll(strings.ReplaceAll(host, "\r", ""), "\n", "")
	return scheme + "://" + host
}

func agentInstallerForwardedScheme(r *http.Request) string {
	if value := strings.ToLower(firstForwardedHeaderValue(r.Header.Get("X-Forwarded-Proto"))); value == "http" || value == "https" {
		return value
	}
	forwarded := firstForwardedHeaderValue(r.Header.Get("Forwarded"))
	for _, part := range strings.Split(forwarded, ";") {
		key, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if found && strings.EqualFold(key, "proto") {
			value = strings.ToLower(strings.Trim(strings.TrimSpace(value), "\""))
			if value == "http" || value == "https" {
				return value
			}
		}
	}
	for _, header := range []string{"X-Forwarded-Ssl", "Front-End-Https"} {
		if value := strings.ToLower(firstForwardedHeaderValue(r.Header.Get(header))); value == "on" || value == "true" || value == "1" {
			return "https"
		}
	}
	if firstForwardedHeaderValue(r.Header.Get("X-Forwarded-Port")) == "443" {
		return "https"
	}
	return ""
}

func firstForwardedHeaderValue(value string) string {
	if comma := strings.IndexByte(value, ','); comma >= 0 {
		value = value[:comma]
	}
	return strings.TrimSpace(value)
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func (a *App) agentDownload(w http.ResponseWriter, r *http.Request) {
	configuredPath := strings.TrimSpace(a.agentBinaryPath)
	if configuredPath == "" {
		writeError(w, http.StatusServiceUnavailable, "agent binary is not configured on this control plane")
		return
	}
	requestedOS, requestedArch := r.URL.Query().Get("os"), r.URL.Query().Get("arch")
	if requestedOS != "linux" || (requestedArch != "amd64" && requestedArch != "arm64") {
		writeError(w, http.StatusNotFound, "agent binary is not available for the requested platform")
		return
	}
	binaryPath := configuredPath
	if strings.Contains(binaryPath, "{os}") || strings.Contains(binaryPath, "{arch}") {
		binaryPath = strings.ReplaceAll(binaryPath, "{os}", requestedOS)
		binaryPath = strings.ReplaceAll(binaryPath, "{arch}", requestedArch)
	} else if requestedOS != runtime.GOOS || requestedArch != runtime.GOARCH {
		writeError(w, http.StatusNotFound, "agent binary is not available for the requested platform")
		return
	}
	info, err := os.Stat(binaryPath)
	if err != nil || info.IsDir() {
		writeError(w, http.StatusServiceUnavailable, "agent binary is unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="wiremesh-agent-`+requestedOS+"-"+requestedArch+`"`)
	w.Header().Set("Cache-Control", "public, max-age=300")
	http.ServeFile(w, r, binaryPath)
}
