#!/usr/bin/env bash
# Run eval-search setup inside an isolated Docker lark-cli environment.
#
# The host account is used only to snapshot the live eval Base. The container
# then authenticates as a separate test user and runs the blind executor setup
# from dataset.jsonl, including the Base access probe, contamination preflight,
# and multi-entity lark-cli evidence collection.

set -euo pipefail

DEFAULT_BASE_TOKEN="B1mRblqjzalTgnswzilcyOA6nVe"
DEFAULT_TABLE_ID="tbl0XN0aM4RUppI1"
DEFAULT_VIEW_ID="vewJcOBB1a"

log() { echo "[eval-search-sandbox] $*"; }
die() { echo "[eval-search-sandbox] ERROR: $*" >&2; exit 1; }

usage() {
  cat <<'EOF'
Usage:
  tests/eval-search/eval-search-sandbox-run.sh [options]

Options:
  --loader-profile <name>       Host lark-cli profile that can read the eval Base
  --run-id <id>                 Snapshot run id; defaults to UTC YYYY-MM-DDTHH-MMZ
  --executor-run-id <id>        Container run id; defaults to <snapshot-run-id>-sandbox
  --subset <n>                  Keep first n cases after dataset conversion
  --dataset-file <path>         Reuse an existing dataset.jsonl and skip host snapshot
  --env-file <path>             Sandbox env file with app credentials
                                default: ~/.eval-search-sandbox.env, or
                                ~/.lark-cli-harness/sandbox.env if it exists
  --image <name>                Docker image name (default: eval-search-sandbox)
  --base-token <token>          Eval Base token
  --table-id <id>               Eval Base table id
  --view-id <id>                Eval Base view id
  --skip-collect                Only run setup/preflight; skip collect-search
  --page-size <n>               collect-search page size (default: 10)
  --fetch-top <n>               collect-search fetch count (default: 3)
  --max-query-variants <n>      collect-search query variants (default: 4)
  -h, --help                    Show this help

The env file must set LARKSUITE_CLI_APP_ID and LARKSUITE_CLI_APP_SECRET for the
isolated executor app. Set LARKSUITE_CLI_USER_ACCESS_TOKEN to avoid interactive
device-code login.
EOF
}

RUN_ID=""
EXECUTOR_RUN_ID=""
LOADER_PROFILE=""
SUBSET=""
DATASET_FILE=""
ENV_FILE=""
IMAGE_NAME="eval-search-sandbox"
BASE_TOKEN="$DEFAULT_BASE_TOKEN"
TABLE_ID="$DEFAULT_TABLE_ID"
VIEW_ID="$DEFAULT_VIEW_ID"
COLLECT="1"
PAGE_SIZE="10"
FETCH_TOP="3"
MAX_QUERY_VARIANTS="4"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --loader-profile) LOADER_PROFILE="${2:-}"; shift 2 ;;
    --run-id) RUN_ID="${2:-}"; shift 2 ;;
    --executor-run-id) EXECUTOR_RUN_ID="${2:-}"; shift 2 ;;
    --subset) SUBSET="${2:-}"; shift 2 ;;
    --dataset-file) DATASET_FILE="${2:-}"; shift 2 ;;
    --env-file) ENV_FILE="${2:-}"; shift 2 ;;
    --image) IMAGE_NAME="${2:-}"; shift 2 ;;
    --base-token) BASE_TOKEN="${2:-}"; shift 2 ;;
    --table-id) TABLE_ID="${2:-}"; shift 2 ;;
    --view-id) VIEW_ID="${2:-}"; shift 2 ;;
    --skip-collect) COLLECT="0"; shift ;;
    --page-size) PAGE_SIZE="${2:-}"; shift 2 ;;
    --fetch-top) FETCH_TOP="${2:-}"; shift 2 ;;
    --max-query-variants) MAX_QUERY_VARIANTS="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown option: $1" ;;
  esac
done

ROOT="$(git rev-parse --show-toplevel)"
[ -f "$ROOT/go.mod" ] || die "must run inside the larksuite/cli repository"

if ! command -v docker >/dev/null 2>&1; then
  die "docker is required"
fi
if ! docker info >/dev/null 2>&1; then
  die "docker daemon is not running"
fi

if [ -z "$ENV_FILE" ]; then
  if [ -f "$HOME/.eval-search-sandbox.env" ]; then
    ENV_FILE="$HOME/.eval-search-sandbox.env"
  elif [ -f "$HOME/.lark-cli-harness/sandbox.env" ]; then
    ENV_FILE="$HOME/.lark-cli-harness/sandbox.env"
  else
    ENV_FILE="$HOME/.eval-search-sandbox.env"
    mkdir -p "$(dirname "$ENV_FILE")"
    cp "$ROOT/tests/eval-search/sandbox/env.template" "$ENV_FILE"
    die "created $ENV_FILE from template; fill executor app credentials and rerun"
  fi
fi
[ -f "$ENV_FILE" ] || die "env file not found: $ENV_FILE"

set -a
# shellcheck source=/dev/null
source "$ENV_FILE"
set +a

if [ -z "${LARKSUITE_CLI_APP_ID:-}" ]; then
  die "LARKSUITE_CLI_APP_ID is not set in $ENV_FILE"
fi
if [ -z "${LARKSUITE_CLI_APP_SECRET:-}" ]; then
  die "LARKSUITE_CLI_APP_SECRET is not set in $ENV_FILE"
fi

RESOLVED_ENV_FILE="$(mktemp)"
trap 'rm -f "$RESOLVED_ENV_FILE"' EXIT

for var in \
  LARKSUITE_CLI_APP_ID \
  LARKSUITE_CLI_APP_SECRET \
  LARKSUITE_CLI_USER_ACCESS_TOKEN \
  LARKSUITE_CLI_BRAND \
  EVAL_SEARCH_AUTH_DOMAINS \
  EVAL_SEARCH_EXPECTED_EXECUTOR_NAME \
  EVAL_SEARCH_EXPECTED_EXECUTOR_OPEN_ID; do
  if [ -n "${!var+x}" ]; then
    printf '%s=%s\n' "$var" "${!var}" >> "$RESOLVED_ENV_FILE"
  fi
done

# Sandbox credentials must not leak into host-side lark-cli calls. Host commands
# should use the configured loader profile/keychain, while the container gets
# credentials only via RESOLVED_ENV_FILE.
unset \
  LARKSUITE_CLI_APP_ID \
  LARKSUITE_CLI_APP_SECRET \
  LARKSUITE_CLI_USER_ACCESS_TOKEN \
  LARKSUITE_CLI_BRAND \
  EVAL_SEARCH_AUTH_DOMAINS

if [ -z "$DATASET_FILE" ]; then
  RUN_ID="${RUN_ID:-$(date -u +%Y-%m-%dT%H-%MZ)}"
  SNAPSHOT_CMD=(
    node --experimental-strip-types tests/eval-search/eval-search-run.ts
    --snapshot-only
    --run-id "$RUN_ID"
    --base-token "$BASE_TOKEN"
    --table-id "$TABLE_ID"
    --view-id "$VIEW_ID"
  )
  if [ -n "$LOADER_PROFILE" ]; then
    SNAPSHOT_CMD+=(--loader-profile "$LOADER_PROFILE")
  fi
  if [ -n "$SUBSET" ]; then
    SNAPSHOT_CMD+=(--subset "$SUBSET")
  fi
  log "Creating host dataset snapshot: ${SNAPSHOT_CMD[*]}"
  (cd "$ROOT" && "${SNAPSHOT_CMD[@]}")
  DATASET_FILE="tests/eval-search/runs/$RUN_ID/dataset.jsonl"
else
  if [ -z "$RUN_ID" ]; then
    RUN_ID="$(echo "$DATASET_FILE" | sed -n 's#.*tests/eval-search/runs/\([^/]*\)/dataset\.jsonl#\1#p')"
  fi
  RUN_ID="${RUN_ID:-$(date -u +%Y-%m-%dT%H-%MZ)}"
fi

EXECUTOR_RUN_ID="${EXECUTOR_RUN_ID:-${RUN_ID}-sandbox}"

case "$DATASET_FILE" in
  /*) DATASET_HOST="$DATASET_FILE" ;;
  *) DATASET_HOST="$ROOT/$DATASET_FILE" ;;
esac
[ -f "$DATASET_HOST" ] || die "dataset file not found: $DATASET_HOST"

DATASET_DIR="$(cd "$(dirname "$DATASET_HOST")" && pwd)"
DATASET_HOST="$DATASET_DIR/$(basename "$DATASET_HOST")"
case "$DATASET_HOST" in
  "$ROOT"/*) DATASET_CONTAINER="/workspace/lark-cli/${DATASET_HOST#"$ROOT"/}" ;;
  *) die "dataset file must be inside repository: $DATASET_HOST" ;;
esac

log "Building sandbox image: $IMAGE_NAME"
docker build -t "$IMAGE_NAME" "$ROOT/tests/eval-search/sandbox"

log "Running isolated executor setup"
docker run --rm \
  --env-file "$RESOLVED_ENV_FILE" \
  -e EVAL_SEARCH_DATASET_FILE="$DATASET_CONTAINER" \
  -e EVAL_SEARCH_RUN_ID="$EXECUTOR_RUN_ID" \
  -e EVAL_SEARCH_BASE_TOKEN="$BASE_TOKEN" \
  -e EVAL_SEARCH_TABLE_ID="$TABLE_ID" \
  -e EVAL_SEARCH_VIEW_ID="$VIEW_ID" \
  -e EVAL_SEARCH_COLLECT="$COLLECT" \
  -e EVAL_SEARCH_PAGE_SIZE="$PAGE_SIZE" \
  -e EVAL_SEARCH_FETCH_TOP="$FETCH_TOP" \
  -e EVAL_SEARCH_MAX_QUERY_VARIANTS="$MAX_QUERY_VARIANTS" \
  --volume "$ROOT:/workspace/lark-cli" \
  --volume eval-search-gomodcache:/go/pkg/mod \
  --volume eval-search-gobuildcache:/root/.cache/go-build \
  "$IMAGE_NAME"

log "Executor run dir: tests/eval-search/runs/$EXECUTOR_RUN_ID"
