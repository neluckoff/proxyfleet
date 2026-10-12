# ProxyFleet

[![CI](https://github.com/neluckoff/proxyfleet/actions/workflows/ci.yml/badge.svg)](https://github.com/neluckoff/proxyfleet/actions/workflows/ci.yml)
[![Docker publish](https://github.com/neluckoff/proxyfleet/actions/workflows/docker-publish.yml/badge.svg)](https://github.com/neluckoff/proxyfleet/actions/workflows/docker-publish.yml)
[![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go&logoColor=white)](https://go.dev/)

Self-hosted control plane для управления приватными HTTP- и SOCKS5-прокси. ProxyFleet разворачивает лёгкие агенты на VPS, раздаёт им желаемую конфигурацию и собирает состояние всей инфраструктуры в одной панели.

Проект не требует обязательного `.env`: владелец, пароль, адрес панели и внутренний сессионный секрет настраиваются при первом открытии. Состояние хранится в Docker volume.

## Возможности

- несколько proxy-нод под единым control plane;
- несколько независимых прокси на одной ноде;
- HTTP proxy с `CONNECT` и Basic Auth;
- SOCKS5 proxy с логином и паролем;
- создание, отключение, перенос и редактирование прокси без перезапуска контейнера;
- одноразовые enrollment-токены для подключения агентов;
- статусы нод, heartbeat, uptime, трафик и активные соединения;
- память Go runtime, память хоста, goroutines и load average;
- графики активности нод и подключений;
- журнал действий и событий инфраструктуры;
- Telegram-уведомления с поддержкой HTTP(S)-proxy;
- смена пароля владельца, названия и логотипа панели;
- русский и английский интерфейс;
- адаптивная мобильная навигация;
- блокировка loopback, private и link-local целей по умолчанию.

## Архитектура

```text
Браузер
   │ HTTPS
   ▼
Reverse proxy (nginx/Caddy)
   │ 127.0.0.1:8088
   ▼
ProxyFleet control plane
   │ исходящие HTTPS-запросы агентов
   ├───────────────┬───────────────┐
   ▼               ▼               ▼
Agent / VPS 1   Agent / VPS 2   Agent / VPS N
HTTP/SOCKS5     HTTP/SOCKS5     HTTP/SOCKS5
```

Control plane хранит желаемое состояние. Агенты самостоятельно подключаются к панели, получают конфигурацию и отправляют heartbeat. Открывать управляющий порт на proxy-нодах не требуется.

## Требования

Для обычного запуска:

- Linux-сервер;
- Docker Engine 24+;
- Docker Compose v2;
- домен и HTTPS reverse proxy для публичной установки.

Для разработки без Docker:

- Go 1.23+;
- Node.js только для проверки JavaScript-синтаксиса.

## Быстрый запуск

Пока публичный Docker Hub-образ не опубликован, используйте локальную сборку:

```bash
git clone https://github.com/neluckoff/proxyfleet.git
cd proxyfleet
docker compose up -d --build
docker compose ps
```

Панель откроется на `http://127.0.0.1:8088`.

При первом открытии:

1. укажите логин владельца;
2. задайте пароль длиной не менее 12 символов;
3. укажите публичный HTTPS-адрес панели;
4. завершите мастер настройки.

Пароль сохраняется только как bcrypt-хеш. Секрет подписи сессий генерируется автоматически. Оба значения хранятся в volume `proxyfleet-control-data`.

### Запуск из опубликованного образа

После публикации `neluckoff/proxyfleet:latest` локальную сборку можно отключить командой:

```bash
docker compose pull
docker compose up -d --no-build
```

Для собственного registry:

```bash
PROXYFLEET_IMAGE=registry.example.com/team/proxyfleet:1.0.0 docker compose up -d --no-build
```

## Production-установка

### 1. Запустите control plane

```bash
sudo mkdir -p /opt/proxyfleet
sudo chown -R "$USER":"$(id -gn)" /opt/proxyfleet
git clone https://github.com/neluckoff/proxyfleet.git /opt/proxyfleet
cd /opt/proxyfleet
docker compose up -d --build
```

Контейнер слушает только `127.0.0.1:8088`, поэтому наружу панель следует публиковать через HTTPS.

### 2. Настройте nginx

```nginx
server {
    listen 443 ssl http2;
    server_name proxy.example.com;

    location / {
        proxy_pass http://127.0.0.1:8088;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

Secure-cookie включается автоматически при TLS или заголовке `X-Forwarded-Proto: https`.

### 3. Подключите ноду

1. Откройте раздел «Ноды».
2. Нажмите «Добавить ноду».
3. Укажите название, регион, публичный адрес и порты.
4. Выполните сгенерированную команду на новом VPS.

Enrollment-токен действует один раз. После подключения агент получает постоянный ключ и сохраняет его в volume `proxyfleet-node-data`.

Если Docker Hub-образ ещё не опубликован, клонируйте репозиторий на ноду и используйте Compose:

```bash
git clone https://github.com/neluckoff/proxyfleet.git
cd proxyfleet
CONTROL_URL=https://proxy.example.com \
ENROLL_TOKEN=<TOKEN_ИЗ_ПАНЕЛИ> \
docker compose -f docker-compose.node.yml up -d --build
```

### 4. Создайте прокси

На странице «Прокси» задайте:

- название;
- логин;
- пароль или автоматическую генерацию;
- протокол HTTP либо SOCKS5;
- ноду, которая будет обслуживать подключение.

Готовая ссылка показывается только после создания или перевыпуска пароля. Сохраните её сразу: исходный пароль в control plane не хранится.

## Firewall

Порты HTTP/SOCKS5 следует открывать только для доверенных клиентов:

```bash
sudo ufw allow from <IP_КЛИЕНТА> to any port 3128 proto tcp
sudo ufw allow from <IP_КЛИЕНТА> to any port 1080 proto tcp
```

Не открывайте proxy-порты всему интернету даже при включённой авторизации.

## Telegram-уведомления

В разделе «Настройки» можно указать:

- Bot Token;
- Chat ID;
- HTTP(S)-proxy для доступа к Telegram;
- состояние отправки уведомлений.

Кнопка проверки вызывает `getMe`, затем отправляет тестовое сообщение. Токен не возвращается браузеру после сохранения.

Уведомления создаются при подключении, отключении и восстановлении нод, изменениях конфигурации и операциях с прокси.

## Обновление

При локальной сборке:

```bash
cd /opt/proxyfleet
git pull --ff-only
docker compose up -d --build
docker image prune -f
```

При использовании опубликованного образа:

```bash
cd /opt/proxyfleet
git pull --ff-only
docker compose pull
docker compose up -d --no-build
```

## Резервное копирование

Все данные control plane находятся в volume `proxyfleet-control-data`.

```bash
docker run --rm \
  -v proxyfleet-control-data:/data:ro \
  -v "$PWD":/backup \
  alpine tar -czf /backup/proxyfleet-backup.tar.gz -C /data .
```

Восстановление выполняйте только при остановленном control plane:

```bash
docker compose down
docker run --rm \
  -v proxyfleet-control-data:/data \
  -v "$PWD":/backup:ro \
  alpine sh -c 'rm -rf /data/* && tar -xzf /backup/proxyfleet-backup.tar.gz -C /data'
docker compose up -d
```

## Конфигурация

| Переменная | Где используется | По умолчанию | Назначение |
|---|---|---:|---|
| `LISTEN_ADDR` | control plane | `:8080` | Адрес HTTP-сервера внутри контейнера |
| `DATA_PATH` | control plane | `/data/state.json` | Файл состояния |
| `NODE_IMAGE` | control plane | `neluckoff/proxyfleet:latest` | Образ в генерируемой команде ноды |
| `PROXYFLEET_IMAGE` | Compose | `neluckoff/proxyfleet:latest` | Override образа панели и агента |
| `CONTROL_URL` | agent | обязательно | Публичный URL control plane |
| `ENROLL_TOKEN` | agent | первый запуск | Одноразовый токен подключения |
| `AGENT_STATE_PATH` | agent | `/data/agent.json` | Постоянное состояние агента |
| `HTTP_PROXY_PORT` | node Compose | `3128` | Публичный HTTP proxy-порт |
| `SOCKS_PROXY_PORT` | node Compose | `1080` | Публичный SOCKS5-порт |

## Локальная разработка

```bash
git clone https://github.com/neluckoff/proxyfleet.git
cd proxyfleet
go mod download
go run ./cmd/proxyfleet server
```

По умолчанию сервер доступен на `http://127.0.0.1:8080`.

Проверки:

```bash
go test ./...
go vet ./...
node --check internal/control/web/app.js
node --check internal/control/web/i18n.js
```

## Структура проекта

```text
cmd/proxyfleet/             точка входа server/agent
internal/agent/             proxy-движок, синхронизация и метрики ноды
internal/control/           HTTP API, авторизация и файловое хранилище
internal/control/web/       встроенный интерфейс панели
internal/model/             общие структуры control plane и агента
.github/workflows/          CI и публикация Docker-образа
docker-compose.yml          control plane
docker-compose.node.yml     отдельная proxy-нода
```

## HTTP API

Основные маршруты:

- `/api/setup/*` — первоначальная настройка;
- `/api/login`, `/api/logout`, `/api/me` — сессия владельца;
- `/api/nodes` — управление нодами;
- `/api/proxies` — управление прокси;
- `/api/analytics`, `/api/activity`, `/api/events` — наблюдаемость;
- `/api/settings/*` — оформление, Telegram и смена пароля;
- `/api/agent/*` — enrollment, конфигурация и heartbeat агента.

Все административные маршруты, кроме setup/login и публичного branding, требуют активной сессии.

## Публикация Docker-образа

Workflow `.github/workflows/docker-publish.yml` публикует multi-arch образ `linux/amd64` и `linux/arm64` в Docker Hub.

Добавьте в GitHub → Settings → Secrets and variables → Actions:

- `DOCKERHUB_USERNAME` — `neluckoff`;
- `DOCKERHUB_TOKEN` — access token с правом Read & Write.

После этого создайте тег:

```bash
git tag v0.1.0
git push origin v0.1.0
```

Workflow опубликует `neluckoff/proxyfleet:0.1.0`, `:0.1`, `:0` и `:latest`. Его также можно запустить вручную через Actions.

## Участие в разработке

Перед изменениями прочитайте [CONTRIBUTING.md](.github/CONTRIBUTING.md). Ошибки и предложения оформляйте через GitHub Issues. Уязвимости не публикуйте открыто — порядок описан в [SECURITY.md](.github/SECURITY.md).

## Roadmap

- mTLS или WireGuard между control plane и нодами;
- restart/update нод из панели;
- автоматический failover и единая gateway-точка;
- лимиты трафика и соединений;
- дополнительные роли и 2FA;
- PostgreSQL для нескольких экземпляров control plane.

## Лицензия

Лицензия проекта пока не выбрана. До добавления файла `LICENSE` исходный код доступен для ознакомления, но формально не предоставляет стандартных open-source прав на использование и распространение.
