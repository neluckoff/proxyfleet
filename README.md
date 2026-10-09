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
- пользователи с назначением на выбранные ноды;
- синхронизация конфигурации без перезапуска контейнера;
- активные соединения и счётчики трафика;
- журнал событий;
- установка панели и ноды через Docker Compose;
- блокировка loopback/private/link-local целей по умолчанию.

## Быстрый запуск панели

```bash
cp .env.example .env
```

Задайте минимум:

```dotenv
ADMIN_PASSWORD=очень-длинный-пароль
SESSION_SECRET=случайная-строка-минимум-32-символа
PUBLIC_URL=https://proxy.example.com
COOKIE_SECURE=true
```

Запустите:

```bash
docker compose up -d --build
```

Панель слушает только `127.0.0.1:8088`. Публикуйте её через nginx/Caddy с HTTPS.

## Подключение ноды

1. Откройте панель.
2. Нажмите «Добавить ноду».
3. Выполните сгенерированную Docker-команду на VPS.

До публикации образа в registry можно развернуть ноду из checkout:

```bash
cp .env.example .env
# В .env укажите CONTROL_URL и одноразовый ENROLL_TOKEN из панели.
docker compose -f docker-compose.node.yml up -d --build
```

После первого подключения enrollment token становится недействительным. Постоянный ключ агента хранится в Docker volume `proxyfleet-node-data`.

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

## Переменные control plane

| Переменная | Назначение |
|---|---|
| `ADMIN_USERNAME` | Логин администратора |
| `ADMIN_PASSWORD` | Пароль, минимум 12 символов |
| `SESSION_SECRET` | Ключ подписи сессий, минимум 32 символа |
| `PUBLIC_URL` | Публичный HTTPS URL панели для агентов |
| `NODE_IMAGE` | Образ, используемый в генерируемой команде установки |
| `COOKIE_SECURE` | `true` при работе панели через HTTPS |

## Следующий этап

- выпуск образов в GHCR через GitHub Actions;
- mTLS или WireGuard между control plane и нодами;
- команды restart/update из панели;
- автоматический failover и единая gateway-точка;
- лимиты трафика и соединений по пользователям;
- роли администраторов и второй фактор;
- миграция хранилища с JSON на PostgreSQL для нескольких экземпляров control plane.
