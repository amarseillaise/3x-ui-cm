---
name: 3x-ui-api
description: Work with the 3x-ui master panel REST API (v3.x, multi-node) — Bearer auth, find a client by subId/email, read expiry and traffic, safe read-only probing of the live panel via .env, extending a subscription with bulkAdjust. Use when editing internal/xui, debugging panel responses, or when the user mentions 3x-ui, панель, subId, expiryTime, inbound, node, подписка.
---

# 3x-ui API

Контекст проекта — в `AGENT.md` (§2, §3, §9). Здесь — как именно ходить в панель и что она отвечает.

## Когда использовать

- Пишешь или меняешь клиент панели в `internal/xui`.
- Нужно проверить, как панель на самом деле отвечает (форма `obj`, коды, поведение фильтров).
- Нужно продлить/скорректировать клиента (только с подтверждения владельца).

## Как обращаться к панели

Helper `scripts/xui.sh` сам читает `.env` (`NODE_URL`, `NODE_API_TOKEN`), подставляет Bearer,
и **редактирует секреты в ответе** (`id`/`uuid`/`password`/`subId`/`email`/ключи → `<redacted>`).

```bash
S=.claude/skills/3x-ui-api/scripts/xui.sh
$S GET /panel/api/server/status
$S GET /panel/api/clients/list/paged 'search=<subId>&pageSize=5'
$S GET /panel/api/clients/list | jq '.obj | length'
XUI_RAW=1 $S GET /panel/api/clients/subLinks/<subId>      # без редактирования — только если это правда нужно
XUI_ALLOW_WRITE=1 $S POST /panel/api/clients/bulkAdjust '{"emails":["<email>"],"addDays":30}'
```

- POST без `XUI_ALLOW_WRITE=1` скрипт отклоняет. Деструктивные пути отклоняет всегда.
- `XUI_INSECURE=1` — не проверять TLS-сертификат панели (сейчас у панели валидный сертификат, флаг не нужен).
- `XUI_TIMEOUT=60` — таймаут curl в секундах (по умолчанию 30; `clients/list*` иногда отвечает медленно).
- Соединение иногда не устанавливается (curl 28 connect timeout), а повтор проходит мгновенно — при таймауте просто повтори GET один раз.
- Для форм ответов печатай ключи и типы, а не значения: `jq '.obj[0] | map_values(type)'`.

## Правила

1. Панель — production. GET свободно; POST — только `bulkAdjust`, `onlines`, `lastOnline`,
   `setting/all` и только с явного подтверждения владельца для изменяющих вызовов.
2. Никогда: `del*`, `bulkDel`, `delDepleted`, `delOrphans`, `resetAllTraffics`, `importDB`,
   `updatePanel`, `server/stopXrayService`, `server/restartXrayService`, `setting/update`.
3. Не печатать токен, `subId`, `uuid`, `password`, `email` в чат, лог, тесты, коммиты.
4. Проверять `success` в ответе; HTTP 200 не означает успех.
5. `clients/update/{email}` заменяет запись целиком — не использовать для «подкрутить одно поле».

## Аутентификация

- `Authorization: Bearer <token>`; токен создаётся в панели: Settings → Security → API Token.
- Токен = полные админ-права. CSRF-заголовок для Bearer не нужен.
- Cookie-режим (`POST /login`) в проекте не используем.
- Ответ всегда `{"success": bool, "msg": string, "obj": any}`.

## Ключевые эндпоинты

| Задача | Вызов | Ответ `obj` |
|---|---|---|
| Клиент по subId | `GET /panel/api/clients/list/paged?search=<subId>&pageSize=50` | `ClientPageResponse{items[ClientSlim], total, filtered, summary}`. **Search — подстрока по email/subId/comment/uuid/password/auth/tgId**, поэтому обязательно затем `item.subId == subId` |
| Все клиенты | `GET /panel/api/clients/list` | `[ClientRecord + inboundIds[] + traffic{ClientTraffic}]` |
| Клиент по email | `GET /panel/api/clients/get/{email}` | `{client, inboundIds, externalLinks, usedTraffic}` |
| Трафик | `GET /panel/api/clients/traffic/{email}` | `ClientTraffic` (email URL-encode) |
| Онлайн | `POST /panel/api/clients/onlines` (пустое тело) | `[email]` |
| Последний онлайн | `POST /panel/api/clients/lastOnline` | `{email: unix_ms}` |
| Ссылки подписки | `GET /panel/api/clients/subLinks/{subId}` | `["vless://…", …]`, пусто если нет включённых клиентов |
| Ссылки одного клиента | `GET /panel/api/clients/links/{email}` | `[url]` |
| Продлить | `POST /panel/api/clients/bulkAdjust` `{"emails":[…],"addDays":N,"addBytes":M}` | `{adjusted, skipped:{email: reason}}` (значения могут быть отрицательными; безлимит пропускается; истёкший авто-включается) |
| Ноды | `GET /panel/api/nodes/list` | `[{name, status, lastHeartbeat, onlineCount, clientCount, panelVersion, xrayState, …}]` |
| Мастер | `GET /panel/api/server/status` | `{panelVersion, xray{state,version}, cpu, mem, uptime, …}` |
| Настройки | `POST /panel/api/setting/all` | все настройки панели, в т.ч. `subPath`, `subURI`, `subJsonPath` |

Полный список (189 эндпоинтов) — `reference/endpoints.md`. Наблюдаемые формы ответов
живой панели — `reference/shapes.md`.

## Семантика полей

| Поле | Единицы | Особые значения |
|---|---|---|
| `expiryTime` | Unix **мс** | `0` бессрочно; отрицательное — «отложенный старт», длительность `abs()` мс (не подтверждено на живых данных) |
| `totalGB` | **байты** (имя обманывает) | `0` безлимит |
| `traffic.up/down/total` | байты | `total` = квота |
| `traffic.lastOnline`, `lastSubFetch` | Unix мс | `0` = никогда |
| `reset` / `resetDay` / `resetMax` | дни / день месяца / кол-во | автосброс трафика; `0` = выкл |
| `enable` | bool | панель сама ставит `false` при истечении/исчерпании и возвращает `true` после `bulkAdjust` |
| `limitIp`, `limitHwid` | шт | `0` безлимит |

## Поведение записи (из описаний OpenAPI)

- `add`/`update`/`bulk*` применяются к inbound'ам **параллельно и независимо**: при
  `success:false` часть inbound'ов могла измениться. После записи перечитывать клиента.
- `update/{email}` — полная замена записи (не patch).
- `bulkAdjust`: клиенты с `expiryTime=0` / `totalGB=0` пропускаются по соответствующему полю.
  Клиент, выключенный **только** из-за истечения/исчерпания, включается автоматически.

## Подписочный сервер (без токена)

`GET /<subPath>/<subId>` — base64-ссылки; с `Accept: text/html` — HTML-страница;
с `?format=info` — JSON view-model (трафик, срок, онлайн). `GET /<subPath>/<subId>/hwid-status`.
В MVP не используем, но это запасной источник данных без админ-токена.

## Источники

- OpenAPI: https://docs.sanaei.dev/openapi.json (локальная копия не хранится — скачивать при необходимости).
- Docs: https://docs.sanaei.dev/docs/reference/api/clients/ , /docs/operations/multi-node/
