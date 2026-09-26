# codex-gateway

Ubuntu 서버에서 OpenAI Codex OAuth 로그인을 끝낸 뒤, 그 자격 증명을 서버 안에 두고, 별도의 Gateway API Key로 Codex Responses 요청을 대신 보내는 작은 게이트웨이입니다.

설치 스크립트를 한 번 실행한 뒤에는 브라우저 관리 화면에서 Codex 로그인, API 키 발급, 수신 주소 변경까지 모두 할 수 있습니다. 바꾼 값은 재시작 없이 바로 적용됩니다. 터미널이 편하면 인자 없이 `codex-gateway`를 실행해 방향키 메뉴를 써도 되고, 스크립트에서는 아래 하위 명령을 그대로 씁니다.

하는 일은 다음과 같습니다.

- Codex OAuth 로그인
- access token 자동 갱신
- Gateway API Key 발급과 폐기
- Codex Responses API 프록시
- 위 설정을 모두 하는 브라우저 관리 화면

회원가입, 결제, 그룹, Redis, PostgreSQL은 없습니다. 저장소는 SQLite 하나입니다.

OAuth 상수와 토큰 교환, 기본 instructions 문구는 [sub2api](https://github.com/Wei-Shaw/sub2api) `20a94fbb567b62208751292ed7786b24a7e7c0fe`에서 가져왔습니다. 라이선스는 LGPL-3.0 입니다. 자세한 출처는 `NOTICE`에 있습니다.

## 설치

릴리스 파일을 받는 방법이 가장 짧습니다. Linux amd64, Linux arm64, macOS amd64, macOS arm64 실행 파일이 [릴리스](https://github.com/yldst-dev/codex2api/releases/latest)에 있습니다.

```bash
curl -fsSL https://raw.githubusercontent.com/yldst-dev/codex2api/main/scripts/install-release.sh | sudo sh
```

이 명령은 이 기기에 맞는 최신 파일을 받아 SHA256SUMS로 확인한 뒤 `/usr/local/bin/codex-gateway`에 넣습니다. root가 아니면 `$HOME/.local/bin`에 넣습니다.

Ubuntu에서 계정, 데이터 디렉터리, systemd 서비스까지 만들려면 다음을 씁니다.

```bash
curl -fsSL https://raw.githubusercontent.com/yldst-dev/codex2api/main/scripts/install-release.sh | sudo sh -s -- --service
```

끝나면 관리자 비밀번호를 정하는 1회용 링크가 출력됩니다. 그 링크를 열면 나머지 설정은 [관리 화면](#관리-화면)에서 합니다.

```text
Open this link to set the admin password and finish setup:

  http://127.0.0.1:8081/#setup=...
```

소스에서 직접 빌드할 때는 Go 1.24 이상과 Node.js 24 이상이 필요합니다. 관리 화면을 먼저 빌드해야 실행 파일 안에 들어갑니다.

```bash
git clone https://github.com/yldst-dev/codex2api.git
cd codex2api
npm --prefix web ci
npm --prefix web run build
CGO_ENABLED=0 go build -o codex-gateway ./cmd/codex-gateway
sudo install -m 0755 codex-gateway /usr/local/bin/codex-gateway
```

## Ubuntu 설치 예시

```bash
sudo useradd --system --home /var/lib/codex-gateway --shell /usr/sbin/nologin codex-gateway
sudo mkdir -p /var/lib/codex-gateway
sudo chown codex-gateway:codex-gateway /var/lib/codex-gateway
sudo chmod 700 /var/lib/codex-gateway
sudo install -m 0755 codex-gateway /usr/local/bin/codex-gateway
```

데이터 디렉터리와 마스터 키는 이 계정이 소유해야 합니다. root로 로그인하면 서비스 계정이 DB를 읽지 못합니다.

## 관리 화면

`codex-gateway server start`는 게이트웨이와 함께 관리 화면을 `127.0.0.1:8081`에 엽니다. systemd 서비스도 같은 명령으로 뜨므로 따로 켤 것이 없습니다.

원격 서버라면 PC에서 SSH 터널을 먼저 열고, 설치할 때 받은 링크를 PC 브라우저로 엽니다. `1455`는 Codex 로그인 콜백용입니다.

```bash
ssh -L 8081:127.0.0.1:8081 -L 1455:127.0.0.1:1455 user@server
```

관리 화면은 왼쪽 탭(모바일에서는 위쪽 탭)으로 Codex 계정, API 키, 게이트웨이, 업데이트, 관리자를 나눠 보여 줍니다. 하는 일은 다음과 같습니다.

- 관리자 비밀번호 정하기와 바꾸기
- Codex 로그인, 토큰 새로 받기, 연결 해제
- API 키 만들기, 재발급, 폐기. 키 이름은 꼭 적어야 하고 64자까지 됩니다. 새 키는 만든 직후 한 번만 보이며, HTTP로 접속해도 복사 버튼이 동작합니다. 사용 중과 폐기됨 목록을 나눠 보여 주고, 폐기한 키는 하나씩 또는 한꺼번에 목록에서 지울 수 있습니다.
- 게이트웨이 수신 주소 바꾸기. 새 주소를 먼저 연 뒤 옛 주소를 닫으므로 재시작이 필요 없습니다.
- 새 버전 확인과 업데이트, 자동 업데이트 켜고 끄기

Codex 로그인 버튼을 누르면 OpenAI 로그인 창이 열리고, 끝나면 `127.0.0.1:1455` 콜백으로 자동 연결됩니다. 터널 없이 접속했다면 로그인 뒤 브라우저 주소창의 주소 전체를 화면의 입력칸에 붙여 넣으면 됩니다.

보안 경계는 다음과 같습니다.

- 관리 화면은 기본으로 루프백 주소에만 열립니다. `CODEX_GATEWAY_ADMIN_ALLOW` 없이 `CODEX_GATEWAY_ADMIN_LISTEN`에 루프백이 아닌 주소를 넣으면 시작하지 않습니다.
- 요청을 보낸 IP가 루프백이나 허용 대역 밖이면 거절합니다.
- `Host`가 `127.0.0.1`, `localhost`, `::1`, 또는 허용 대역 안의 IP 주소가 아니면 거절합니다. 도메인 이름은 받지 않습니다. DNS rebinding으로 들어오는 요청을 막기 위해서입니다.
- 쓰기 요청은 JSON만 받고, 다른 출처의 `Origin`이나 `Sec-Fetch-Site: cross-site`가 붙으면 거절합니다.
- 세션은 쿠키가 아니라 탭의 `sessionStorage`에 두고 `Authorization` 헤더로 보냅니다. 같은 기기의 다른 localhost 앱에 세션이 새지 않습니다.
- 관리자 비밀번호는 PBKDF2-SHA256 600000회로 저장합니다. 5분 안에 10번 틀리면 잠시 막힙니다. 비밀번호를 바꾸면 다른 세션은 모두 끊깁니다.
- 1회용 설정 링크는 `setup.token` 파일에만 있고, 비밀번호를 정하면 지워집니다. systemd 로그에는 링크를 남기지 않습니다.

### LAN에서 바로 열기

같은 네트워크의 PC에서 SSH 터널 없이 `http://서버IP:8081`로 열려면 `/etc/codex-gateway.env`에 두 값을 넣고 서비스를 다시 시작합니다.

```bash
CODEX_GATEWAY_ADMIN_LISTEN=0.0.0.0:8081
CODEX_GATEWAY_ADMIN_ALLOW=192.168.0.0/24
```

```bash
sudo systemctl restart codex-gateway
```

`CODEX_GATEWAY_ADMIN_ALLOW`에는 쉼표로 여러 대역이나 IP를 적을 수 있습니다. 사설망 대역인 `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, Tailscale의 `100.64.0.0/10`, IPv6 `fc00::/7` 안쪽만 받고, `0.0.0.0/0`처럼 인터넷이 섞인 대역은 시작을 거부합니다.

이 방식은 HTTPS가 아니라서 로그인할 때 관리자 비밀번호가 네트워크에 암호화되지 않은 채 흐릅니다. 집이나 사무실처럼 믿을 수 있는 네트워크에서만 켜고, 비밀번호는 다른 곳에 쓰지 않는 값으로 정합니다. 서버에 방화벽이 있으면 허용 대역에서 8081로 들어오는 연결을 열어 둡니다.

LAN 주소로 접속하면 Codex 로그인 뒤 브라우저가 PC의 `localhost:1455`로 이동해서 연결할 수 없다는 화면이 뜹니다. 그 화면의 주소 전체를 관리 화면의 입력칸에 붙여 넣으면 연결됩니다.

비밀번호를 잊었으면 서버에서 새 설정 링크를 받습니다. 서비스는 다시 시작하지 않아도 됩니다.

```bash
sudo -u codex-gateway env CODEX_GATEWAY_DATA_DIR=/var/lib/codex-gateway codex-gateway admin reset-password
```

## 업데이트

관리 화면의 업데이트 칸에서 새 버전을 확인하고 바로 설치할 수 있습니다. 자동 업데이트는 기본으로 켜져 있고, 6시간마다 GitHub 최신 릴리스를 확인해서 새 버전이 있으면 설치합니다. 화면에서 끌 수 있습니다.

설치는 이 순서로 합니다.

1. `https://api.github.com/repos/yldst-dev/codex2api/releases/latest`에서 최신 태그를 읽습니다. `v1.2.3` 형태가 아닌 태그와 사전 릴리스는 받지 않습니다.
2. 그 태그의 `SHA256SUMS`와 이 기기용 실행 파일을 HTTPS로 받습니다.
3. SHA256이 맞을 때만 같은 폴더의 임시 파일을 실행 파일 자리로 옮깁니다. 맞지 않으면 기존 파일을 그대로 둡니다.
4. 게이트웨이를 다시 시작합니다. 관리 화면 세션은 메모리에만 있으므로 다시 로그인해야 합니다.

systemd 서비스는 권한 없는 계정으로 돌고 `/usr/local/bin`을 쓸 수 없습니다. 그래서 웹은 데이터 폴더에 `update.request` 파일만 만들고, root로 도는 `codex-gateway-update.path`가 이를 보고 `codex-gateway-update.service`를 실행합니다. 이 도우미는 요청 파일 내용을 믿지 않고 직접 최신 릴리스를 받아 확인한 뒤 서비스를 다시 시작합니다. 설치 스크립트가 두 유닛을 함께 등록합니다.

직접 `codex-gateway server start`로 띄웠고 실행 파일 폴더에 쓸 수 있으면, 그 프로세스가 파일을 바꾸고 같은 PID로 다시 실행됩니다. 둘 다 아니면 웹 업데이트는 꺼지고 확인만 합니다. 직접 빌드한 `dev` 버전도 확인만 합니다.

터미널에서는 다음을 씁니다.

```bash
codex-gateway version
codex-gateway update --check
sudo codex-gateway update --restart codex-gateway.service
```

`v0.1.0`으로 설치한 서버에는 업데이트 도우미가 없습니다. 설치 스크립트를 `--service`로 한 번 다시 실행하면 이후부터 웹에서 업데이트됩니다.

도우미가 5분 안에 처리하지 않으면 관리 화면에 오류가 뜹니다. 그때는 서버에서 다음을 확인합니다.

```bash
systemctl status codex-gateway-update.path
journalctl -u codex-gateway-update.service
```

## 설정

| 환경 변수 | 기본값 | 의미 |
| --- | --- | --- |
| `CODEX_GATEWAY_LISTEN` | `127.0.0.1:8080` | 수신 주소. 값을 넣으면 관리 화면과 저장한 값보다 우선하고, 관리 화면에서 바꿀 수 없게 됩니다. |
| `CODEX_GATEWAY_DATA_DIR` | `./data` | SQLite와 마스터 키 위치 |
| `CODEX_GATEWAY_MASTER_KEY` | 없음 | 32바이트 키. hex 64자 또는 base64 |
| `CODEX_GATEWAY_ADMIN_LISTEN` | `127.0.0.1:8081` | 관리 화면 주소. 기본은 루프백만 됩니다. `off`면 끕니다. |
| `CODEX_GATEWAY_ADMIN_ALLOW` | 없음 | 관리 화면에 접속할 수 있는 사설망 대역. 넣으면 LAN 주소로 열 수 있습니다. |
| `CODEX_GATEWAY_UPDATER` | 없음 | `systemd`면 웹 업데이트를 `codex-gateway-update.path`에 맡깁니다. systemd 유닛이 넣습니다. |

마스터 키 우선순위는 환경 변수, 그다음 `CODEX_GATEWAY_DATA_DIR/master.key` 입니다. 둘 다 없고 `gateway.db`도 없는 첫 실행에서만 `master.key`를 만들고 권한을 `0600`으로 둡니다. 이 파일이 없으면 DB 안의 OAuth 토큰을 풀 수 없습니다.

`gateway.db`가 이미 있는데 `master.key`가 없으면 새 키를 만들지 않고 오류로 멈춥니다. DB에는 마스터 키 확인값이 들어 있어서, 다른 키로 열면 `master key does not match this data directory` 오류가 납니다. 서비스와 CLI가 같은 마스터 키를 써야 합니다. 마스터 키를 `/etc/codex-gateway.env`에 넣었다면 CLI를 실행할 때도 그 값을 함께 넘깁니다.

systemd를 쓸 때는 `/etc/codex-gateway.env`에 넣습니다. 예시는 `deploy/codex-gateway.env.example` 입니다.

```bash
openssl rand -base64 32
```

출력값을 `CODEX_GATEWAY_MASTER_KEY`에 넣거나, `master.key` 파일에 한 줄로 저장합니다.

수신 주소는 관리 화면에서 바꾸면 바로 적용됩니다. `codex-gateway config set listen 127.0.0.1:8080`은 데이터 디렉터리에 값만 저장하므로 이미 떠 있는 서버에는 재시작 후 적용됩니다. `CODEX_GATEWAY_LISTEN` 환경 변수가 있으면 그 값이 우선하고, 관리 화면에서도 바꿀 수 없습니다. systemd 유닛은 수신 주소를 고정하지 않으므로, `/etc/codex-gateway.env`의 `CODEX_GATEWAY_LISTEN`을 비워 두면 관리 화면과 저장한 값이 쓰입니다.

## OAuth 로그인

관리 화면의 Codex 로그인 버튼이 이 과정을 대신합니다. 아래는 터미널에서 할 때의 방법입니다.

서버에서 다음을 실행합니다.

```bash
sudo -u codex-gateway env \
  CODEX_GATEWAY_DATA_DIR=/var/lib/codex-gateway \
  codex-gateway auth login
```

CLI가 `https://auth.openai.com/oauth/authorize?...` 주소를 출력하고 `127.0.0.1:1455`에서 콜백을 기다립니다. 브라우저가 없는 서버에서는 자신의 PC 브라우저로 그 주소를 엽니다.

공개 클라이언트 ID는 sub2api가 Codex CLI 공식 공개 식별자로 적어 둔 `app_EMoamEEZ73f0CkXaXp7hrann` 입니다. 클라이언트 시크릿은 없습니다. redirect URI는 `http://localhost:1455/auth/callback` 으로 고정입니다. PKCE S256과 `state` 검증을 생략하지 않습니다.

로그인이 끝나면 access token, refresh token, id token, 만료 시각, ChatGPT account id, user id, email, plan type이 SQLite에 저장됩니다. 토큰 세 개는 AES-256-GCM으로 암호화됩니다. 서버를 재시작해도 다시 로그인할 필요가 없습니다.

## 원격 서버에서 SSH로 로그인

콜백은 서버의 `127.0.0.1:1455`에만 열립니다. PC에서 터널을 연 다음 브라우저로 로그인 주소를 엽니다.

```bash
ssh -L 1455:127.0.0.1:1455 user@server
```

브라우저의 `localhost:1455`가 서버 콜백으로 이어집니다. 터널을 쓰기 어려우면 수동 입력을 사용합니다.

```bash
codex-gateway auth login --manual
```

브라우저 주소창에 남은 최종 콜백 URL 전체를 붙여 넣습니다. `code`와 `state`가 둘 다 있어야 하고, `state`가 이번 로그인에서 만든 값과 같아야 합니다.

상태 확인, 강제 갱신, 로그아웃은 다음입니다.

```bash
codex-gateway auth status
codex-gateway auth refresh
codex-gateway auth logout
```

`auth refresh`는 만료 여부와 관계없이 refresh token으로 갱신합니다. 로그아웃은 저장된 OAuth 계정만 지웁니다. API Key는 남습니다.

## API 키 발급

클라이언트에 주는 열쇠는 OpenAI access token이 아닙니다. 게이트웨이가 따로 발급한 `cg_` 키입니다. 이 키는 클라이언트에서 게이트웨이로 들어오는 요청만 확인합니다. 게이트웨이가 OpenAI에 보낼 때는 저장된 OAuth access token을 쓰고, 그 토큰은 키 발급 화면에도 응답에도 나오지 않습니다.

발급 전에 OAuth 로그인이 끝나 있어야 합니다. 로그인이 없으면 키는 만들어지지만 Codex 요청은 `503`으로 거절됩니다.

관리 화면에서는 API 키 칸에 이름을 적고 키 만들기를 누르면 됩니다. 새 키는 그 자리에서 한 번만 보이고 복사 버튼이 붙습니다. 아래는 터미널에서 할 때의 방법입니다.

터미널에서 메뉴로 발급하려면 인자 없이 실행한 뒤 API 키, 키 만들기를 고릅니다. 이름만 물어보고, Enter만 누르면 `default`가 됩니다.

```bash
codex-gateway
```

명령으로 발급할 때는 다음을 씁니다. Ubuntu 서비스 계정이면 데이터 디렉터리를 같은 값으로 맞춥니다.

```bash
sudo -u codex-gateway env CODEX_GATEWAY_DATA_DIR=/var/lib/codex-gateway \
  codex-gateway key create --name default
```

출력은 이 형태입니다. `Key:` 줄이 평문이며 이후에는 다시 보여 주지 않습니다. 비밀번호 관리자나 root만 읽는 파일에 바로 적습니다.

```text
API key created.

Name: default
Key: cg_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx

This key will only be displayed once.
```

키는 `crypto/rand`로 만든 32바이트입니다. 앞은 `cg_`입니다. DB에는 평문 대신 마스터 키로 만든 HMAC-SHA256만 저장합니다. 목록에는 앞 11글자, 이름, 상태, 만든 시각, 마지막 사용 시각, 폐기 시각만 나옵니다.

```bash
codex-gateway key list
codex-gateway key create --name ci --json
codex-gateway key rotate KEY_ID
codex-gateway key revoke KEY_ID
codex-gateway key delete KEY_ID
```

`KEY_ID`는 `key list`의 ID 열입니다. 메뉴에서는 폐기와 재발급도 목록에서 고르므로 ID를 치지 않아도 됩니다. 재발급은 같은 ID의 새 평문을 한 번 출력하고 이전 평문은 즉시 무효가 됩니다. 이미 폐기한 키는 재발급되지 않습니다. `key delete`는 폐기한 키만 목록에서 지웁니다. 사용 중인 키는 먼저 폐기해야 합니다.

`--json`은 스크립트용입니다. 평문은 `key create`와 `key rotate`의 JSON에만 들어 있습니다. `key list --json`에는 없습니다.

폐기된 키, 틀린 키, 키가 없는 요청은 모두 `401`입니다. 어떤 키가 맞는지 응답 문구로 구분하지 않습니다. 쿼리 문자열 `?api_key=`, `?key=`, `?access_token=`은 `400`으로 거절합니다. 인증 헤더는 아래 한 가지입니다.

```http
Authorization: Bearer cg_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

## 서버 실행

```bash
codex-gateway server start
codex-gateway server status
```

기본 수신 주소는 `127.0.0.1:8080` 입니다. `0.0.0.0`으로 열려면 관리 화면에서 바꾸거나 `CODEX_GATEWAY_LISTEN`을 직접 지정해야 합니다. 관리 화면은 이와 별도로 `127.0.0.1:8081`에 열립니다.

준비된 경로는 다음입니다.

```text
GET  /health
GET  /v1/models
GET  /models
GET  /backend-api/codex/models
POST /v1/responses
POST /responses
POST /backend-api/codex/responses
POST /v1/responses/compact
POST /responses/compact
POST /backend-api/codex/responses/compact
```

클라이언트 인증은 `Authorization: Bearer cg_...` 만 받습니다. 쿼리 문자열의 API Key는 거절합니다.

게이트웨이는 그 키를 OpenAI에 보내지 않습니다. 저장된 OAuth access token으로 `https://chatgpt.com/backend-api/codex/responses`에 요청합니다. 계정 헤더는 `chatgpt-account-id` 입니다. `User-Agent`, `originator`, `version`은 아래 Codex 클라이언트 버전을 사용합니다.

```text
User-Agent: codex-tui/0.157.1 (Ubuntu 22.4.0; x86_64) xterm-256color
originator: codex-tui
version: 0.157.1
OpenAI-Beta: responses=experimental
```

이 버전은 [openai/codex](https://github.com/openai/codex)의 최신 안정 릴리스를 따릅니다. GitHub Actions `.github/workflows/codex-version.yml`이 매일 00:17 KST에 latest 릴리스를 확인하고, 숫자가 다르면 `internal/oauth/oauth.go`와 이 문서의 버전 표기만 고친 뒤 기본 브랜치에 커밋합니다. `0.157.0-alpha.1` 같은 사전 릴리스는 넣지 않습니다. 기본 브랜치가 Actions의 push를 막으면 이 자동 커밋은 실패합니다.

`/responses`의 `Accept`는 `text/event-stream` 입니다. `/responses/compact`만 `application/json` 입니다. 클라이언트가 보낸 `session_id`, `conversation_id`, `x-codex-*` 일부는 그대로 전달합니다. `instructions`가 비어 있으면 sub2api의 기본 Codex instructions를 넣습니다. `reasoning.effort`가 `minimal`이면 `none`으로 바꿉니다. 고칠 것이 없으면 본문을 그대로 보냅니다. 고칠 때도 바꾼 필드 말고는 값을 그대로 두며, 최상위 키 순서와 공백만 달라질 수 있습니다.

`GET /v1/models`는 ChatGPT Codex 모델 목록을 OpenAI list 형태로 바꿉니다. `GET /models`와 `GET /backend-api/codex/models`는 업스트림 JSON을 그대로 돌려줍니다. 모델 이름은 하드코딩하지 않습니다.

SSE 응답은 업스트림에서 읽는 대로 클라이언트에 보내고 각 조각마다 flush 합니다. Codex 업스트림은 스트리밍 응답에 `Content-Type`을 붙이지 않을 때가 있어서, `/responses`의 성공 응답은 타입이 비었거나 `text/plain`이어도 스트리밍으로 보고 `Content-Type: text/event-stream`을 붙여 보냅니다. JSON 성공 응답과 오류 응답, `/compact`는 모아서 한 번에 보냅니다. 클라이언트가 끊으면 업스트림 요청도 취소됩니다. 본문 상한은 32MB, 헤더 상한은 1MB 입니다. 스트리밍이 아닌 업스트림 응답이 32MB를 넘으면 잘라서 보내지 않고 `502`를 돌려줍니다. 업스트림으로의 리다이렉트는 따라가지 않고, TLS 검증을 끄지 않습니다.

access token은 만료 3분 전에 refresh token으로 갱신합니다. 동시에 여러 요청이 들어와도 프로세스 안에서는 mutex로, 프로세스 사이에서는 `refresh.lock`으로 갱신을 한 번만 합니다. 새 refresh token이 오면 access token과 함께 한 트랜잭션에 저장합니다. 응답에 refresh token이 없으면 기존 값을 유지합니다.

`invalid_grant`처럼 다시 로그인해야 하는 오류는 토큰을 지우지 않고 상태만 `reauth_required`로 바꿉니다. 그때 응답은 다음과 같습니다.

```text
OAuth session is no longer valid.
Run:

codex-gateway auth login
```

`403`은 본문에 `invalid_grant` 같은 재로그인 코드가 있을 때만 재로그인 필요로 봅니다. 코드가 없는 `403`은 차단 페이지일 수 있어 일시 오류로 다룹니다. 토큰 응답에 `expires_in`이 없으면 access token의 `exp`를 만료 시각으로 씁니다.

네트워크 오류와 5xx는 기존 토큰을 유지합니다. access token이 아직 살아 있으면 그 토큰으로 요청을 계속합니다.

`SIGINT`와 `SIGTERM`에서는 30초 동안 진행 중인 요청을 기다립니다. 그때까지 끝나지 않은 스트림은 강제로 닫고 정상 종료합니다.

## systemd

```bash
sudo cp deploy/codex-gateway.service deploy/codex-gateway-update.service deploy/codex-gateway-update.path /etc/systemd/system/
sudo cp deploy/codex-gateway.env.example /etc/codex-gateway.env
sudo chmod 600 /etc/codex-gateway.env
sudo systemctl daemon-reload
sudo systemctl enable --now codex-gateway codex-gateway-update.path
sudo systemctl status codex-gateway
```

`/etc/codex-gateway.env`의 마스터 키를 채우거나, `/var/lib/codex-gateway/master.key`를 서비스 계정 소유 `0600`으로 둡니다.

설치 스크립트 없이 직접 등록했다면 첫 설정 링크는 다음처럼 만듭니다. 서비스가 처음 뜰 때 이 파일을 만듭니다.

```bash
echo "http://127.0.0.1:8081/#setup=$(sudo cat /var/lib/codex-gateway/setup.token)"
```

터미널에서 로그인과 키 발급을 할 때는 서비스와 같은 계정으로 실행합니다.

```bash
sudo -u codex-gateway env CODEX_GATEWAY_DATA_DIR=/var/lib/codex-gateway codex-gateway auth login
sudo -u codex-gateway env CODEX_GATEWAY_DATA_DIR=/var/lib/codex-gateway codex-gateway key create
```

마스터 키를 `/etc/codex-gateway.env`에 넣었다면 CLI에도 같은 값을 넘깁니다.

```bash
sudo sh -c 'set -a; . /etc/codex-gateway.env; exec sudo -E -u codex-gateway codex-gateway key create'
```

## API 사용

서버는 기본적으로 이 주소만 엽니다.

```text
http://127.0.0.1:8080
```

다른 PC에서 쓰려면 게이트웨이를 인터넷에 열지 말고 SSH로 포트만 가져옵니다.

```bash
ssh -L 8080:127.0.0.1:8080 user@server
```

그다음 PC의 `http://127.0.0.1:8080`으로 요청합니다. 아래 예시는 키를 환경 변수에 넣은 상태입니다. 예시에 실제 키를 적지 않습니다.

```bash
export CODEX_GATEWAY_API_KEY='cg_여기에_발급받은_키'
```

상태 확인에는 키가 필요 없습니다.

```bash
curl http://127.0.0.1:8080/health
```

로그인이 살아 있으면 `{"status":"ok","oauth":true}` 입니다.

모델 이름은 코드에 고정하지 않습니다. 먼저 목록을 받아서 그 `id`를 요청에 넣습니다.

```bash
curl http://127.0.0.1:8080/v1/models \
  -H "Authorization: Bearer $CODEX_GATEWAY_API_KEY"
```

`GET /v1/models`는 `id`, `object`, `owned_by`만 있는 짧은 목록입니다. `GET /models`와 `GET /backend-api/codex/models`는 Codex 원본 목록입니다. 원본에는 표시 이름과 모델별 속성이 더 들어 있습니다. 목록은 `client_version`에 따라 달라지므로, 게이트웨이 기본 버전으로 받은 결과를 기준으로 고릅니다.

Codex 업스트림은 문자열 `input`을 받지 않습니다. `input`은 배열이어야 하고, `store`는 `false`, `stream`은 `true`여야 합니다. 이 셋이 빠지면 `400`과 함께 `Input must be a list`, `Store must be set to false`, `Stream must be set to true`가 옵니다.

```bash
curl -N http://127.0.0.1:8080/v1/responses \
  -H "Authorization: Bearer $CODEX_GATEWAY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "MODEL_NAME",
    "store": false,
    "stream": true,
    "reasoning": {"effort": "low"},
    "input": [
      {"role": "user", "content": "안녕"}
    ]
  }'
```

`MODEL_NAME`은 `/v1/models`의 `id`로 바꿉니다. `-N`은 스트리밍 응답을 버퍼에 모으지 않고 바로 출력합니다. 성공하면 `200`이고 `text/event-stream`으로 `response.output_text.delta`가 이어진 뒤 `response.completed`가 옵니다.

`reasoning.effort`는 생략할 수 있습니다. 생략하면 게이트웨이가 값을 채우지 않고 업스트림 기본값을 씁니다. `none`과 `low`는 확인된 값입니다. `minimal`만 전달 전에 `none`으로 바뀝니다.

### fast 모드

요청 본문에 `"service_tier": "priority"`를 넣으면 Codex fast 모드로 처리됩니다. Codex CLI의 `/fast on`과 같은 기능이고, 게이트웨이는 이 값을 그대로 넘기므로 따로 켤 설정은 없습니다.

```bash
curl -N http://127.0.0.1:8080/v1/responses \
  -H "Authorization: Bearer $CODEX_GATEWAY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "MODEL_NAME",
    "store": false,
    "stream": true,
    "service_tier": "priority",
    "input": [{"role": "user", "content": "안녕"}]
  }'
```

| `service_tier` | 결과 |
| --- | --- |
| 넣지 않음, `"default"` | 표준 속도 |
| `"priority"` | fast 모드 |
| `"fast"`, `"flex"`, `"auto"` 등 | `400 {"detail":"Unsupported service_tier: ..."}` |

일반 OpenAI API는 `"fast"`도 받지만, Codex 백엔드는 `"priority"`만 받습니다.

- 비용: ChatGPT 요금제의 Codex 사용량을 표준의 2.5배 씁니다. Plus와 Pro 요금제 기능입니다. 자세한 기준은 [Codex 속도 문서](https://learn.chatgpt.com/codex/agent-configuration/speed)에 있습니다.
- 속도: 첫 글자가 나오는 시간은 거의 같고, 글자가 나오는 속도가 빨라집니다. 2026-09-27에 80개 숫자를 출력하게 해서 표준과 번갈아 4번씩 잰 중앙값은 `gpt-6-astra`가 6.10초에서 3.98초(33에서 61 토큰/초), `gpt-5.6-luna`가 4.14초에서 2.92초(54에서 79 토큰/초)였습니다.
- 지원 모델: `GET /models`의 각 모델 `service_tiers`에 `"priority"`가 있으면 지원합니다. 없는 모델(예: `gpt-daybreak-blue-latest`)은 `"priority"`를 보내도 오류 없이 표준으로 처리됩니다.
- 확인: 응답의 `service_tier`는 fast로 처리돼도 `default`로 옵니다. 응답만 보고 fast 적용 여부를 알 수는 없습니다.

사용량을 많이 쓰므로 모든 요청에 켜기보다 빠른 답이 필요한 요청에만 넣는 편이 좋습니다. 게이트웨이는 키별로 fast 모드를 막지 않으므로, API 키를 가진 클라이언트는 누구나 켤 수 있습니다.

같은 본문을 받는 별칭은 다음입니다.

```text
POST /v1/responses
POST /responses
POST /backend-api/codex/responses
```

`/compact`가 붙은 세 경로는 압축 요청용이며 `Accept`가 `application/json`입니다.

실패할 때 자주 보는 응답은 다음입니다.

| 상태 | 의미 |
| --- | --- |
| `401` | 키가 없거나, 틀리거나, 폐기됨 |
| `400` | 본문 형식이 업스트림 조건과 다르거나, 키가 쿼리 문자열에 있음 |
| `503` | OAuth 로그인이 없거나, refresh token이 거절되어 다시 로그인해야 함 |
| `502` | 업스트림 연결 실패 |

Codex CLI나 다른 클라이언트를 붙일 때는 공급자 주소를 `http://127.0.0.1:8080`으로 두고, API 키에 `cg_` 키를 넣습니다. OpenAI OAuth access token을 클라이언트 설정에 넣지 않습니다.

## 데이터 위치

```text
${CODEX_GATEWAY_DATA_DIR}/gateway.db
${CODEX_GATEWAY_DATA_DIR}/master.key
${CODEX_GATEWAY_DATA_DIR}/refresh.lock
${CODEX_GATEWAY_DATA_DIR}/setup.token
${CODEX_GATEWAY_DATA_DIR}/update.request
```

기본 데이터 디렉터리는 실행 위치의 `data/` 입니다. systemd 예시는 `/var/lib/codex-gateway` 입니다. DB 파일과 마스터 키는 `0600`, 디렉터리는 `0700` 입니다.

DB에 있는 것은 OAuth 계정 하나와 API Key 목록, `listen` 설정, 관리자 비밀번호 해시입니다. `setup.token`은 관리자 비밀번호를 정하기 전에만 있고, `update.request`는 업데이트를 요청한 뒤 도우미가 처리하기 전까지만 있습니다. 자동 업데이트 설정도 DB에 들어 있습니다. email, account id, plan type은 상태 출력용으로 평문입니다. access token, refresh token, id token은 암호화됩니다.

## 자격 증명 주의사항

- access token, refresh token, Authorization 헤더, API Key 전체, authorization code는 로그에 남기지 않습니다.
- Gateway API Key와 OpenAI OAuth 토큰은 역할이 다릅니다. 키 관리 명령은 OAuth 토큰을 출력하지 않습니다.
- 게이트웨이는 API Key를 업스트림에 넣지 않고, OAuth access token을 클라이언트 응답에 복사하지 않습니다.
- `.env`, `data/`, `master.key`, `refresh.lock`, `setup.token`, `*.db`와 SQLite WAL 파일은 git에서 제외됩니다. `.env.example`만 저장소에 있습니다.
- 쿼리 문자열 인증은 거부합니다.
- TLS 검증을 끄지 않습니다.

마스터 키나 `gateway.db`를 채팅, 셸 기록, 이슈에 붙이지 마십시오.

## 백업

서비스를 멈춘 다음 데이터 디렉터리를 복사합니다. DB만 복사하면 마스터 키가 없어 토큰을 풀 수 없습니다.

```bash
sudo systemctl stop codex-gateway
sudo cp -a /var/lib/codex-gateway/gateway.db /var/lib/codex-gateway/gateway.db-wal /var/lib/codex-gateway/master.key /safe/backup/
sudo chown root:root /safe/backup/master.key /safe/backup/gateway.db
sudo chmod 600 /safe/backup/master.key /safe/backup/gateway.db
sudo systemctl start codex-gateway
```

`gateway.db-wal`이 없으면 그 파일은 빼고 복사해도 됩니다. 복원할 때도 DB와 `master.key`를 같은 디렉터리에 두고 소유자를 `codex-gateway`로 맞춥니다.

## 로그아웃과 다시 로그인

```bash
codex-gateway auth logout
codex-gateway auth login
```

refresh token이 거절되면 기존 토큰은 DB에 남아 있고 상태만 재로그인이 필요하다고 바뀝니다. 위 로그인 명령을 다시 실행하면 그 계정을 덮어씁니다.

## 라이선스

이 프로그램은 GNU Lesser General Public License v3.0으로 배포됩니다. 전문은 `LICENSE`에 있습니다.

Codex OAuth와 Codex 요청 형식의 일부는 Wei-Shaw의 sub2api에서 파생되었습니다. sub2api도 LGPL-3.0 입니다. 파생 범위와 원본 파일은 `NOTICE`를 보십시오. 사용자 시스템, 결제, 프록시 풀, Claude, Gemini, Grok 연동은 가져오지 않았습니다.

## 개발

관리 화면은 `web/`에 있는 Vite, React, TypeScript, shadcn/ui 프로젝트입니다. 빌드 결과는 `internal/admin/dist/app`에 들어가고 Go 실행 파일에 포함됩니다.

```bash
npm --prefix web ci
npm --prefix web run lint
npm --prefix web run build
```

화면을 고치면서 볼 때는 게이트웨이를 띄운 채 `npm --prefix web run dev`를 실행합니다. `/api` 요청은 `127.0.0.1:8081`로 넘어갑니다. 다른 주소면 `CODEX_GATEWAY_ADMIN_URL`로 바꿉니다.

```bash
go fmt ./...
go test ./...
go vet ./...
go build ./...
```
