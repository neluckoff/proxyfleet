# Участие в разработке ProxyFleet

Спасибо за желание улучшить проект.

## Перед началом

1. Проверьте существующие issue и pull request.
2. Для крупного изменения сначала создайте feature request и согласуйте направление.
3. Не публикуйте токены, пароли, proxy-ссылки, IP-адреса пользователей и содержимое production state-файлов.

## Локальная разработка

```bash
git clone https://github.com/neluckoff/proxyfleet.git
cd proxyfleet
go mod download
go test ./...
go run ./cmd/proxyfleet server
```

## Требования к изменениям

- форматируйте Go-код через `gofmt`;
- добавляйте тесты для новой backend-логики;
- сохраняйте обратную совместимость persisted state либо добавляйте автоматическую миграцию;
- не возвращайте секреты через API после первоначальной выдачи;
- обновляйте README при изменении установки, конфигурации или публичного API;
- проверяйте desktop и mobile UI для визуальных изменений.

## Проверки перед pull request

```bash
go test ./...
go vet ./...
node --check internal/control/web/app.js
node --check internal/control/web/i18n.js
test -z "$(gofmt -l .)"
```

## Коммиты и pull request

Используйте короткие сообщения в повелительном наклонении, например:

```text
feat: add proxy traffic limits
fix: keep mobile navigation visible
docs: document node upgrades
```

В PR опишите мотивацию, изменение поведения, миграцию и выполненные проверки. Для интерфейса приложите скриншоты.

