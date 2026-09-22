# AGENT.md — 3x-ui-cm (клиентский кабинет для 3x-ui)

Инструкции для ИИ-агентов и разработчиков. Прочитать целиком перед любой задачей.
Для деталей API панели есть скилл `.claude/skills/3x-ui-api/` (вызов `/3x-ui-api`).

## 1. Что это за проект

PWA-приложение (в первую очередь iOS и Android), из которого клиент VPN-сервиса управляет
своей подпиской. Проект — надстройка над панелью [3x-ui](https://github.com/MHSanaei/3x-ui)
и без неё не имеет смысла. Все данные о подписках живут в панели; у нас хранится только то,
чего в панели нет (push-подписки, история уведомлений, заявки на продление).

Функции MVP, по приоритету:

1. Показать информацию о текущей подписке (срок, трафик, статус, ссылка/QR для клиента).
2. Прислать push о скором истечении подписки.
3. Присылать кастомные push (рассылка от администратора).
4. Продлить подписку: клиент выбирает тариф, видит реквизиты, нажимает «Я перевёл» — продление
   применяется сразу (принцип доверия), админ получает push и подтверждает или откатывает.

Чем проект НЕ является: не админка панели, не создаёт и не удаляет клиентов, не управляет
нодами и inbound'ами, не заменяет ссылку подписки 3x-ui, не принимает платежи сам (нет платёжных
провайдеров, оплата идёт вне приложения).

## 2. Зависимость от 3x-ui

| Что | Значение |
|---|---|
| Репозиторий | https://github.com/MHSanaei/3x-ui |
| Документация | https://docs.sanaei.dev/docs/ |
| API reference | https://docs.sanaei.dev/docs/reference/api/ (клиенты: `/docs/reference/api/clients/`) |
| OpenAPI | https://docs.sanaei.dev/openapi.json (~480 KB, версия «3.x») |
| Версия панели | 3.6.0 (мастер и ноды, Xray 26.7.28), ветка с multi-node и новым API `/panel/api/clients/*` |
| Топология | Одна мастер-панель + 7 нод. Приложение ходит ТОЛЬКО в мастер (`NODE_URL`); мастер сам синхронизирует ноды |
| Аутентификация | `Authorization: Bearer <token>` (Settings → Security → API Token). CSRF для Bearer не нужен |
| Права токена | Полный админ. Хранить как пароль, никогда не логировать, не отдавать во фронт |
| Формат ответа | Всегда `{ "success": bool, "msg": string, "obj": any }`. Проверять `success`, HTTP-код может быть 200 при ошибке |

Старый API `/panel/api/inbounds/*` в панели тоже есть, но в этом проекте используем только
`/panel/api/clients/*`, `/panel/api/nodes/list`, `/panel/api/server/status`.

## 3. Доменная модель

**Подписка = клиент панели, идентифицируемый по `subId`.**

- В панели клиент (`email` — уникальный ключ) прикреплён к нескольким inbound'ам
  (на практике до 11, по одному-двум на ноду). Трафик и срок — одна запись на клиента.
- На текущей панели `subId` ↔ `email` строго 1:1 (81 клиент, 81 уникальный subId).
  Код обязан корректно работать, если по одному subId найдётся несколько клиентов:
  срок = минимальный ненулевой `expiryTime`, трафик суммируется, `enable` = хотя бы один включён.
- `subId` — это то, что уже есть в ссылке подписки клиента (`https://<host>/<subPath>/<subId>`),
  поэтому magic-link по subId не даёт клиенту ничего сверх того, что у него уже есть.

Ключевые поля клиента (см. скилл для полного списка):

| Поле | Тип | Семантика |
|---|---|---|
| `email` | string | Уникальный идентификатор клиента, ключ для всех `clients/*` эндпоинтов |
| `subId` | string | Идентификатор подписки, наш ключ входа |
| `enable` | bool | Выключенный клиент не может подключаться (панель сама выключает истёкших/исчерпавших) |
| `expiryTime` | int64, **миллисекунды** Unix | `0` = бессрочно. Отрицательное = «отложенный старт»: срок начнётся при первом подключении, длительность = `abs(value)` мс (проверить на реальном клиенте перед использованием) |
| `totalGB` | int64, **байты** (несмотря на имя) | `0` = безлимит |
| `traffic.up` / `traffic.down` / `traffic.total` | int64, байты | Счётчики; `total` дублирует квоту |
| `traffic.lastOnline` | int64, мс | 0 = никогда |
| `traffic.lastSubFetch` | int64, мс | Когда клиентское приложение последний раз тянуло подписку |
| `tgId` | int64 | Telegram ID, если админ заполнил. Пока не используем |
| `inboundIds` | int[] | К каким inbound'ам прикреплён |

Статусы подписки в нашем приложении (вычисляются у нас, в панели их нет):
`unlimited` (expiryTime=0 и totalGB=0) · `active` · `expiring` (осталось ≤ порога) ·
`expired` (expiryTime < now) · `depleted` (totalGB>0 и used ≥ totalGB) · `disabled` (enable=false).

Снимок реальной панели на 2026-09-15 (для приоритизации): 81 клиент, у всех `totalGB=0`,
у 53 `expiryTime=0`, у 28 срок задан. Значит главный сценарий — срок действия; квота трафика
второстепенна, но поддерживается.

## 4. Принятые продуктовые решения (2026-09-15)

| Вопрос | Решение |
|---|---|
| Вход клиента | Magic-link `GET /s/<subId>` → сессия в httpOnly-cookie. Без паролей и регистрации. Клиенту выдаётся его **ссылка подписки** из панели (та же, что в VPN-приложении): открытая в браузере, она через шаблон страницы подписки 3x-ui (`subThemeDir`) редиректит на `APP_BASE_URL/s/<subId>` (решение 2026-09-18) |
| Оплата | Вне приложения, вручную, по принципу доверия. Платёжных провайдеров нет; слой `Payment` с единственной реализацией `manual` (реквизиты из конфига) |
| Продление | Клиент выбирает план → сумма + реквизиты → «Я перевёл» → приложение сразу делает `bulkAdjust` → админу push. Семантика: +N дней от `max(now, expiryTime)`; бессрочным кнопка не показывается |
| Тарифы | `config.yaml`: 30 дн/180 ₽, 90 дн/500 ₽, 180 дн/960 ₽; реквизиты там же, показываются как есть, без кода заявки |
| Защита | Одна неподтверждённая (`pending`) заявка на подписку. «Отклонить» откатывает ровно `applied_days` и шлёт клиенту push |
| Push-канал | Только Web Push (VAPID). Без Firebase, без Telegram |
| Админ | Входит по своей ссылке `/a/<ADMIN_LINK_SECRET>` в тот же PWA. Минимальный экран `/admin`: заявки (подтвердить/отклонить) и форма рассылки. Плюс admin HTTP API с `ADMIN_TOKEN` и CLI `pushctl` |
| Хранилище | SQLite (файл в volume) |
| Деплой | Docker Compose: сервис `app` на `:8080` + опциональный профиль `edge` с Caddy. Может стоять и на сервере панели, и на отдельном VPS |
| Панель | Мастер + ноды; приложение знает только мастер |

## 5. Стек

Предложен агентом и принят по умолчанию. Менять только с явного согласия владельца.

**Backend — Go 1.23+**
- Роутер: стандартный `net/http` с паттернами методов (`"GET /api/me"`). Без фреймворков.
- SQLite: `modernc.org/sqlite` (без CGO → простой Docker-образ). Миграции — встроенные SQL-файлы
  (`//go:embed`) с таблицей версий.
- Web Push: `github.com/SherClockHolmes/webpush-go`.
- Логи: `log/slog`, JSON в проде. Конфиг: переменные окружения (§10).
- Фронт встраивается в бинарник через `//go:embed web/dist` → один процесс, один порт.
- Почему Go: панель и её экосистема на Go, `.gitignore` уже под Go, статический бинарник,
  фоновый планировщик — обычная goroutine без внешних cron/queue.

**Frontend — Vite + React 18 + TypeScript**
- PWA: `vite-plugin-pwa` в режиме `injectManifest` (нужен свой `sw.ts` с обработчиками
  `push` и `notificationclick`).
- Стили: Tailwind CSS v4. Роутинг: `react-router`. Данные: `@tanstack/react-query`. QR: `qrcode.react`.
- Язык интерфейса: русский, строки в одном файле `web/src/i18n/ru.ts` (задел под другие языки).
- Почему React: самый распространённый вариант, любой агент/разработчик его знает, экосистема PWA зрелая.

## 6. Структура репозитория (целевая)

```
cmd/server/          точка входа HTTP-сервера + планировщика
cmd/pushctl/         CLI: отправка кастомных push через admin API
internal/config/     загрузка и валидация env
internal/xui/        HTTP-клиент к панели 3x-ui: типы, вызовы, кэш списка клиентов
internal/store/      SQLite: миграции, репозитории (sessions, push_subscriptions, renewal_requests, notifications)
internal/store/queries/  все SQL-запросы в .sql-файлах, встроены через go:embed; в Go только имена
internal/subtemplate/    шаблон страницы подписки 3x-ui, ведущий в кабинет (server sub-template)
internal/nginxconf/      генерация nginx-блока из APP_BASE_URL (server nginx-config)
internal/auth/       magic-link клиента и админа, сессии, rate-limit, middleware ролей
internal/push/       отправка Web Push, VAPID, удаление мёртвых подписок
internal/renewal/    сервис продления: планы, интерфейс Payment (manual), транзакция + bulkAdjust + откат
internal/notify/     планировщик: истечение/квота клиентам, напоминание админу о pending, дедупликация
internal/httpapi/    handlers, роутинг, admin API, раздача статики
internal/domain/     модель Subscription, вычисление статуса (чистые функции, без I/O)
internal/webdist/    //go:embed собранного фронта; dist/ генерируется Vite (в git только .gitkeep)
web/                 Vite-приложение (PWA); сборка пишет в ../internal/webdist/dist
deploy/              docker-compose.yml, Dockerfile, Caddyfile, nginx.example.conf, .env.example, config.example.yaml
deploy/install-nginx.sh  установка сгенерированного блока: бэкап, nginx -t, reload, проверка порта
.tools/              переносимый Node (make tools-node), не в git
Makefile             build / vet / test / check / web / tools-node / compose-*
.claude/skills/      скиллы для агентов (3x-ui-api)
AGENT.md             этот файл
```

## 7. Архитектура и потоки

**Magic-link и сессия**
1. `GET /s/{subId}` → ищем клиента в панели (`clients/list/paged?search=<subId>` и затем
   ТОЧНОЕ сравнение `subId`, поиск подстрочный) → если нет — страница «ссылка недействительна» (404).
2. Если есть — создаём запись в `sessions`, ставим cookie `sid` (httpOnly, Secure, SameSite=Lax,
   срок 90 дней, продлевается при активности), редирект `302 /`.
3. Эндпоинт защищён rate-limit по IP (например 10 попыток/мин) — subId'ы случайные 16-символьные,
   но перебор всё равно надо душить. Ответ по времени не должен отличаться для «есть/нет».
4. В БД храним `sub_id` как есть (он нужен для запросов в панель). Email хранится только в
   `renewal_requests` (админу нужно видеть, кто заявил оплату); uuid/password не храним никогда.

**Ссылка подписки = вход в кабинет.** Панель отдаёт по `{subPath}{subId}` base64 VPN-приложениям и
HTML браузеру (`Accept: text/html`). HTML берётся из `subThemeDir/index.html` — Go `html/template`
с полями `sId`, `subTitle`, `subUrl`, `expire`, `links`, … (см. docs/custom-subscription-templates.md
в 3x-ui). Наш шаблон лежит в `internal/subtemplate/index.html`, `server sub-template` / `make sub-template`
подставляют `APP_BASE_URL` и печатают файл; его копируют на хост панели и указывают путь в
Sub Theme Directory. Шаблон делает `meta refresh` + `location.replace` на `/s/{{ .sId }}`, cookie
`SameSite=Lax` переживает cross-site переход. `/api/admin/link` отдаёт `url` = ссылка подписки,
`cabinetUrl` = прямая ссылка.

**Magic-link админа** — `GET /a/{ADMIN_LINK_SECRET}` → сессия `role=admin` → `302 /admin`. Тот же
rate-limit. Секрет отдельный от `ADMIN_TOKEN`, чтобы API-токен не попадал в URL и историю браузера.

**Экран подписки** — `GET /api/me`: бэкенд берёт клиента из кэша списка (`clients/list`,
TTL 30–60 с; панель не должна получать запрос на каждый рендер), считает статус (§3), отдаёт:
срок, дней осталось, трафик (использовано/квота), `enable`, online (по `clients/onlines`)
и lastOnline, ссылку подписки и список ссылок (`clients/subLinks/{subId}`) для QR.

**Продление по доверию** — `POST /api/renew {planId}`:
1. План из `config.yaml`; свежий `clients/get/{email}` (не кэш) → `expiryTime`, `email`.
2. Отказы: `expiryTime == 0` → `409 unlimited`; есть `pending` заявка → `409 pending_exists`.
3. `addDays = days`, если `expiryTime > now`; иначе `days + ceil((now − expiryTime)/1 день)` —
   панель сдвигает от текущего `expiryTime`, поэтому истёкшему добавляем «долг». Округление вверх:
   клиент получает не меньше оплаченного.
4. Транзакция: `INSERT renewal_requests(status=pending, applied_days, expiry_before)`; частичный
   уникальный индекс `(sub_id) WHERE status='pending'` защищает от двойного нажатия.
5. `bulkAdjust {emails:[email], addDays}`. Ошибка → `status=failed`, `502`. Успех → перечитать
   клиента → `expiry_after`, ответ `200 {expiryTime}`.
6. Push всем подпискам `role=admin`: «Заявка: {email}, {days} дн, {amount} ₽», `url: /admin`.
   Нет админских подписок → только лог; заявка всё равно видна в `/admin`.

**Решение админа** — `POST /api/admin/renewals/{id}/confirm|reject`, только для `pending`:
- confirm → `status=confirmed`, push клиенту «Оплата подтверждена».
- reject → `bulkAdjust addDays = −applied_days` → перечитать → `status=rejected`, push клиенту
  «Платёж не найден, продление отменено». Если срок ушёл в прошлое, панель сама выключит клиента.
  После reject клиент может подать новую заявку.

**Push-подписка** — фронт после жеста пользователя вызывает `PushManager.subscribe`
с `VAPID_PUBLIC_KEY`, шлёт результат в `POST /api/push/subscribe`. Храним `endpoint` (PK),
`p256dh`, `auth`, `sub_id`, `user_agent`, `created_at`, `last_ok_at`, `fail_count`.
При ответе 404/410 от push-сервиса — удаляем подписку.

**Планировщик истечения** — goroutine с интервалом `POLL_INTERVAL` (по умолчанию 10 мин):
1. Тянет `clients/list` (тот же кэш).
2. Для каждого клиента с `expiryTime > 0` считает дни до конца; для каждого порога из
   `NOTIFY_DAYS` (по умолчанию `7,3,1`) и события `expired` — если порог пройден и в
   `notifications` нет записи с ключом `(sub_id, kind, expiry_time)` — шлёт push всем
   push-подпискам этого sub_id и пишет запись. Ключ включает `expiry_time`, поэтому после
   продления уведомления сработают снова.
3. Аналогично для трафика: `kind=traffic_90` при `totalGB>0` и `used/totalGB ≥ NOTIFY_TRAFFIC_PCT`.
4. Раз в сутки — напоминание админу о `pending` старше 24 ч (`kind=admin_pending_reminder`,
   `ref=<request_id>:<date>`).
5. Панель недоступна → лог warning, пропуск цикла, без ретраев в цикле.

**Кастомные push** — форма в `/admin` и `POST /api/admin/push` (cookie `role=admin` или
`Authorization: Bearer <ADMIN_TOKEN>`): `{title, body, url?, target: {"all":true} | {"subIds":[...]} | {"emails":[...]}}`.
Синхронно рассылает, отвечает `{recipients, sent, failed, removed, unknownEmails}` и пишет в
`notifications` (`kind=custom`). `cmd/pushctl` — CLI над admin API: `send`, `pending`, `history`,
`confirm`, `reject`, `link`, `stats`; читает `APP_BASE_URL`/`ADMIN_TOKEN` из окружения или `.env`.

## 8. HTTP API приложения (контракт)

| Метод и путь | Auth | Назначение |
|---|---|---|
| `GET /s/{subId}` | — | Magic-link клиента: сессия `client`, редирект на `/` |
| `GET /a/{secret}` | — | Magic-link админа: сессия `admin`, редирект на `/admin` |
| `GET /api/me` | client | Данные подписки, `hasPending`, `vapidPublicKey`, `pushSubscribed` |
| `GET /api/plans` | client | Планы + реквизиты + `hasPending` |
| `POST /api/renew` | client | «Я перевёл» → мгновенное продление |
| `GET /api/renewals` | client | Последние 10 своих заявок |
| `POST /api/push/subscribe` | client/admin | Сохранить PushSubscription |
| `DELETE /api/push/subscribe` | client/admin | Удалить по `endpoint` |
| `POST /api/logout` | any | Удалить сессию |
| `GET /api/admin/renewals?status=pending` | admin | Список заявок |
| `POST /api/admin/renewals/{id}/confirm` · `/reject` | admin | Решение по заявке |
| `POST /api/admin/push` | admin | Кастомная рассылка |
| `GET /api/admin/link?email=` | admin | Ссылка для выдачи клиенту: `url` (ссылка подписки, fallback — прямая) и `cabinetUrl` (`/s/<subId>`) |
| `GET /api/admin/stats` | admin | Кол-во сессий/push-подписок/заявок |
| `GET /healthz` | — | Живость + доступность панели (`{panel:"ok"\|"down"}`) |
| `GET /*` | — | SPA (index.html), `manifest.webmanifest`, `sw.js` |

Ошибки — `{ "error": "code", "message": "человекочитаемо" }` с правильным HTTP-кодом.
`GET /api/me` при отсутствии/протухшей сессии — `401`; фронт показывает экран
«откройте ссылку из сообщения администратора». `403` — сессия есть, но роль не та.
«admin» в колонке Auth = cookie-сессия `role=admin` **или** `Authorization: Bearer <ADMIN_TOKEN>`.

## 9. Шпаргалка по API панели (только нужное)

| Задача | Вызов |
|---|---|
| Найти клиента по subId | `GET /panel/api/clients/list/paged?search=<subId>&pageSize=50` → фильтр `item.subId == subId` (поиск подстрочный по email/subId/comment/uuid/…) |
| Все клиенты с трафиком (для планировщика) | `GET /panel/api/clients/list` → `obj[]` = `ClientRecord` + `inboundIds[]` + `traffic{}` |
| Один клиент по email | `GET /panel/api/clients/get/{email}` → `{client, inboundIds, externalLinks, usedTraffic}` |
| Счётчики трафика | `GET /panel/api/clients/traffic/{email}` (email URL-кодировать) |
| Кто онлайн | `POST /panel/api/clients/onlines` → `obj: [email]`; `POST /panel/api/clients/lastOnline` → `obj: {email: ts}` |
| Ссылки подписки (для QR) | `GET /panel/api/clients/subLinks/{subId}` → `obj: ["vless://…", …]` |
| Продлить на N дней | `POST /panel/api/clients/bulkAdjust {"emails":[…],"addDays":30}` (или `addBytes`) |
| Состояние нод | `GET /panel/api/nodes/list` → `status`, `lastHeartbeat`, `onlineCount`, `panelVersion` |
| Здоровье мастера | `GET /panel/api/server/status` → `xray.state`, `panelVersion` |
| Настройки подписки (subPath/subURI) | `POST /panel/api/setting/all` (именно POST) — нужны, если `SUB_BASE_URL` не задан |

Подводные камни:
- `clients/update/{email}` **заменяет всю запись**, а не патчит. Для продления использовать
  только `bulkAdjust`.
- `bulkAdjust` пропускает клиентов с `expiryTime=0`/`totalGB=0` (безлимит не превращается в лимит).
- Массовые операции применяются к inbound'ам параллельно: `success:false` может означать
  частичное применение. Всегда перечитывать клиента после записи.
- Пути `del*`, `delDepleted`, `delOrphans`, `resetAllTraffics`, `server/*` (кроме `status`),
  `importDB`, `updatePanel` — в этом проекте **запрещены**.
- Соединение с панелью **иногда не устанавливается** (connect timeout при следующем же запросе за 70 мс).
  HTTP-клиент в `internal/xui`: таймаут соединения 5 с, общий 20 с, одна повторная попытка для GET,
  без повторов для POST (чтобы не продлить дважды).
- Публичный `GET /<subPath>/<subId>?format=info` отдаёт JSON со сроком/трафиком без токена —
  запасной вариант, если мастер-API недоступен; в MVP не используем.

## 10. Конфигурация (переменные окружения)

`.env` в git не попадает; шаблон держим в `deploy/.env.example`.

| Переменная | Обязательна | Назначение |
|---|---|---|
| `NODE_URL` | да | Базовый URL мастер-панели с basePath, напр. `https://panel.example.com:2053/secret/` |
| `NODE_API_TOKEN` | да | Bearer-токен панели |
| `NODE_TLS_INSECURE` | нет | `1` — не проверять сертификат панели (только если самоподписанный) |
| `APP_BASE_URL` | да | Публичный URL приложения, напр. `https://cab.example.com` (для magic-link и push `url`) |
| `SUB_BASE_URL` | нет | База ссылки подписки, напр. `https://sub.example.com:2096/sub/`; пусто → читать из `setting/all` |
| `ADMIN_TOKEN` | да | Bearer-токен для `/api/admin/*` (CLI), ≥32 случайных байт |
| `ADMIN_LINK_SECRET` | да | Секрет ссылки входа админа `/a/<secret>`, ≥32 случайных байт, отдельный от `ADMIN_TOKEN` |
| `CONFIG_PATH` | нет | Путь к `config.yaml` (планы, реквизиты, тексты); по умолчанию `/data/config.yaml` |
| `VAPID_PUBLIC_KEY`, `VAPID_PRIVATE_KEY`, `VAPID_SUBJECT` | да | Ключи Web Push; `VAPID_SUBJECT` = `mailto:…` |
| `SESSION_SECRET` | да | Для подписи cookie |
| `DB_PATH` | нет | По умолчанию `/data/app.db` |
| `LISTEN_ADDR` | нет | По умолчанию `:8080` |
| `TRUST_PROXY` | нет | `1` (по умолчанию) — IP клиента брать из `X-Forwarded-For`/`X-Real-IP`; ставить `0`, если приложение смотрит в интернет напрямую |
| `POLL_INTERVAL` | нет | По умолчанию `10m` |
| `NOTIFY_DAYS` | нет | По умолчанию `7,3,1` |
| `NOTIFY_TRAFFIC_PCT` | нет | По умолчанию `90` |
| `LOG_LEVEL` | нет | `info` |

## 11. Требования PWA и iOS

- Только HTTPS. Web Push на iOS работает **только** если PWA добавлена на экран «Домой»
  (iOS 16.4+). В Safari-вкладке пушей нет — фронт определяет `navigator.standalone === false`
  на iOS и показывает инструкцию «Поделиться → На экран Домой» вместо кнопки подписки.
- `manifest.webmanifest`: `display: standalone`, `start_url: /`, иконки 192/512 + maskable,
  `apple-touch-icon` в `index.html`, `theme_color`.
- Запрос разрешения на уведомления — только по жесту пользователя (кнопка), никогда при загрузке.
- Service worker: обработчики `push` (показать `title/body`, `data.url`) и `notificationclick`
  (открыть/сфокусировать `url`). `/api/*` — только сеть (не кэшировать), app shell — precache.
- После обновления SW показывать ненавязчивый тост «доступно обновление».
- Мобильный вьюпорт: safe-area (`env(safe-area-inset-*)`), крупные тапабельные элементы,
  никаких hover-only взаимодействий.

## 12. Правила работы для агента

**Безопасность и панель**
- Панель — это **production** с живыми клиентами. Без вопросов разрешены только GET-запросы
  из §9. Любой POST в панель — только с явного подтверждения владельца в текущей задаче.
  Запрещённые пути — в §9.
- Никогда не выводить в лог, чат, тесты или коммиты: токен, `subId`, `uuid`, `password`, `email`
  клиентов. Для отладки печатать только ключи/типы/счётчики. Helper: `.claude/skills/3x-ui-api/scripts/xui.sh`
  редактирует секреты автоматически.
- `.env` не читать целиком в вывод; при необходимости показывать только имена переменных.

**Код**
- Комментарии, имена, коммиты — на английском. Тексты интерфейса и push — на русском.
- Единицы: время в панели — миллисекунды, у нас внутри — `time.Time`; байты не конвертировать
  в GB до слоя представления.
- Вся логика статусов/порогов — в `internal/domain`, чистые функции с табличными тестами.
- Клиент панели (`internal/xui`) тестировать через `httptest.Server` с записанными
  фикстурами формы `{success,msg,obj}`. Тесты никогда не ходят в реальную панель.
- Не добавлять зависимости без необходимости; перед добавлением — проверить, нет ли уже в `go.mod`/`package.json`.
- Коммиты в стиле Conventional Commits (`feat:`, `fix:`, `chore:`…), без коммита и пуша без просьбы.

**Перед завершением задачи**
```
make check          # go vet + go test + go build + typecheck фронта
make web            # полная сборка фронта в internal/webdist/dist (перед сборкой бинарника/образа)
```
Node в системе может отсутствовать: `make tools-node` скачивает переносимый Node 22 для текущей
ОС/архитектуры (Linux/macOS, x64/arm64) в `.tools/node`, `make web` подхватывает его автоматически.
`.tools/` и `web/node_modules/` привязаны к платформе: при переносе проекта на другую машину
повторить `make tools-node` и `make web`. Makefile совместим с GNU make 3.81 (macOS по умолчанию). Docker на машине разработчика может быть без прав —
образ собирается на сервере. Если что-то падает — сообщить с выводом, не маскировать.

**Локальный запуск** — `.env` содержит только `NODE_URL`/`NODE_API_TOKEN`; остальные переменные
(§10) для dev-запуска задаются в оболочке, `CONFIG_PATH=deploy/config.example.yaml`,
`DB_PATH` — во временный файл, `LISTEN_ADDR=127.0.0.1:18080`.

**Документация**
- Любое изменение архитектурного решения → обновить этот файл (§4, §5, §7, §8, §10) и добавить
  строку в §14. Не оставлять устаревшие утверждения.

## 12a. Состояние реализации (2026-09-16)

Все вехи плана M0–M6 реализованы и покрыты тестами (`make check` зелёный):

| Веха | Что есть |
|---|---|
| M0 | Каркас: config (env + yaml), store (SQLite, миграции), xui-клиент с retry и кэшем, domain, httpapi, embed фронта, Dockerfile/compose/Makefile |
| M1 | Magic-link клиента и админа, подписанные cookie-сессии, rate-limit, `/api/me`, экран подписки с QR |
| M2 | Web Push: `gen-vapid`, отправка с удалением мёртвых endpoint'ов, subscribe-эндпоинты, переключатель во фронте |
| M3 | Продление по доверию: `/api/plans`, `/api/renew`, confirm/reject с откатом, экраны `/renew` и `/admin` |
| M4 | Планировщик: пороги истечения, трафик, напоминание админу о pending > 24 ч, дедупликация |
| M5 | Рассылка: `POST /api/admin/push`, форма в админке, CLI `pushctl` |
| M6 | README, `make backup`, nginx-пример, Caddy-профиль |

Шаблон страницы подписки (`internal/subtemplate`) проверен только тестом через `html/template`;
на панель ещё не установлен (нужно скопировать файл на хост панели и задать Sub Theme Directory).
Проверено на бою 2026-09-22: рассылка доходит до FCM; доставка на iOS чинится нормализацией
`VAPID_SUBJECT` (см. §14). Не проверено на реальной панели (нужен тестовый клиент от владельца): семантика `bulkAdjust`
(сдвиг от `expiryTime` или от «сейчас»), форма ответа `bulkAdjust`, доставка push на iOS.
Проверено на реальной панели: magic-link → `/api/me` с настоящими данными, ссылка подписки из
`setting/all`, вход админа, `/api/admin/link`.

## 12b. Правила по коду

**SQL.** Ни одного SQL-литерала в `.go`. Запросы живут в `internal/store/queries/*.sql`, каждый
начинается со строки `-- name: <ключ>`; комментарий между запросами документирует следующий и
отбрасывается, заметку к запросу пишут после его `-- name:`. При старте пакета ключи привязываются
к полям `queries.Set`; недостающий ключ или поле без ключа роняют процесс сразу, а не при первом
обращении к базе. В коде — `queries.Q.PushList`. Формат `%s` допустим только в двух запросах со
списком `IN (...)`, туда подставляются лишь знаки вопроса, значения всегда биндятся параметрами.

**Тесты.** Живут рядом с кодом, как принято в Go: только так они видят непубличные функции
(`sign`, `verify`, `loadEnv`, `panelHealthy`, `expiryMessage`, `sendOne`). Отдельный каталог
`tests/` не заводим — решение владельца от 2026-09-22 делать идиоматично для языка.

**Блокировки.** Никогда не держать мьютекс во время сетевого вызова к панели. Кэш читается и
пишется под замком, сам запрос идёт снаружи; одновременный запрос отдаёт прошлое значение либо
ждёт общий результат через канал. См. `panelHealthy` и `subscriptionURL`.

## 13. Открытые вопросы

- Онлайн-оплата: провайдер выбирается, когда у владельца появится юридический статус
  (самозанятость/ИП). До этого — ручная оплата по доверию.
- Семантика `bulkAdjust`: сдвигает от текущего `expiryTime` или от «сейчас»? Проверить на тестовом
  клиенте в M3; от ответа зависит расчёт «долга» в §7.
- Показывать ли клиенту состояние нод/серверов (`nodes/list`) — полезно, но раскрывает топологию.
- Нужен ли клиенту выбор между ссылкой подписки и отдельными ссылками по протоколам.
- Мультиязычность интерфейса (сейчас только русский).
- Отрицательный `expiryTime` («отложенный старт») — подтвердить семантику на реальном клиенте.
- Политика хранения истории уведомлений (сколько держать).

## 14. Журнал решений

- 2026-09-15 — Заведён AGENT.md и скилл `3x-ui-api`. Зафиксированы: вход по magic-link (subId),
  продление как заглушка, только Web Push (VAPID), admin API + CLI для рассылок, SQLite +
  Docker Compose, стек Go + React/Vite. Проверено на живой панели: версия 3.6.x, мастер + 7 нод,
  новый API `/panel/api/clients/*` доступен, поиск по subId через `list/paged?search=`.
- 2026-09-16 — Спланирована архитектура (план `~/.claude/plans/jaunty-wibbling-crane.md`). Продление
  из заглушки стало потоком по доверию с мгновенным `bulkAdjust`, одной pending-заявкой и откатом.
  Тарифы и реквизиты — в `config.yaml`. Появился минимальный экран `/admin` и вход админа по
  `/a/<secret>`; уведомления админу — Web Push в тот же PWA. Деплой — compose с опциональным Caddy.
- 2026-09-22 — Настройка nginx автоматизирована. `server nginx-config` выводит блок из
  `APP_BASE_URL` и путей к сертификату, `deploy/install-nginx.sh` кладёт его, откатывает при
  отказе `nginx -t` и после reload проверяет, что порт занял именно nginx: без этой проверки
  занятый порт выглядит как успешная установка. Выпуск сертификата не трогаем, здесь его делает
  acme.sh, и в её `Le_ReloadCmd` уже есть `systemctl reload nginx`.
- 2026-09-22 — Рефакторинг. Весь SQL вынесен в `internal/store/queries/*.sql` с именованными
  запросами и проверкой связывания при старте. Мьютексы `healthMu` и `subMu` больше не
  удерживаются на время обращения к панели: медленная панель не выстраивает клиентов в очередь,
  добавлен регрессионный тест `TestSlowPanelDoesNotSerializeClients`. Тесты оставлены рядом с
  кодом по соглашению Go.
- 2026-09-22 — Исправлена доставка push на iOS. `webpush-go` сам дописывает `mailto:` к subject,
  не начинающемуся с `https:`, поэтому документированный `VAPID_SUBJECT=mailto:…` уходил в токен
  как `mailto:mailto:…`. FCM это глотал, Apple отвечал 403 и не доставлял ничего. Добавлена
  нормализация в `push.New` (`normalizeSubject`), принимаются обе формы и https-URL.
- 2026-09-18 — Клиенту выдаётся одна ссылка — ссылка подписки панели. Браузерный вариант
  страницы подписки заменяется нашим шаблоном (`subThemeDir`), который редиректит на
  `/s/<subId>`. Добавлены `internal/subtemplate`, `server sub-template`, `make sub-template`;
  `/api/admin/link` и `pushctl link` отдают ссылку подписки первой.
- 2026-09-18 — Проект перенесён на macOS (arm64). Makefile переведён на табы (make 3.81),
  `tools-node` определяет ОС/архитектуру. В `renewal.Service` добавлен `SetClock` — тест продления
  с просрочкой зависел от реального времени и падал через два дня после написания.
- 2026-09-16 — Реализованы вехи M0–M6 (см. §12a). Модуль `github.com/amarseillaise/3x-ui-cm`,
  Go 1.26, Vite 7 + React 19, Tailwind 4, vite-plugin-pwa 1.x. Добавлены `TRUST_PROXY`,
  `ADMIN_LINK_SECRET`, `CONFIG_PATH`; Node берётся из `.tools/node` (`make tools-node`).
