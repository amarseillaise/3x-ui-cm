# 3x-ui-cm — клиентский кабинет для 3x-ui

PWA-кабинет, из которого клиент VPN-сервиса на [3x-ui](https://github.com/MHSanaei/3x-ui) видит
свою подписку, продлевает её и получает push-уведомления. Приложение живёт рядом с мастер-панелью
и ходит в неё по API с Bearer-токеном. Подробности архитектуры и правила для агентов — в
[AGENT.md](AGENT.md).

## Возможности

- Вход по той же ссылке, что стоит в VPN-приложении: `https://sub.example.com:7115/subway/<subId>`,
  открытая в браузере, ведёт в кабинет (через шаблон страницы подписки 3x-ui, см. ниже). Без паролей.
  Прямая ссылка `https://cab.example.com/s/<subId>` тоже работает.
- Экран подписки: статус, срок, трафик, онлайн, ссылка подписки и QR.
- Продление по доверию: клиент выбирает тариф, видит реквизиты, нажимает «Я перевёл» —
  срок продлевается сразу, админ получает push и подтверждает или откатывает.
- Push-уведомления (Web Push/VAPID) об истечении и исчерпании трафика.
- Кастомные рассылки из админки, CLI `pushctl` или HTTP API.

## Быстрый старт (сервер)

Нужны Docker с Compose и домен, указывающий на сервер.

```bash
git clone https://github.com/amarseillaise/3x-ui-cm.git && cd 3x-ui-cm
cp deploy/.env.example .env
cp deploy/config.example.yaml config.yaml     # тарифы и реквизиты
make gen-secrets                              # ADMIN_TOKEN, ADMIN_LINK_SECRET, SESSION_SECRET → в .env
docker run --rm golang:1.26-alpine sh -c 'cd /tmp && git clone -q https://github.com/amarseillaise/3x-ui-cm.git && cd 3x-ui-cm && go run ./cmd/server gen-vapid'
```

Заполните `.env`:

| Переменная | Что это |
|---|---|
| `NODE_URL` | URL мастер-панели с базовым путём, например `https://panel.example.com:2053/secret/` |
| `NODE_API_TOKEN` | Панель → Settings → Security → API Token |
| `APP_BASE_URL`, `APP_DOMAIN` | Публичный адрес кабинета |
| `ADMIN_TOKEN`, `ADMIN_LINK_SECRET`, `SESSION_SECRET` | Из `make gen-secrets` |
| `VAPID_PUBLIC_KEY`, `VAPID_PRIVATE_KEY`, `VAPID_SUBJECT` | Из `gen-vapid`; subject — `mailto:вы@example.com` |

Запуск:

```bash
# вариант А: Caddy внутри compose получает сертификат сам (порты 80/443 свободны)
make compose-edge-up
# вариант Б: TLS терминирует ваш nginx/caddy (пример: deploy/nginx.example.conf), приложение на 127.0.0.1:8080
make compose-up
```

Проверка: `curl https://cab.example.com/healthz` → `{"panel":"ok","status":"ok"}`.

## Работа администратора

- Вход в админку: `https://cab.example.com/a/<ADMIN_LINK_SECRET>`. Установите PWA на телефон и
  включите уведомления — заявки на продление будут приходить push'ем.
- Ссылка для клиента — его ссылка подписки из панели (та же, что в VPN-приложении).
  `pushctl link -email <email>` печатает её, а прямую ссылку кабинета `https://cab.example.com/s/<subId>`
  выводит в stderr на случай, если шаблон на панели ещё не установлен.
- Заявки: вкладка «Заявки» в админке или `pushctl pending`, `pushctl confirm <id>`, `pushctl reject <id>`.
- Рассылка: вкладка «Рассылка» или `pushctl send -title "…" -body "…" -all`.

`pushctl` читает `APP_BASE_URL` и `ADMIN_TOKEN` из окружения или `.env`:

```bash
docker compose --project-directory . -f deploy/docker-compose.yml exec app pushctl pending
```

## Ссылка подписки как вход в кабинет

3x-ui отдаёт по ссылке подписки base64-список конфигов VPN-приложениям, а браузеру
(`Accept: text/html`) — HTML-страницу. Страницу можно заменить своим шаблоном
(Settings → Subscription → Information → **Sub Theme Directory**). Наш шаблон сразу перенаправляет
браузер на `APP_BASE_URL/s/<subId>`, поэтому клиенту достаточно одной ссылки.

```bash
set -a && source .env && set +a && make sub-template        # → sub_template/index.html
scp sub_template/index.html root@panel:/etc/3x-ui/sub_templates/cabinet/index.html
```

В панели укажите Sub Theme Directory `/etc/3x-ui/sub_templates/cabinet/` и сохраните. Проверка:

```bash
curl -s -H 'Accept: text/html' https://sub.example.com:7115/subway/<subId> | grep -o 'url=[^"]*'
curl -s https://sub.example.com:7115/subway/<subId> | head -c 80      # VPN-приложениям по-прежнему base64
```

Если панель и кабинет на одном сервере, `scp` не нужен — просто скопируйте файл. При смене
`APP_BASE_URL` перегенерируйте и замените шаблон.

## Обновление и бэкап

```bash
git pull && make compose-up            # пересборка образа и перезапуск
make backup                            # копия SQLite → ./backups/app-<дата>.db
```

Данные приложения — том `app_data` (`/data/app.db`): сессии, push-подписки, заявки на продление,
журнал уведомлений. Подписки клиентов живут в панели 3x-ui, приложение их не дублирует.

## Разработка

```bash
make tools-node        # переносимый Node 22 в .tools/ (если Node не установлен)
make web               # сборка фронта в internal/webdist/dist
make check             # go vet + go test + go build + typecheck фронта
```

Локальный запуск. Бинарник `.env` сам не читает — переменные нужно экспортировать. Один раз
заполните `.env` в корне (`NODE_URL`, `NODE_API_TOKEN`, `APP_BASE_URL=http://127.0.0.1:8080`,
`LISTEN_ADDR=127.0.0.1:8080`, `DB_PATH=$PWD/data/app.db`, `CONFIG_PATH=deploy/config.example.yaml`,
`TRUST_PROXY=0`, секреты из `make gen-secrets`, ключи из `go run ./cmd/server gen-vapid`), затем:

```bash
set -a && source .env && set +a && make run
```

Файл `.env` должен заканчиваться переводом строки, иначе дописанная переменная склеится с последней.
Проверка: `curl -s http://127.0.0.1:8080/healthz`, вход клиентом — `/s/<subId>`, админом — `/a/<ADMIN_LINK_SECRET>`.
Панель в `.env` боевая: просмотр безопасен, но «Я перевёл» реально двигает срок через `bulkAdjust`.

Фронт в режиме разработки: `cd web && npm run dev` (прокси `/api`, `/s`, `/a` на `127.0.0.1:8080`).

## Как это устроено

Go-сервер (`cmd/server`) отдаёт встроенный React-PWA, хранит своё состояние в SQLite и ходит в
мастер-панель 3x-ui только по нескольким эндпоинтам: список клиентов, поиск по `subId`, онлайн,
ссылки подписки, `bulkAdjust` для продления. Планировщик раз в `POLL_INTERVAL` проверяет сроки и
трафик и шлёт напоминания за `NOTIFY_DAYS` дней. Схема, потоки и контракт API — в `AGENT.md`.
