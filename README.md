# ProxyFleet

Центральная панель управления приватными HTTP/SOCKS5 proxy-нодами. Архитектура похожа на Remnawave: control plane хранит желаемую конфигурацию, а лёгкие агенты на VPS сами подключаются к панели и применяют изменения.

## Что уже работает

- единая тёмная админ-панель;
- создание ноды и одноразовый enrollment token;
- исходящее подключение агента к control plane — управляющий порт на ноде не нужен;
- heartbeat и статусы online/offline;
- проверка доступности `api.telegram.org` с каждой ноды;
- HTTP proxy с `CONNECT` и Basic Auth;
- SOCKS5 proxy с username/password;
- отдельные HTTP/SOCKS5-прокси с собственными логинами, паролями и назначением на ноду;
- синхронизация конфигурации без перезапуска контейнера;
- активные соединения и счётчики трафика;
- отдельные графики активности подключений и нод с пятиминутной историей;
- Telegram-уведомления о состоянии нод и изменениях доступов с поддержкой HTTP(S)-прокси;
- страница настроек для смены пароля и оформления панели;
- журнал событий;
- установка панели и ноды через Docker Compose;
- мастер первого запуска без ручной подготовки `.env`;
- bcrypt-хеширование пароля владельца и автоматически созданный ключ сессий;
- блокировка loopback/private/link-local целей по умолчанию.

## Быстрый запуск панели

```bash
docker compose pull
docker compose up -d
```

Откройте `http://127.0.0.1:8088` и пройдите короткий мастер: укажите логин владельца, пароль и публичный URL панели. Пароль сохраняется только как bcrypt-хеш, а ключ подписи сессий генерируется автоматически. Оба значения живут в Docker volume `proxyfleet-control-data`; файл `.env` не нужен.

Панель слушает только `127.0.0.1:8088`. Для внешнего доступа публикуйте её через nginx/Caddy с HTTPS. Secure-cookie включается автоматически по TLS или заголовку `X-Forwarded-Proto: https`.

## Подключение ноды

1. Откройте панель.
2. Нажмите «Добавить ноду».
3. Выполните сгенерированную Docker-команду на VPS.

Панель сама выдаёт готовую `docker run` команду с адресом control plane, одноразовым ключом и образом `neluckoff/proxyfleet:latest` из Docker Hub — отдельный конфигурационный файл на ноде не нужен.

После первого подключения enrollment token становится недействительным. Постоянный ключ агента хранится в Docker volume `proxyfleet-node-data`.

## Ноды и прокси

Нода — это сервер с запущенным агентом. На одной ноде можно создать любое количество прокси. Каждый прокси выбирает одну ноду и один протокол (`HTTP` или `SOCKS5`), получает отдельный логин и пароль и выдаётся в виде готовой ссылки подключения. Прокси можно отключать, переносить на другую ноду, менять его протокол и перевыпускать пароль из панели.

## Reverse proxy (nginx)

```nginx
server {
    listen 443 ssl http2;
    server_name proxy.example.com;

    location / {
        proxy_pass http://127.0.0.1:8088;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

## Firewall

Proxy-порты следует открывать только для серверов-клиентов. Например:

```bash
ufw allow from <IP_СЕРВЕРА_NEXUS> to any port 3128 proto tcp
ufw allow from <IP_СЕРВЕРА_NEXUS> to any port 1080 proto tcp
```

Не публикуйте HTTP/SOCKS5 порты всему интернету даже при включённой авторизации.

## Структура проекта

```text
cmd/proxyfleet/        точка входа server/agent
internal/agent/        proxy-движок и синхронизация ноды
internal/control/
  server.go            запуск HTTP-сервера и маршруты
  auth.go              первый запуск, вход и сессии
  nodes.go             управление нодами
  users.go             прокси, учётные данные и назначения на ноды
  agents.go            enrollment, config и heartbeat
  store.go             атомарное файловое хранилище
  http_helpers.go      JSON, middleware и крипто-утилиты
  web/                 статический интерфейс панели
internal/model/        общие структуры control plane и агента
```

В обычном Docker-запуске настраивать переменные не требуется. Панель и агенты используют образ `neluckoff/proxyfleet:latest` из Docker Hub. `DATA_PATH`, `LISTEN_ADDR`, `NODE_IMAGE` и `PROXYFLEET_IMAGE` остаются лишь необязательными техническими override-параметрами для разработки и собственного registry.

## Следующий этап

- автоматическая публикация версионированных образов в Docker Hub;
- mTLS или WireGuard между control plane и нодами;
- команды restart/update из панели;
- автоматический failover и единая gateway-точка;
- лимиты трафика и соединений по отдельным прокси;
- роли администраторов и второй фактор;
- миграция хранилища с JSON на PostgreSQL для нескольких экземпляров control plane.
