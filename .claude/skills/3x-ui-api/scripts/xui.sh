#!/usr/bin/env bash
# Safe helper for the 3x-ui master panel API.
#
#   xui.sh GET  <path> [query]        e.g. xui.sh GET /panel/api/clients/list/paged 'search=abc&pageSize=5'
#   xui.sh POST <path> [json-body]    requires XUI_ALLOW_WRITE=1; destructive paths are always refused
#
# Env: NODE_URL, NODE_API_TOKEN (loaded from repo .env unless already set or XUI_ENV_FILE points elsewhere)
#      XUI_RAW=1       print the response without redacting secrets
#      XUI_INSECURE=1  skip TLS verification (self-signed panel cert)
#      XUI_TIMEOUT=30  curl timeout in seconds (the panel can be slow on list endpoints)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)"
ENV_FILE="${XUI_ENV_FILE:-$ROOT/.env}"
if [ -f "$ENV_FILE" ]; then
  set -a; . "$ENV_FILE"; set +a
fi
: "${NODE_URL:?NODE_URL is not set}"
: "${NODE_API_TOKEN:?NODE_API_TOKEN is not set}"

method="${1:?usage: xui.sh GET|POST <path> [query|json]}"
path="${2:?path required}"
arg="${3:-}"
base="${NODE_URL%/}"
insecure=()
[ "${XUI_INSECURE:-0}" = "1" ] && insecure=(-k)

case "$method" in
  GET)
    url="$base$path"
    [ -n "$arg" ] && url="$url?$arg"
    resp="$(curl -sS "${insecure[@]}" -m "${XUI_TIMEOUT:-30}" -H "Authorization: Bearer $NODE_API_TOKEN" -H 'Accept: application/json' "$url")"
    ;;
  POST)
    case "$path" in
      */del/*|*/bulkDel*|*resetAllTraffics*|*delDepleted*|*delOrphans*|*/server/*|*importDB*|*updatePanel*|*/setting/update*|*/nodes/del*|*/inbounds/del*)
        echo "xui.sh: refusing destructive path: $path" >&2; exit 3 ;;
    esac
    if [ "${XUI_ALLOW_WRITE:-0}" != "1" ]; then
      echo "xui.sh: POST refused; set XUI_ALLOW_WRITE=1 after the owner confirmed the write" >&2; exit 2
    fi
    resp="$(curl -sS "${insecure[@]}" -m "${XUI_TIMEOUT:-30}" -X POST -H "Authorization: Bearer $NODE_API_TOKEN" \
      -H 'Content-Type: application/json' -H 'Accept: application/json' --data "${arg:-{\}}" "$base$path")"
    ;;
  *)
    echo "xui.sh: unsupported method $method" >&2; exit 1 ;;
esac

if [ "${XUI_RAW:-0}" = "1" ] || ! command -v jq >/dev/null; then
  printf '%s\n' "$resp"
else
  printf '%s' "$resp" | jq '
    def secret: IN("id","uuid","password","auth","subId","email","secret","privateKey","publicKey",
                   "preSharedKey","apiToken","token","address","publicIP","pinnedCertSha256");
    walk(if type=="object" then with_entries(
        if (.key|secret) and (.value|type)=="string" and (.value|length)>0 then .value="<redacted>"
        elif .key=="tgId" and (.value|type)=="number" and .value!=0 then .value="<redacted>"
        else . end)
      else . end)' 2>/dev/null || printf '%s\n' "$resp"
fi
