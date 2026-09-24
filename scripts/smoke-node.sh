#!/usr/bin/env bash

set -euo pipefail

repo_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
target=${MCP_SMOKE_TARGET:-brenet@192.168.1.203}
tool_name=get_daemon_status
tool_arguments='{}'
token_role=guest
run_tests=true

usage() {
	cat <<'EOF'
Usage: scripts/smoke-node.sh [options]

Build the current MCP worktree, stage it temporarily on an OpenSVC lab node,
start an isolated MCP instance, and call one tool directly without om ai.

Options:
  --target USER@HOST    SSH target (default: brenet@192.168.1.203)
  --tool NAME           MCP tool to call (default: get_daemon_status)
  --arguments JSON      Tool arguments object (default: {})
  --role ROLE           OpenSVC role for the temporary JWT (default: guest)
  --skip-tests          Skip the focused Go tests before building
  -h, --help            Show this help

Environment:
  MCP_SMOKE_TARGET      Alternative default SSH target

The remote JWT, candidate process, socket, binary, and request files are
temporary. The installed opensvc-daemon-mcp service is not modified.
EOF
}

while (($# > 0)); do
	case "$1" in
	--target)
		[[ $# -ge 2 ]] || { echo "missing value for --target" >&2; exit 2; }
		target=$2
		shift 2
		;;
	--tool)
		[[ $# -ge 2 ]] || { echo "missing value for --tool" >&2; exit 2; }
		tool_name=$2
		shift 2
		;;
	--arguments)
		[[ $# -ge 2 ]] || { echo "missing value for --arguments" >&2; exit 2; }
		tool_arguments=$2
		shift 2
		;;
	--role)
		[[ $# -ge 2 ]] || { echo "missing value for --role" >&2; exit 2; }
		token_role=$2
		shift 2
		;;
	--skip-tests)
		run_tests=false
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "unknown option: $1" >&2
		usage >&2
		exit 2
		;;
	esac
done

[[ $target =~ ^[A-Za-z0-9._-]+@[A-Za-z0-9._-]+$ ]] || {
	echo "invalid SSH target: $target" >&2
	exit 2
}
[[ $tool_name =~ ^[A-Za-z0-9_-]{1,128}$ ]] || {
	echo "invalid MCP tool name: $tool_name" >&2
	exit 2
}
[[ $token_role =~ ^[A-Za-z0-9:_-]{1,128}$ ]] || {
	echo "invalid OpenSVC token role: $token_role" >&2
	exit 2
}
command -v go >/dev/null || { echo "go is required" >&2; exit 1; }
command -v ssh >/dev/null || { echo "ssh is required" >&2; exit 1; }
command -v scp >/dev/null || { echo "scp is required" >&2; exit 1; }

ssh_options=(
	-o BatchMode=yes
	-o ConnectTimeout=5
	-o StrictHostKeyChecking=yes
)

local_tmp=$(mktemp -d /tmp/opensvc-mcp-smoke-build.XXXXXX)
remote_tmp=
cleanup() {
	rm -rf -- "$local_tmp"
	if [[ -n $remote_tmp && $remote_tmp =~ ^/tmp/opensvc-mcp-smoke\.[A-Za-z0-9]+$ ]]; then
		remote_suffix=${remote_tmp##*.}
		ssh "${ssh_options[@]}" "$target" \
			"sudo -n systemctl stop 'opensvc-daemon-mcp-smoke-$remote_suffix.service' >/dev/null 2>&1 || true; sudo -n rm -rf -- '$remote_tmp'" \
			>/dev/null 2>&1 || true
	fi
}
trap cleanup EXIT

binary=$local_tmp/opensvc-daemon-mcp
printf '%s\n' "$tool_name" >"$local_tmp/tool-name"
printf '%s\n' "$tool_arguments" >"$local_tmp/arguments.json"
printf '%s\n' "$token_role" >"$local_tmp/token-role"

if [[ $run_tests == true ]]; then
	echo "==> focused Go tests"
	(
		cd "$repo_dir"
		go test ./internal/core ./internal/tools
	)
fi

echo "==> build linux/amd64 candidate"
(
	cd "$repo_dir"
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -trimpath -o "$binary" ./cmd/opensvc-daemon-mcp
)
local_sha=$(sha256sum "$binary" | awk '{print $1}')
printf '%s\n' "$local_sha" >"$local_tmp/expected-sha256"
echo "candidate sha256: $local_sha"

remote_tmp=$(ssh "${ssh_options[@]}" "$target" 'mktemp -d /tmp/opensvc-mcp-smoke.XXXXXX')
[[ $remote_tmp =~ ^/tmp/opensvc-mcp-smoke\.[A-Za-z0-9]+$ ]] || {
	echo "unexpected remote temporary directory: $remote_tmp" >&2
	exit 1
}

echo "==> stage candidate on $target:$remote_tmp"
scp "${ssh_options[@]}" \
	"$binary" "$local_tmp/tool-name" "$local_tmp/arguments.json" "$local_tmp/token-role" "$local_tmp/expected-sha256" \
	"$target:$remote_tmp/"

echo "==> call $tool_name through an isolated MCP instance"
ssh "${ssh_options[@]}" "$target" "bash -s -- '$remote_tmp'" <<'REMOTE_SCRIPT'
set -euo pipefail

remote_tmp=$1
[[ $remote_tmp =~ ^/tmp/opensvc-mcp-smoke\.[A-Za-z0-9]+$ ]] || {
	echo "unsafe remote temporary directory: $remote_tmp" >&2
	exit 1
}

for command in curl jq om systemd-run; do
	command -v "$command" >/dev/null || {
		echo "$command is required on the remote node" >&2
		exit 1
	}
done
sudo -n true
jq -e 'type == "object"' "$remote_tmp/arguments.json" >/dev/null || {
	echo "--arguments must be a valid JSON object" >&2
	exit 2
}
token_role=$(<"$remote_tmp/token-role")
[[ $token_role =~ ^[A-Za-z0-9:_-]{1,128}$ ]] || {
	echo "invalid OpenSVC token role: $token_role" >&2
	exit 2
}

candidate=$remote_tmp/opensvc-daemon-mcp
socket=$remote_tmp/mcp.sock
remote_suffix=${remote_tmp##*.}
unit="opensvc-daemon-mcp-smoke-$remote_suffix"
headers=$remote_tmp/headers
response=$remote_tmp/response
response_json=$remote_tmp/response.json

cleanup_remote() {
	sudo -n systemctl stop "$unit.service" >/dev/null 2>&1 || true
	sudo -n rm -rf -- "$remote_tmp"
}
trap cleanup_remote EXIT

chmod 0755 "$candidate"
remote_sha=$(sha256sum "$candidate" | awk '{print $1}')
expected_sha=$(<"$remote_tmp/expected-sha256")
[[ $remote_sha == "$expected_sha" ]] || {
	echo "candidate checksum mismatch: expected $expected_sha, got $remote_sha" >&2
	exit 1
}

sudo -n systemd-run \
	--quiet \
	--collect \
	--unit="$unit" \
	--setenv=OPENSVC_DAEMON_URL=https://127.0.0.1:1215 \
	--setenv=OPENSVC_DAEMON_REQUEST_TIMEOUT=20s \
	--setenv=OPENSVC_MCP_SOCKET_PATH="$socket" \
	--setenv=OPENSVC_MCP_JWT_VERIFY_KEY_FILE=/var/lib/opensvc/certs/ca_certificates \
	--setenv=OPENSVC_DAEMON_TLS_INSECURE=true \
	"$candidate"

for _ in $(seq 1 50); do
	[[ -S $socket ]] && break
	sleep 0.1
done
if [[ ! -S $socket ]]; then
	echo "candidate MCP socket was not created" >&2
	sudo -n journalctl --no-pager -n 50 -u "$unit.service" >&2 || true
	exit 1
fi

# The JWT remains only in this process memory. It is never written to a file,
# passed as an MCP argument, or printed.
token=$(sudo -n om daemon auth --role "$token_role" --duration 5m --output json | jq -er '.access_token')

mcp_post() {
	local request_file=$1
	local output_file=$2
	local header_file=$3
	local session_id=${4:-}
	local curl_args=(
		--silent
		--show-error
		--fail-with-body
		--unix-socket "$socket"
		--request POST
		--url http://localhost/mcp
		--header 'Content-Type: application/json'
		--header 'Accept: application/json, text/event-stream'
		--data-binary "@$request_file"
		--output "$output_file"
	)
	if [[ -n $header_file ]]; then
		curl_args+=(--dump-header "$header_file")
	fi
	if [[ -n $session_id ]]; then
		curl_args+=(
			--header "Mcp-Session-Id: $session_id"
			--header 'MCP-Protocol-Version: 2025-06-18'
		)
	fi
	printf 'header = "Authorization: Bearer %s"\n' "$token" |
		sudo -n curl --config - "${curl_args[@]}"
}

extract_json_response() {
	local input_file=$1
	local output_file=$2
	if jq -e . "$input_file" >/dev/null 2>&1; then
		cp "$input_file" "$output_file"
		return
	fi
	sed -n 's/^data: //p' "$input_file" | tr -d '\r' | tail -n 1 >"$output_file"
	jq -e . "$output_file" >/dev/null
}

cat >"$remote_tmp/initialize.json" <<'EOF'
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"opensvc-mcp-smoke","version":"1"}}}
EOF
mcp_post "$remote_tmp/initialize.json" "$response" "$headers"
extract_json_response "$response" "$response_json"
jq -e '.result.protocolVersion == "2025-06-18"' "$response_json" >/dev/null

session_id=$(awk 'BEGIN {IGNORECASE=1} /^Mcp-Session-Id:/ {gsub("\\r", "", $2); print $2}' "$headers" | tail -n 1)
[[ -n $session_id ]] || { echo "MCP initialization returned no session ID" >&2; exit 1; }

cat >"$remote_tmp/initialized.json" <<'EOF'
{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}
EOF
mcp_post "$remote_tmp/initialized.json" /dev/null /dev/null "$session_id"

tool_name=$(<"$remote_tmp/tool-name")
jq -n \
	--arg name "$tool_name" \
	--slurpfile arguments "$remote_tmp/arguments.json" \
	'{jsonrpc:"2.0",id:2,method:"tools/call",params:{name:$name,arguments:$arguments[0]}}' \
	>"$remote_tmp/call.json"
mcp_post "$remote_tmp/call.json" "$response" /dev/null "$session_id"
extract_json_response "$response" "$response_json"

if ! jq -e '.error == null and .result.isError != true and (.result.structuredContent | type == "object")' "$response_json" >/dev/null; then
	echo "MCP tool call failed:" >&2
	jq '.error // .result' "$response_json" >&2
	exit 1
fi

printf 'remote sha256: %s\n' "$remote_sha"
printf 'tool result:\n'
jq '.result.structuredContent' "$response_json"
REMOTE_SCRIPT

remote_tmp=
echo "==> smoke test completed; installed service unchanged"
