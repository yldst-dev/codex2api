# codex-gateway

Ubuntu 서버에서 OpenAI Codex OAuth 로그인을 끝낸 뒤, 그 자격 증명을 서버 안에 두고, 별도의 Gateway API Key로 Codex Responses 요청을 대신 보내는 작은 게이트웨이입니다.

터미널에서 인자 없이 `codex-gateway`를 실행하면 방향키 메뉴가 열립니다. 직접 입력하는 값은 키 이름, 콜백 주소, 수신 주소뿐입니다. 스크립트는 아래 하위 명령을 그대로 쓰면 됩니다.

하는 일은 네 가지입니다.

- Codex OAuth 로그인
- access token 자동 갱신
- Gateway API Key 발급과 폐기
- Codex Responses API 프록시

회원가입, 관리 웹, 결제, 그룹, Redis, PostgreSQL은 없습니다. 저장소는 SQLite 하나입니다.

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

소스에서 직접 빌드할 때는 Go 1.24 이상이 필요합니다.

```bash
git clone https://github.com/yldst-dev/codex2api.git
cd codex2api
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

## 설정

| 환경 변수 | 기본값 | 의미 |
| --- | --- | --- |
| `CODEX_GATEWAY_LISTEN` | `127.0.0.1:8080` | 수신 주소. 외부 공개는 이 값을 직접 바꿀 때만 됩니다. |
| `CODEX_GATEWAY_DATA_DIR` | `./data` | SQLite와 마스터 키 위치 |
| `CODEX_GATEWAY_MASTER_KEY` | 없음 | 32바이트 키. hex 64자 또는 base64 |

마스터 키 우선순위는 환경 변수, 그다음 `CODEX_GATEWAY_DATA_DIR/master.key` 입니다. 둘 다 없으면 첫 실행에서 `master.key`를 만들고 권한을 `0600`으로 둡니다. 이 파일이 없으면 DB 안의 OAuth 토큰을 풀 수 없습니다.

systemd를 쓸 때는 `/etc/codex-gateway.env`에 넣습니다. 예시는 `deploy/codex-gateway.env.example` 입니다.

```bash
openssl rand -base64 32
```

출력값을 `CODEX_GATEWAY_MASTER_KEY`에 넣거나, `master.key` 파일에 한 줄로 저장합니다.

`codex-gateway config set listen 127.0.0.1:8080`은 데이터 디렉터리에 수신 주소만 저장합니다. 이미 떠 있는 서버에는 재시작 후 적용됩니다. 환경 변수가 있으면 그 값이 우선합니다.

## OAuth 로그인

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
```

`KEY_ID`는 `key list`의 ID 열입니다. 메뉴에서는 폐기와 재발급도 목록에서 고르므로 ID를 치지 않아도 됩니다. 재발급은 같은 ID의 새 평문을 한 번 출력하고 이전 평문은 즉시 무효가 됩니다. 이미 폐기한 키는 재발급되지 않습니다.

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

기본 수신 주소는 `127.0.0.1:8080` 입니다. `0.0.0.0`으로 열려면 `CODEX_GATEWAY_LISTEN`을 직접 지정해야 합니다.

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
User-Agent: codex-tui/0.156.1 (Ubuntu 22.4.0; x86_64) xterm-256color
originator: codex-tui
version: 0.156.1
OpenAI-Beta: responses=experimental
```

이 버전은 [openai/codex](https://github.com/openai/codex)의 최신 안정 릴리스를 따릅니다. GitHub Actions `.github/workflows/codex-version.yml`이 매일 00:17 KST에 latest 릴리스를 확인하고, 숫자가 다르면 `internal/oauth/oauth.go`와 이 문서의 버전 표기만 고친 뒤 기본 브랜치에 커밋합니다. `0.157.0-alpha.1` 같은 사전 릴리스는 넣지 않습니다. 기본 브랜치가 Actions의 push를 막으면 이 자동 커밋은 실패합니다.

`/responses`의 `Accept`는 `text/event-stream` 입니다. `/responses/compact`만 `application/json` 입니다. 클라이언트가 보낸 `session_id`, `conversation_id`, `x-codex-*` 일부는 그대로 전달합니다. `instructions`가 비어 있으면 sub2api의 기본 Codex instructions를 넣습니다. `reasoning.effort`가 `minimal`이면 `none`으로 바꿉니다. 그 외 본문은 재인코딩하지 않습니다.

`GET /v1/models`는 ChatGPT Codex 모델 목록을 OpenAI list 형태로 바꿉니다. `GET /models`와 `GET /backend-api/codex/models`는 업스트림 JSON을 그대로 돌려줍니다. 모델 이름은 하드코딩하지 않습니다.

SSE 응답은 업스트림에서 읽는 대로 클라이언트에 보내고 각 조각마다 flush 합니다. 클라이언트가 끊으면 업스트림 요청도 취소됩니다. 본문 상한은 32MB, 헤더 상한은 1MB 입니다. 업스트림으로의 리다이렉트는 따라가지 않고, TLS 검증을 끄지 않습니다.

access token은 만료 3분 전에 refresh token으로 갱신합니다. 동시에 여러 요청이 들어와도 프로세스 안에서는 mutex로, 프로세스 사이에서는 `refresh.lock`으로 갱신을 한 번만 합니다. 새 refresh token이 오면 access token과 함께 한 트랜잭션에 저장합니다. 응답에 refresh token이 없으면 기존 값을 유지합니다.

`invalid_grant`처럼 다시 로그인해야 하는 오류는 토큰을 지우지 않고 상태만 `reauth_required`로 바꿉니다. 그때 응답은 다음과 같습니다.

```text
OAuth session is no longer valid.
Run:

codex-gateway auth login
```

네트워크 오류와 5xx는 기존 토큰을 유지합니다. access token이 아직 살아 있으면 그 토큰으로 요청을 계속합니다.

`SIGINT`와 `SIGTERM`에서는 30초 안에 연결을 닫고 종료합니다.

## systemd

```bash
sudo cp deploy/codex-gateway.service /etc/systemd/system/codex-gateway.service
sudo cp deploy/codex-gateway.env.example /etc/codex-gateway.env
sudo chmod 600 /etc/codex-gateway.env
sudo systemctl daemon-reload
sudo systemctl enable --now codex-gateway
sudo systemctl status codex-gateway
```

`/etc/codex-gateway.env`의 마스터 키를 채우거나, `/var/lib/codex-gateway/master.key`를 서비스 계정 소유 `0600`으로 둡니다. 로그인과 키 발급도 같은 계정으로 실행합니다.

```bash
sudo -u codex-gateway env CODEX_GATEWAY_DATA_DIR=/var/lib/codex-gateway codex-gateway auth login
sudo -u codex-gateway env CODEX_GATEWAY_DATA_DIR=/var/lib/codex-gateway codex-gateway key create
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
```

기본 데이터 디렉터리는 실행 위치의 `data/` 입니다. systemd 예시는 `/var/lib/codex-gateway` 입니다. DB 파일과 마스터 키는 `0600`, 디렉터리는 `0700` 입니다.

DB에 있는 것은 OAuth 계정 하나와 API Key 목록, `listen` 설정입니다. email, account id, plan type은 상태 출력용으로 평문입니다. access token, refresh token, id token은 암호화됩니다.

## 자격 증명 주의사항

- access token, refresh token, Authorization 헤더, API Key 전체, authorization code는 로그에 남기지 않습니다.
- Gateway API Key와 OpenAI OAuth 토큰은 역할이 다릅니다. 키 관리 명령은 OAuth 토큰을 출력하지 않습니다.
- 게이트웨이는 API Key를 업스트림에 넣지 않고, OAuth access token을 클라이언트 응답에 복사하지 않습니다.
- `.env`, `data/`, `master.key`, `refresh.lock`, `*.db`와 SQLite WAL 파일은 git에서 제외됩니다. `.env.example`만 저장소에 있습니다.
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

```bash
go fmt ./...
go test ./...
go vet ./...
go build ./...
```
