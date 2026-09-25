#!/bin/sh
set -eu

repo="yldst-dev/codex2api"
prefix="${PREFIX:-/usr/local}"
service=0
if [ "${1:-}" = "--service" ]; then
  service=1
fi

os=$(uname -s)
arch=$(uname -m)
case "$os-$arch" in
  Linux-x86_64) asset="codex-gateway-linux-amd64" ;;
  Linux-aarch64|Linux-arm64) asset="codex-gateway-linux-arm64" ;;
  Darwin-arm64) asset="codex-gateway-darwin-arm64" ;;
  Darwin-x86_64) asset="codex-gateway-darwin-amd64" ;;
  *)
    echo "unsupported platform: $os $arch" >&2
    exit 1
    ;;
esac

if [ "$service" -eq 1 ] && [ "$(id -u)" -ne 0 ]; then
  echo "run as root for --service" >&2
  exit 1
fi

workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT
base="https://github.com/${repo}/releases/latest/download"
curl -fsSL "$base/$asset" -o "$workdir/codex-gateway"
curl -fsSL "$base/SHA256SUMS" -o "$workdir/SHA256SUMS"
(
  cd "$workdir"
  if command -v sha256sum >/dev/null 2>&1; then
    grep "  ${asset}$" SHA256SUMS | sed "s/  ${asset}$/  codex-gateway/" | sha256sum -c -
  else
    grep "  ${asset}$" SHA256SUMS | sed "s/  ${asset}$/  codex-gateway/" | shasum -a 256 -c -
  fi
)

if [ "$(id -u)" -eq 0 ]; then
  install -m 0755 "$workdir/codex-gateway" "$prefix/bin/codex-gateway"
  echo "installed $prefix/bin/codex-gateway"
else
  mkdir -p "$HOME/.local/bin"
  install -m 0755 "$workdir/codex-gateway" "$HOME/.local/bin/codex-gateway"
  echo "installed $HOME/.local/bin/codex-gateway"
fi

if [ "$service" -ne 1 ]; then
  echo "run 'codex-gateway server start' and open the admin link it prints"
  exit 0
fi

if ! id codex-gateway >/dev/null 2>&1; then
  useradd --system --home /var/lib/codex-gateway --shell /usr/sbin/nologin codex-gateway
fi
mkdir -p /var/lib/codex-gateway
chown codex-gateway:codex-gateway /var/lib/codex-gateway
chmod 700 /var/lib/codex-gateway
for unit in codex-gateway.service codex-gateway-update.service codex-gateway-update.path; do
  curl -fsSL "https://raw.githubusercontent.com/${repo}/main/deploy/${unit}" \
    -o "/etc/systemd/system/${unit}"
done
systemctl daemon-reload
systemctl enable codex-gateway >/dev/null 2>&1
systemctl enable --now codex-gateway-update.path >/dev/null 2>&1
systemctl restart codex-gateway

admin=$(sed -n 's/^CODEX_GATEWAY_ADMIN_LISTEN=//p' /etc/codex-gateway.env 2>/dev/null | tail -n 1)
admin=${admin:-127.0.0.1:8081}
token_file=/var/lib/codex-gateway/setup.token
waited=0
while [ "$waited" -lt 15 ]; do
  if systemctl is-active --quiet codex-gateway && [ -s "$token_file" ]; then
    break
  fi
  sleep 1
  waited=$((waited + 1))
done

if ! systemctl is-active --quiet codex-gateway; then
  systemctl --no-pager --full status codex-gateway || true
  echo "codex-gateway did not start. Check: journalctl -u codex-gateway" >&2
  exit 1
fi

echo
echo "codex-gateway is running."
echo
if [ "$admin" = "off" ]; then
  echo "The admin page is off (CODEX_GATEWAY_ADMIN_LISTEN=off)."
  exit 0
fi
if [ -s "$token_file" ]; then
  echo "Open this link to set the admin password and finish setup:"
  echo
  echo "  http://${admin}/#setup=$(tr -d '[:space:]' < "$token_file")"
else
  echo "Admin page:"
  echo
  echo "  http://${admin}/"
fi
echo
echo "From another computer, open an SSH tunnel first and use the same link:"
echo
echo "  ssh -L ${admin##*:}:127.0.0.1:${admin##*:} -L 1455:127.0.0.1:1455 user@this-server"
