# 3x-ui-cm — клиентский кабинет для 3x-ui

PWA-кабинет, в котором клиент VPN-сервиса на [3x-ui](https://github.com/MHSanaei/3x-ui) видит свою
подписку, продлевает её и получает push. Go-сервер отдаёт встроенный React-PWA, хранит состояние в
SQLite и ходит в мастер-панель по API. Архитектура и правила для агентов — в [AGENT.md](AGENT.md).

- Вход без паролей: ссылка подписки из VPN-приложения, открытая в браузере, ведёт в кабинет.
- Экран подписки: статус, срок, трафик, онлайн, ссылка и QR.
- Продление по доверию: клиент жмёт «Я перевёл», срок растёт сразу, админ подтверждает или откатывает.
- Push об истечении и трафике, рассылки из админки или через `pushctl`.

## Установка на сервер

Нужны Docker с Compose и домен. Все команды — из корня репозитория.

**1. Код и конфигурация.**

```bash
git clone https://github.com/amarseillaise/3x-ui-cm.git && cd 3x-ui-cm
cp deploy/.env.example .env
cp deploy/config.example.yaml config.yaml    # тарифы и реквизиты, впишите свои
```

**2. Секреты.**

```bash
make gen-secrets                                                       # три секрета
docker compose --project-directory . -f deploy/docker-compose.yml build
docker compose --project-directory . -f deploy/docker-compose.yml run --rm app gen-vapid
```

**3. Заполните `.env`.** Файл должен заканчиваться переводом строки.

| Переменная | Что это |
|---|---|
| `NODE_URL` | URL панели с базовым путём: `https://panel.example.com:2053/secret/` |
| `NODE_API_TOKEN` | Панель → Settings → Security → API Token |
| `APP_BASE_URL` | Публичный адрес кабинета, вместе с портом, если он нестандартный |
| `APP_DOMAIN` | Домен кабинета, нужен только профилю Caddy |
| `ADMIN_TOKEN`, `ADMIN_LINK_SECRET`, `SESSION_SECRET` | Из `make gen-secrets` |
| `VAPID_*` | Из `gen-vapid`, subject — почта или https-адрес |
| `NGINX_CERT_FILE`, `NGINX_KEY_FILE` | Пути к сертификату на хосте, нужны только генератору конфига |
| `TRUST_PROXY=1` | Если впереди стоит обратный прокси |

**4. Запуск.** Приложение слушает `127.0.0.1:8080` и отдаёт plain HTTP, TLS терминирует прокси.

```bash
make compose-up        # внешний nginx, см. шаг 5
make compose-edge-up   # либо Caddy внутри compose, если порты 80 и 443 свободны
```

**5. nginx.** На сервере с 3x-ui порт 443 обычно занят самим Xray, а 80 — существующим nginx,
поэтому кабинету нужен свой порт. Укажите его прямо в `APP_BASE_URL`, остальное выводится оттуда:

```bash
make nginx-config                      # → deploy/nginx/cabinet.conf
sudo deploy/install-nginx.sh --dry-run # показать, что будет сделано
sudo deploy/install-nginx.sh           # положить, проверить, перезагрузить
curl -sS https://cab.example.com:8443/healthz    # {"panel":"ok","status":"ok"}
```

Скрипт сам находит, куда nginx включает конфиги, делает резервную копию, откатывается при отказе
`nginx -t` и после перезагрузки проверяет, что порт занял именно nginx. Без этой проверки блок с
уже занятым портом выглядит успешно установленным, а nginx продолжает работать со старой
конфигурацией. Заменить существующий файл — `--target /etc/nginx/conf.d/ваш.conf`.

**6. Ссылка подписки как вход в кабинет.** По ней 3x-ui отдаёт base64 VPN-приложениям, а браузеру —
HTML-страницу, которую можно заменить своим шаблоном.

```bash
set -a && source .env && set +a && make sub-template    # → sub_template/index.html
```

Положите файл на хост панели, например в `/etc/x-ui/sub_templates/cabinet/index.html`, и укажите в
Settings → Subscription → Information → Sub Theme Directory **путь до каталога**, не до файла.
Проверка, что браузер уходит в кабинет, а приложения получают прежнее:

```bash
curl -s -H 'Accept: text/html' https://sub.example.com:7115/subway/<subId> | grep -o 'url=[^"]*'
curl -s https://sub.example.com:7115/subway/<subId> | head -c 80
```

При смене `APP_BASE_URL` шаблон нужно перегенерировать.

## Администрирование

Админка — `https://cab.example.com/a/<ADMIN_LINK_SECRET>`. Установите PWA на телефон и включите
уведомления, иначе заявки приходить не будут. Клиенту отдавайте его ссылку подписки, её печатает
`pushctl link -email <email>`. CLI читает `APP_BASE_URL` и `ADMIN_TOKEN` из `.env`:

```bash
C="docker compose --project-directory . -f deploy/docker-compose.yml exec app"
$C pushctl pending                 # confirm <id> | reject <id> | history | stats
$C pushctl send -title "…" -body "…" -all
```

## Обновление и бэкап

```bash
git pull && make compose-up     # пересборка и перезапуск
make backup                     # копия SQLite → ./backups/app-<дата>.db
make compose-logs
```

Состояние лежит в томе `app_data` (`/data/app.db`): сессии, push-подписки, заявки, журнал
уведомлений. Подписки клиентов живут в панели, приложение их не дублирует.

## Разработка

```bash
make tools-node    # переносимый Node 22 в .tools/, если Node не установлен
make web           # сборка фронта в internal/webdist/dist
make check         # go vet + go test + go build + typecheck фронта
```

Бинарник `.env` сам не читает. Для локального запуска добавьте `APP_BASE_URL=http://127.0.0.1:8080`,
`LISTEN_ADDR=127.0.0.1:8080`, `DB_PATH=$PWD/data/app.db`, `CONFIG_PATH=deploy/config.example.yaml`,
`TRUST_PROXY=0`, затем `set -a && source .env && set +a && make run`. Фронт в режиме разработки —
`cd web && npm run dev`. Панель в `.env` боевая: просмотр безопасен, но «Я перевёл» реально двигает
срок клиенту через `bulkAdjust`.
