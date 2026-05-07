#!/usr/bin/env bash
# Container-side entrypoint for eval-search sandbox runs.

set -euo pipefail

log() { echo "[eval-search-sandbox] $*"; }
die() { echo "[eval-search-sandbox] ERROR: $*" >&2; exit 1; }
step() { echo ""; echo "[eval-search-sandbox] == $* =="; }

import_uat_session() {
  local helper_dir=/workspace/lark-cli/.tmp/eval-search-uat-import
  local helper_file="$helper_dir/main.go"
  local helper_rel_file="./.tmp/eval-search-uat-import/main.go"

  mkdir -p "$helper_dir"
  cat > "$helper_file" <<'EOF'
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/larksuite/cli/internal/auth"
	"github.com/larksuite/cli/internal/core"
)

func fetchUserInfo(brand core.LarkBrand, uat string) (string, string, error) {
	ep := core.ResolveEndpoints(brand)
	req, err := http.NewRequest(http.MethodGet, ep.Open+"/open-apis/authen/v1/user_info", nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+uat)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			OpenID string `json:"open_id"`
			Name   string `json:"name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", err
	}
	if result.Code != 0 {
		return "", "", fmt.Errorf("user_info API error [%d]: %s", result.Code, result.Msg)
	}
	if result.Data.OpenID == "" {
		return "", "", fmt.Errorf("user_info returned empty open_id")
	}
	return result.Data.OpenID, result.Data.Name, nil
}

func main() {
	uat := os.Getenv("LARKSUITE_CLI_USER_ACCESS_TOKEN")
	if uat == "" {
		fmt.Fprintln(os.Stderr, "LARKSUITE_CLI_USER_ACCESS_TOKEN is required")
		os.Exit(1)
	}

	multi, err := core.LoadMultiAppConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}
	app := multi.CurrentAppConfig("")
	if app == nil {
		fmt.Fprintln(os.Stderr, "no active app config")
		os.Exit(1)
	}

	if app.Brand == "" {
		app.Brand = core.BrandFeishu
	}

	openID, name, err := fetchUserInfo(app.Brand, uat)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fetch user info: %v\n", err)
		os.Exit(1)
	}

	app.Users = []core.AppUser{{UserOpenId: openID, UserName: name}}
	if app.DefaultAs == "" {
		app.DefaultAs = core.AsUser
	}
	if err := core.SaveMultiAppConfig(multi); err != nil {
		fmt.Fprintf(os.Stderr, "save config: %v\n", err)
		os.Exit(1)
	}

	now := time.Now().UnixMilli()
	expiresAt := now + int64(24*time.Hour/time.Millisecond)
	token := &auth.StoredUAToken{
		UserOpenId:       openID,
		AppId:            app.AppId,
		AccessToken:      uat,
		RefreshToken:     "",
		ExpiresAt:        expiresAt,
		RefreshExpiresAt: expiresAt,
		Scope:            "",
		GrantedAt:        now,
	}
	if err := auth.SetStoredToken(token); err != nil {
		fmt.Fprintf(os.Stderr, "store token: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Imported UAT for %s (%s)\n", name, openID)
}
EOF

  (
    cd /workspace/lark-cli
    go run "$helper_rel_file"
  )
  rm -rf "$helper_dir"
}

device_code_login() {
  local domains="${EVAL_SEARCH_AUTH_DOMAINS:-base,drive,docs,wiki,task,contact,im,minutes,vc,mail,calendar}"
  log "Starting device-code auth for domains: $domains"

  local no_wait
  no_wait="$(lark-cli auth login --domain "$domains" --no-wait --json)"

  local verification_url
  local device_code
  verification_url="$(printf '%s' "$no_wait" | node -e 'const fs=require("fs"); const j=JSON.parse(fs.readFileSync(0,"utf8")); console.log(j.verification_url || j.data?.verification_url || "");')"
  device_code="$(printf '%s' "$no_wait" | node -e 'const fs=require("fs"); const j=JSON.parse(fs.readFileSync(0,"utf8")); console.log(j.device_code || j.data?.device_code || "");')"

  if [ -z "$verification_url" ] || [ -z "$device_code" ]; then
    die "auth login --no-wait did not return verification_url/device_code"
  fi

  echo ""
  echo "[eval-search-sandbox] Open this URL in a browser to authorize the sandbox executor:"
  echo "[eval-search-sandbox] $verification_url"
  echo ""
  lark-cli auth login --device-code "$device_code"
}

: "${EVAL_SEARCH_DATASET_FILE:?EVAL_SEARCH_DATASET_FILE is required}"
: "${EVAL_SEARCH_RUN_ID:?EVAL_SEARCH_RUN_ID is required}"
: "${LARKSUITE_CLI_APP_ID:?LARKSUITE_CLI_APP_ID is required}"
: "${LARKSUITE_CLI_APP_SECRET:?LARKSUITE_CLI_APP_SECRET is required}"

cd /workspace/lark-cli

step "Install lark-cli"
make install

step "Initialize isolated config"
printf '%s' "$LARKSUITE_CLI_APP_SECRET" \
  | env -u LARKSUITE_CLI_APP_ID -u LARKSUITE_CLI_APP_SECRET \
      lark-cli config init \
        --app-id "$LARKSUITE_CLI_APP_ID" \
        --app-secret-stdin \
        --brand "${LARKSUITE_CLI_BRAND:-feishu}"

unset LARKSUITE_CLI_APP_ID LARKSUITE_CLI_APP_SECRET

step "Authenticate isolated executor"
if [ -n "${LARKSUITE_CLI_USER_ACCESS_TOKEN:-}" ]; then
  log "Importing injected UAT"
  import_uat_session
else
  device_code_login
fi

AUTH_JSON="$(lark-cli auth status 2>&1 || true)"
TOKEN_STATUS="$(printf '%s' "$AUTH_JSON" | jq -r '.tokenStatus // .token_status // empty' 2>/dev/null || true)"
IDENTITY="$(printf '%s' "$AUTH_JSON" | jq -r '.identity // empty' 2>/dev/null || true)"
if [ "$TOKEN_STATUS" != "valid" ]; then
  die "lark-cli auth is not valid after login: identity=$IDENTITY token_status=$TOKEN_STATUS"
fi
USER_OPEN_ID="$(printf '%s' "$AUTH_JSON" | jq -r '.userOpenId // .user_open_id // empty' 2>/dev/null || true)"
USER_NAME="$(printf '%s' "$AUTH_JSON" | jq -r '.userName // .user_name // empty' 2>/dev/null || true)"
if [ -n "${EVAL_SEARCH_EXPECTED_EXECUTOR_OPEN_ID:-}" ] && [ "$USER_OPEN_ID" != "$EVAL_SEARCH_EXPECTED_EXECUTOR_OPEN_ID" ]; then
  die "isolated executor user_open_id mismatch: got $USER_OPEN_ID, expected $EVAL_SEARCH_EXPECTED_EXECUTOR_OPEN_ID"
fi
if [ -n "${EVAL_SEARCH_EXPECTED_EXECUTOR_NAME:-}" ] && [ "$USER_NAME" != "$EVAL_SEARCH_EXPECTED_EXECUTOR_NAME" ]; then
  die "isolated executor user_name mismatch: got $USER_NAME, expected $EVAL_SEARCH_EXPECTED_EXECUTOR_NAME"
fi
log "Auth status valid for isolated executor: ${USER_NAME:-unknown} (${USER_OPEN_ID:-unknown})"

step "Run eval-search setup"
node --experimental-strip-types tests/eval-search/eval-search-run.ts \
  --dataset-file "$EVAL_SEARCH_DATASET_FILE" \
  --run-id "$EVAL_SEARCH_RUN_ID" \
  --base-token "${EVAL_SEARCH_BASE_TOKEN:-OOoEbNWhcaFOdisXDW7c0lKtn4g}" \
  --table-id "${EVAL_SEARCH_TABLE_ID:-tblGWdc19tKFZC6K}" \
  --view-id "${EVAL_SEARCH_VIEW_ID:-vewGToSnWl}"

RUN_DIR="tests/eval-search/runs/$EVAL_SEARCH_RUN_ID"

if [ "${EVAL_SEARCH_COLLECT:-1}" = "1" ]; then
  step "Collect blind lark-cli evidence"
  node --experimental-strip-types tests/eval-search/eval-search-collect-search.ts \
    --run-dir "$RUN_DIR" \
    --page-size "${EVAL_SEARCH_PAGE_SIZE:-10}" \
    --fetch-top "${EVAL_SEARCH_FETCH_TOP:-3}" \
    --max-query-variants "${EVAL_SEARCH_MAX_QUERY_VARIANTS:-4}"
else
  log "Collect step skipped"
fi

step "Done"
log "Run dir: $RUN_DIR"
