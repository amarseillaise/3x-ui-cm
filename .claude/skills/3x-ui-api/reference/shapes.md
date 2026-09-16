# Наблюдаемые формы ответов живой панели

Снято 2026-09-15 с мастер-панели 3.6.x. Показаны только ключи и типы, значения опущены.
Все ответы обёрнуты в `{"success": bool, "msg": string, "obj": ...}`.

## GET /panel/api/clients/list → obj[i]

```
id: number            # numeric row id (НЕ uuid)
email: string
subId: string
uuid: string
password: string
auth: string
flow: string
security: string
privateKey: string
publicKey: string
allowedIPs: string
preSharedKey: string
keepAlive: number
secret: string
adTag: string
limitIp: number
totalGB: number       # bytes
expiryTime: number    # unix ms, 0 = unlimited
enable: boolean
tgId: number
group: string
comment: string
reset: number
createdAt: number
updatedAt: number
reverse: null | object
inboundIds: number[]
traffic: { down, email, enable, expiryTime, id, inboundId, lastOnline, reset, subId, total, up, uuid }
```

## GET /panel/api/clients/get/{email} → obj

```
client: { adTag, allowedIPs, auth, comment, createdAt, email, enable, expiryTime, flow, group, id,
          keepAlive, limitIp, password, preSharedKey, privateKey, publicKey, reset, reverse, secret,
          security, subId, tgId, totalGB, updatedAt, uuid }
externalLinks: []
inboundIds: number[]
usedTraffic: number   # bytes, up+down
```

## GET /panel/api/clients/traffic/{email} → obj

```
{ down, email, enable, expiryTime, id, inboundId, lastOnline, reset, subId, total, up, uuid }
```

## GET /panel/api/clients/list/paged?search=<subId>&pageSize=5 → obj

```
{ filtered: number, groups: [], items: ClientSlim[], page: number, pageSize: number,
  summary: { active, deactive[], deactiveCount, depleted[], depletedCount, expiring[], expiringCount,
             online[], onlineCount, total }, total: number }
```
`total` — все клиенты в БД, `filtered` — после search/filter. `search` подстрочный: по одному
subId вернулся ровно 1 item, но код обязан сравнивать `subId` точно.

ClientSlim: `comment, createdAt, email, enable, expiryTime, group, inboundIds, limitHwid, limitIp,
reset, resetDay, resetMax, subId, totalGB, traffic, updatedAt` (без uuid/password/flow/tgId).

## GET /panel/api/clients/subLinks/{subId} → obj

`string[]` — `vless://…`, `vmess://…`, … по одной на inbound (и на каждый externalProxy).

## GET /panel/api/inbounds/list → obj[i] (ключи)

`clientStats[], down, enable, expiryTime, id, lastTrafficResetTime, listen, originNodeGuid, port,
protocol, remark, settings, shareAddr, shareAddrStrategy, sniffing, streamSettings, subSortIndex,
tag, total, trafficReset, trafficResetDay, up`

`clientStats[i]`: `id, inboundId, enable, email, uuid, subId, up, down, expiryTime, total, reset, lastOnline`

## GET /panel/api/nodes/list → obj[i] (ключи)

`activeCount, address, allowPrivateAddress, basePath, clientCount, configDirty, configDirtyAt, cpuPct,
createdAt, depletedCount, disabledCount, enable, guid, hasApiToken, id, inboundCount, inboundSyncMode,
inboundTags, lastError, lastHeartbeat, latencyMs, memPct, name, netDown, netUp, onlineCount, outboundTag,
panelVersion, parentGuid, pinnedCertSha256, port, remark, scheme, status, tlsVerifyMode, updatedAt,
uptimeSecs, xrayError, xrayState, xrayVersion`

## GET /panel/api/server/status → obj (ключи)

`appStats{threads,mem,uptime}, cpu, cpuCores, cpuSpeedMhz, disk, diskIO, diskTraffic, loads, logicalPro,
mem{current,total}, netIO, netTraffic, panelGuid, panelVersion, publicIP, swap, tcpCount, udpCount,
uptime, xray{state,errorMsg,version}`

## Снимок данных (для приоритизации)

- 81 клиент, 81 уникальный subId (1:1 с email), до 11 inboundIds на клиента, 7 нод.
- `totalGB = 0` у всех (трафик безлимитный); `expiryTime = 0` у 53, задан у 28; отрицательных нет.
- `traffic.total = 0` у всех.
