#!/usr/bin/env bash
# Install the generated nginx server block, test it, reload nginx and verify
# that the port really ended up in nginx's hands.
#
# The last check is the point of this script: `nginx -t` passes and `reload`
# returns 0 even when another process already owns the port, and nginx then
# keeps serving the old configuration without saying so.
#
#   make nginx-config            # writes deploy/nginx/cabinet.conf
#   sudo deploy/install-nginx.sh --dry-run
#   sudo deploy/install-nginx.sh
#   sudo deploy/install-nginx.sh --target /etc/nginx/conf.d/cabinet.conf
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
config="$repo_root/deploy/nginx/cabinet.conf"
name="3x-ui-cm"
target_override=""
dry_run=0

die() { printf 'error: %s\n' "$*" >&2; exit 1; }
note() { printf '%s\n' "$*"; }
run() {
	if (( dry_run )); then
		printf '  would run: %s\n' "$*"
	else
		"$@"
	fi
}

while (( $# )); do
	case $1 in
		--dry-run) dry_run=1 ;;
		--config) config=${2:?--config needs a path}; shift ;;
		--name) name=${2:?--name needs a name}; shift ;;
		--target) target_override=${2:?--target needs a path}; shift ;;
		-h|--help) sed -n '2,12p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
		*) die "unknown argument: $1" ;;
	esac
	shift
done

[[ -f $config ]] || die "$config not found. Run: make nginx-config"
command -v nginx >/dev/null || die "nginx is not installed"
(( dry_run )) || [[ $EUID -eq 0 ]] || die "run as root (sudo $0)"

port=$(grep -oE '^[[:space:]]*listen[[:space:]]+[0-9]+' "$config" | grep -oE '[0-9]+' | head -1)
[[ -n $port ]] || die "no 'listen <port>' directive in $config"
note "config: $config"
note "port:   $port"

# Debian and Ubuntu use sites-available plus a symlink; most others use conf.d.
# --target wins: it is how you replace a file that is already in place.
if [[ -n $target_override ]]; then
	target=$target_override
	link=""
elif grep -qE '^\s*include\s+\S*sites-enabled' /etc/nginx/nginx.conf 2>/dev/null; then
	target="/etc/nginx/sites-available/$name.conf"
	link="/etc/nginx/sites-enabled/$name.conf"
elif grep -qE '^\s*include\s+\S*conf\.d' /etc/nginx/nginx.conf 2>/dev/null; then
	target="/etc/nginx/conf.d/$name.conf"
	link=""
else
	die "nginx.conf includes neither conf.d nor sites-enabled; install $config by hand"
fi
note "target: $target${link:+ (+ symlink $link)}"

# A second block with the same name and port would not be an error: nginx logs
# "conflicting server name", keeps the first one and quietly ignores ours.
server_name=$(grep -oE '^[[:space:]]*server_name[[:space:]]+\S+' "$config" | awk '{print $2}' | tr -d ';' | head -1)
while IFS= read -r other; do
	[[ $other == "$target" || $other == "$link" ]] && continue
	[[ -e $other ]] || continue
	if grep -qE "^[[:space:]]*server_name[[:space:]]+${server_name}[[:space:]]*;" "$other" &&
		grep -qE "^[[:space:]]*listen[[:space:]]+(\[::\]:)?${port}\b" "$other"; then
		die "$other already serves $server_name on port $port. Replace it with: sudo $0 --target $other"
	fi
done < <(ls /etc/nginx/conf.d/*.conf /etc/nginx/sites-enabled/* 2>/dev/null || true)

backup=""
if [[ -f $target ]]; then
	if cmp -s "$config" "$target"; then
		note "target is already identical; still testing and reloading"
	else
		backup="$target.bak.$(date +%Y%m%d-%H%M%S)"
		note "backup: $backup"
		run cp -p "$target" "$backup"
	fi
fi

run install -m 0644 "$config" "$target"
[[ -z $link ]] || run ln -sfn "$target" "$link"

restore() {
	note "restoring the previous configuration"
	if [[ -n $backup ]]; then
		cp -p "$backup" "$target"
	else
		rm -f "$target"
		[[ -z $link ]] || rm -f "$link"
	fi
}

if (( dry_run )); then
	note "would run: nginx -t && systemctl reload nginx, then verify port $port"
	exit 0
fi

if ! nginx -t; then
	restore
	die "nginx rejected the configuration; nothing was changed"
fi
systemctl reload nginx

# Who owns the port, by the name ss reports for the listening socket.
port_holder() {
	ss -ltnp 2>/dev/null |
		awk -v p=":$port\$" '$4 ~ p' |
		sed -n 's/.*users:((\"\([^\"]*\)\".*/\1/p' |
		head -1
}

if ! command -v ss >/dev/null; then
	note "warning: ss is missing, cannot verify that nginx took port $port"
else
	holder=""
	for _ in 1 2 3 4 5; do   # workers need a moment to pick up the listener
		sleep 0.4
		holder=$(port_holder)
		if [[ -n $holder ]]; then break; fi
	done

	if [[ -z $holder ]]; then
		restore
		systemctl reload nginx || true
		die "nothing listens on port $port after the reload; rolled back"
	fi
	if [[ $holder != nginx ]]; then
		restore
		systemctl reload nginx || true
		die "port $port belongs to '$holder', not nginx. Choose a free port in APP_BASE_URL and rerun make nginx-config; rolled back"
	fi
	note "nginx is listening on $port"
fi
if command -v curl >/dev/null; then
	if curl -fsS --max-time 10 "https://$(grep -oE '^\s*server_name\s+\S+' "$config" | awk '{print $2}' | tr -d ';'):$port/healthz"; then
		printf '\n'
	else
		note "warning: /healthz did not answer through nginx; check that the app is up"
	fi
fi
