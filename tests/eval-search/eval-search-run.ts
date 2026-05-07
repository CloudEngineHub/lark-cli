#!/usr/bin/env node

const { spawnSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");

const DEFAULT_BASE_TOKEN = "OOoEbNWhcaFOdisXDW7c0lKtn4g";
const DEFAULT_TABLE_ID = "tblGWdc19tKFZC6K";
const DEFAULT_VIEW_ID = "vewGToSnWl";
const PAGE_LIMIT = 100;

function usage() {
  console.log(`Usage:
  node --experimental-strip-types tests/eval-search/eval-search-run.ts [options]

Options:
  --loader-profile <name>     lark-cli profile that can read the eval Base
  --executor-profile <name>   lark-cli profile used for blind drive search
  --run-id <id>               run id, defaults to UTC YYYY-MM-DDTHH-MMZ
  --subset <n>                keep first n cases after dataset conversion
  --snapshot-only             fetch dataset locally, then stop before blind checks
  --dataset-file <path>       reuse an existing dataset.jsonl instead of Base fetch
  --base-token <token>        eval Base token
  --table-id <id>             eval Base table id
  --view-id <id>              eval Base view id
  --help                      show this help

The runner is deterministic for the setup phase: it fetches the live dataset
with the loader profile, verifies the executor profile cannot read that Base,
then writes dataset.jsonl and preflight.json. It does not run the AI executor
phase itself.

Two-step strict mode:
  1. node --experimental-strip-types tests/eval-search/eval-search-run.ts --snapshot-only --loader-profile <base-reader>
  2. Remove the executor account's Base permission.
  3. node --experimental-strip-types tests/eval-search/eval-search-run.ts --dataset-file tests/eval-search/runs/<snapshot-run>/dataset.jsonl --executor-profile <blind-runner>`);
}

function parseArgs(argv) {
  const out: any = {
    loaderProfile: "",
    executorProfile: "",
    runId: "",
    subset: null,
    snapshotOnly: false,
    datasetFile: "",
    baseToken: DEFAULT_BASE_TOKEN,
    tableId: DEFAULT_TABLE_ID,
    viewId: DEFAULT_VIEW_ID,
  };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    const next = () => {
      if (i + 1 >= argv.length) {
        throw new Error(`missing value for ${arg}`);
      }
      i += 1;
      return argv[i];
    };
    if (arg === "--help" || arg === "-h") {
      out.help = true;
    } else if (arg === "--loader-profile") {
      out.loaderProfile = next();
    } else if (arg === "--executor-profile") {
      out.executorProfile = next();
    } else if (arg === "--run-id") {
      out.runId = next();
    } else if (arg === "--subset") {
      const value = Number.parseInt(next(), 10);
      if (!Number.isFinite(value) || value <= 0) {
        throw new Error("--subset must be a positive integer");
      }
      out.subset = value;
    } else if (arg === "--snapshot-only") {
      out.snapshotOnly = true;
    } else if (arg === "--dataset-file") {
      out.datasetFile = next();
    } else if (arg === "--base-token") {
      out.baseToken = next();
    } else if (arg === "--table-id") {
      out.tableId = next();
    } else if (arg === "--view-id") {
      out.viewId = next();
    } else {
      throw new Error(`unknown option ${arg}`);
    }
  }
  if (out.snapshotOnly && out.datasetFile) {
    throw new Error("--snapshot-only cannot be combined with --dataset-file");
  }
  return out;
}

function utcRunId(date = new Date()) {
  const iso = date.toISOString();
  return iso.slice(0, 16).replace(/:/g, "-");
}

function repoRoot() {
  const result = spawnSync("git", ["rev-parse", "--show-toplevel"], {
    encoding: "utf8",
  });
  if (result.status !== 0) {
    throw new Error("must run inside a git worktree");
  }
  return result.stdout.trim();
}

function ensureDir(dir) {
  fs.mkdirSync(dir, { recursive: true });
}

function profilePrefix(profile) {
  return profile ? ["--profile", profile] : [];
}

function parseJsonOutput(stdout) {
  const text = String(stdout || "").trim();
  if (!text) {
    throw new Error("empty stdout");
  }
  const start = Math.min(
    ...["{", "["]
      .map((needle) => text.indexOf(needle))
      .filter((idx) => idx >= 0),
  );
  if (!Number.isFinite(start)) {
    throw new Error(`stdout does not contain JSON: ${text.slice(0, 120)}`);
  }
  return JSON.parse(text.slice(start));
}

function runCommand(cmd, args, opts: any = {}) {
  const result = spawnSync(cmd, args, {
    cwd: opts.cwd,
    encoding: "utf8",
    maxBuffer: 64 * 1024 * 1024,
  });
  return {
    cmd,
    args,
    status: result.status,
    stdout: result.stdout || "",
    stderr: result.stderr || "",
    ok: result.status === 0,
  };
}

function runJson(cmd, args, opts = {}) {
  const result = runCommand(cmd, args, opts);
  let parsed = null;
  let parseError = "";
  try {
    parsed = parseJsonOutput(result.stdout);
  } catch (err) {
    parseError = err.message;
  }
  return { ...result, json: parsed, parseError };
}

function runLarkJson(profile, args, opts = {}) {
  return runJson("lark-cli", [...profilePrefix(profile), ...args], opts);
}

function commandText(result) {
  return [result.cmd, ...result.args].join(" ");
}

function summarizeFailure(result) {
  const pieces = [];
  pieces.push(`${commandText(result)} exited ${result.status}`);
  if (result.json && result.json.error) {
    const err = result.json.error;
    const detail = err.code ? `code ${err.code}` : "";
    pieces.push([err.type, detail, err.message].filter(Boolean).join(" / "));
  } else if (result.parseError) {
    pieces.push(result.parseError);
  }
  const stderr = result.stderr.trim();
  if (stderr) {
    pieces.push(stderr.split("\n").slice(-3).join(" "));
  }
  const stdout = result.stdout.trim();
  if (!result.json && stdout) {
    pieces.push(stdout.split("\n").slice(-3).join(" "));
  }
  return pieces.filter(Boolean).join(": ");
}

function readTaintedTokens(root) {
  const file = path.join(
    root,
    "skills/eval-search/references/known-tainted-tokens.md",
  );
  const text = fs.readFileSync(file, "utf8");
  const block = text.match(/tainted_tokens:[\s\S]*?```/);
  if (!block) {
    return [];
  }
  const tokens = [];
  for (const line of block[0].split("\n")) {
    const match = line.match(/^\s*-\s*([A-Za-z0-9_:-]+)/);
    if (match) {
      tokens.push(match[1]);
    }
  }
  return tokens;
}

function addTokensFromValue(value, tokens) {
  if (Array.isArray(value)) {
    for (const item of value) {
      addTokensFromValue(item, tokens);
    }
    return;
  }
  if (value && typeof value === "object") {
    for (const item of Object.values(value)) {
      addTokensFromValue(item, tokens);
    }
    return;
  }
  if (typeof value !== "string") {
    return;
  }
  for (const match of value.match(/[A-Za-z0-9_-]{12,}/g) || []) {
    tokens.add(match);
  }
}

function readRunTaintedTokens(runDir) {
  const file = path.join(runDir, "cloud-doc", "tainted_tokens.json");
  if (!fs.existsSync(file)) {
    return [];
  }
  const tokens = new Set();
  addTokensFromValue(JSON.parse(fs.readFileSync(file, "utf8")), tokens);
  return [...tokens];
}

function readExcludedUserIds(root) {
  const file = path.join(
    root,
    "skills/eval-search/references/known-tainted-tokens.md",
  );
  const text = fs.readFileSync(file, "utf8");
  const block = text.match(/excluded_user_ids:[\s\S]*?```/);
  if (!block) {
    return [];
  }
  const ids = [];
  for (const line of block[0].split("\n")) {
    const match = line.match(/^\s*-\s*(ou_[A-Za-z0-9_]+)/);
    if (match) {
      ids.push(match[1]);
    }
  }
  return ids;
}

function gitValue(args, fallback = "") {
  const result = runCommand("git", args);
  return result.ok ? result.stdout.trim() : fallback;
}

function larkCliVersion() {
  const result = runCommand("lark-cli", ["--version"]);
  return result.ok ? result.stdout.trim() : "unknown";
}

function writeJson(file, value) {
  fs.writeFileSync(file, `${JSON.stringify(value, null, 2)}\n`);
}

function writeSummary(runDir, summary) {
  writeJson(path.join(runDir, "summary.json"), summary);
}

function writeMeta(runDir, meta) {
  writeJson(path.join(runDir, "meta.json"), meta);
}

function printRunResult(root, runDir, summary, logger = console.log) {
  logger(
    JSON.stringify(
      {
        run_id: summary.run_id,
        status: summary.status,
        run_dir: path.relative(root, runDir),
        dataset_size: summary.dataset_size,
        primary_bottleneck: summary.primary_bottleneck,
        blockers: summary.blockers,
      },
      null,
      2,
    ),
  );
}

function baseRecordArgs(config, limit, offset) {
  return [
    "base",
    "+record-list",
    "--as",
    "user",
    "--format",
    "json",
    "--base-token",
    config.baseToken,
    "--table-id",
    config.tableId,
    "--view-id",
    config.viewId,
    "--limit",
    String(limit),
    "--offset",
    String(offset),
  ];
}

function assertOkEnvelope(result) {
  if (!result.ok || !result.json || result.json.ok === false) {
    throw new Error(summarizeFailure(result));
  }
  return result.json;
}

function fetchAllBaseRows(config, runDir) {
  const pages = [];
  let combined = null;
  let offset = 0;
  while (true) {
    const result = runLarkJson(
      config.loaderProfile,
      baseRecordArgs(config, PAGE_LIMIT, offset),
    );
    const envelope = assertOkEnvelope(result);
    const data = envelope.data;
    if (!data || !Array.isArray(data.data)) {
      throw new Error("base +record-list returned unexpected data shape");
    }
    pages.push(data);
    if (!combined) {
      combined = {
        data: [],
        field_id_list: data.field_id_list || [],
        fields: data.fields || [],
        record_id_list: [],
        has_more: false,
      };
    }
    combined.data.push(...data.data);
    combined.record_id_list.push(...(data.record_id_list || []));
    if (!data.has_more) {
      break;
    }
    if (data.data.length === 0) {
      throw new Error("base +record-list returned has_more=true with empty page");
    }
    offset += data.data.length;
  }

  ensureDir(path.join(runDir, "raw"));
  writeJson(path.join(runDir, "raw/base_records_pages.json"), pages);
  writeJson(path.join(runDir, "raw/base_records_combined.json"), combined);
  return combined || { data: [], fields: [], field_id_list: [], record_id_list: [] };
}

function rowValue(row, index) {
  if (index < 0 || index >= row.length) {
    return null;
  }
  return row[index];
}

function valueToString(value) {
  if (value === null || value === undefined) {
    return "";
  }
  if (typeof value === "string") {
    return value;
  }
  return JSON.stringify(value);
}

function hasKnowledge(value) {
  if (typeof value === "string") {
    return value.trim() === "是";
  }
  if (Array.isArray(value)) {
    return value.some((item) => String(item).trim() === "是");
  }
  return false;
}

function cutSection(text, marker) {
  const start = text.indexOf(marker);
  if (start < 0) {
    return null;
  }
  const bodyStart = start + marker.length;
  let end = text.length;
  for (const other of ["【关键信息】", "【辅助信息】", "【打分备注】"]) {
    if (other === marker) {
      continue;
    }
    const pos = text.indexOf(other, bodyStart);
    if (pos >= 0 && pos < end) {
      end = pos;
    }
  }
  return {
    section: text.slice(bodyStart, end).replace(/^[:：]/, "").trim(),
    rest: text.slice(end).trim(),
  };
}

function parseExpected(text) {
  const out: any = { key_points: "", aux_info: "", rubric_notes: {} };
  const key = cutSection(text, "【关键信息】");
  if (!key) {
    throw new Error("missing 关键信息 section");
  }
  const aux = cutSection(key.rest, "【辅助信息】");
  if (!aux) {
    throw new Error("missing 辅助信息 section");
  }
  const notes = cutSection(aux.rest, "【打分备注】");
  if (!notes) {
    throw new Error("missing 打分备注 section");
  }
  out.key_points = key.section;
  out.aux_info = aux.section;
  out.rubric_notes_raw = notes.section;
  if (!notes.section) {
    out.rubric_notes_parse_warning = "empty 打分备注 section";
    return out;
  }
  try {
    out.rubric_notes = JSON.parse(notes.section);
  } catch (err) {
    out.rubric_notes_parse_warning = `invalid 打分备注 JSON: ${err.message}`;
  }
  return out;
}

function extractUrls(text) {
  const matches = String(text).match(/https?:\/\/[^\s)]+/g) || [];
  const seen = new Set();
  const urls = [];
  for (let url of matches) {
    url = url.replace(/[.,;，。；]+$/g, "");
    if (!seen.has(url)) {
      seen.add(url);
      urls.push(url);
    }
  }
  return urls;
}

function convertDataset(baseData, subset) {
  const fieldIndex = new Map();
  (baseData.fields || []).forEach((field, index) => {
    fieldIndex.set(field, index);
  });
  const cases = [];
  let skippedEmptyQuery = 0;
  for (let i = 0; i < baseData.data.length; i += 1) {
    const row = baseData.data[i];
    const query = valueToString(rowValue(row, fieldIndex.get("query"))).trim();
    if (!query) {
      skippedEmptyQuery += 1;
      continue;
    }
    const expectedText = valueToString(rowValue(row, fieldIndex.get("预期答复（机评文本）")));
    const sourceText = valueToString(rowValue(row, fieldIndex.get("数据源地址")));
    const recordId = (baseData.record_id_list || [])[i] || "";
    const item: any = {
      case_id: `case_${String(cases.length + 1).padStart(3, "0")}`,
      record_id: recordId,
      query,
      has_knowledge: hasKnowledge(rowValue(row, fieldIndex.get("企业内是否有知识"))),
      expected: { key_points: "", aux_info: "", rubric_notes: {} },
      source_urls: extractUrls(sourceText),
    };
    try {
      item.expected = parseExpected(expectedText);
    } catch (err) {
      item.parse_error = true;
      item.parse_message = err.message;
    }
    cases.push(item);
    if (subset && cases.length >= subset) {
      break;
    }
  }
  return { cases, skippedEmptyQuery };
}

function writeDataset(runDir, cases) {
  const text = cases.map((item) => JSON.stringify(item)).join("\n");
  fs.writeFileSync(path.join(runDir, "dataset.jsonl"), `${text}\n`);
}

function readDatasetFile(root, datasetFile, subset) {
  const file = path.isAbsolute(datasetFile)
    ? datasetFile
    : path.join(root, datasetFile);
  const text = fs.readFileSync(file, "utf8");
  const cases = [];
  for (const [index, line] of text.split(/\r?\n/).entries()) {
    if (!line.trim()) {
      continue;
    }
    try {
      cases.push(JSON.parse(line));
    } catch (err) {
      throw new Error(`cannot parse ${file}:${index + 1}: ${err.message}`);
    }
    if (subset && cases.length >= subset) {
      break;
    }
  }
  return { cases, sourceFile: file };
}

function executorCanReadBase(config) {
  const result = runLarkJson(
    config.executorProfile,
    baseRecordArgs(config, 1, 0),
  );
  if (result.ok && result.json && result.json.ok !== false) {
    return { canRead: true, failure: "" };
  }
  const summary = summarizeFailure(result);
  if (
    summary.includes("91403") ||
    summary.includes("403") ||
    summary.includes("permission")
  ) {
    return { canRead: false, failure: summary };
  }
  return { canRead: null, failure: summary };
}

function extractResultTokens(searchResult) {
  const results = searchResult?.data?.results || [];
  const tokens = [];
  for (const item of results) {
    const meta = item.result_meta || {};
    if (meta.token) {
      tokens.push(meta.token);
    }
    if (meta.url) {
      const urlMatch = String(meta.url).match(/\/(?:docx|wiki|base|sheets|file)\/([^/?#]+)/);
      if (urlMatch) {
        tokens.push(urlMatch[1]);
      }
    }
  }
  return [...new Set(tokens)];
}

function stripHighlights(text) {
  return String(text || "").replace(/<\/?h[b]?>/g, "");
}

function looksLikeEvaluationArtifact(item) {
  const title = stripHighlights(item.title_highlighted);
  const summary = stripHighlights(item.summary_highlighted);
  const text = `${title} ${summary}`;
  return (
    /评测集|评测\s*Case|评测\s*case|case\s*分析|golden\s*set|Golden\s*Set|openclaw-竞对评测/i.test(text) ||
    /Agentic评测|知识问答评测|追问评测|意图_改写评测|搜索cli专项评测/i.test(text)
  );
}

function extractHeuristicTaintedHits(searchResult) {
  const results = searchResult?.data?.results || [];
  return results
    .filter(looksLikeEvaluationArtifact)
    .map((item) => {
      const meta = item.result_meta || {};
      return {
        token: meta.token || "",
        url: meta.url || "",
        title: stripHighlights(item.title_highlighted),
      };
    })
    .filter((item) => item.token || item.url || item.title);
}

function runPreflight(config, cases, taintedTokens) {
  const tainted = new Set(taintedTokens);
  const rows = [];
  for (const item of cases) {
    const result = runLarkJson(config.executorProfile, [
      "drive",
      "+search",
      "--as",
      "user",
      "--query",
      item.query,
      "--page-size",
      "20",
    ]);
    if (!result.ok || !result.json || result.json.ok === false) {
      rows.push({
        case_id: item.case_id,
        query: item.query,
        contamination_risk: true,
        tainted_tokens: [],
        top_20_tokens: [],
        error: summarizeFailure(result),
      });
      continue;
    }
    const tokens = extractResultTokens(result.json);
    const taintedHits = tokens.filter((token) => tainted.has(token));
    const heuristicHits = extractHeuristicTaintedHits(result.json);
    rows.push({
      case_id: item.case_id,
      query: item.query,
      contamination_risk: taintedHits.length > 0 || heuristicHits.length > 0,
      tainted_tokens: taintedHits,
      heuristic_hits: heuristicHits,
      top_20_tokens: tokens,
    });
  }
  return rows;
}

function makeBaseMeta(config, auth, startedAt): any {
  return {
    run_id: config.runId,
    started_at: startedAt,
    ended_at: new Date().toISOString(),
    lark_cli_version: larkCliVersion(),
    git_head: gitValue(["rev-parse", "HEAD"]),
    git_dirty: gitValue(["status", "--short"]) !== "",
    loader_profile: config.loaderProfile || "default",
    executor_profile: config.executorProfile || "default",
    user_open_id: auth?.userOpenId || "",
    user_name: auth?.userName || "",
    subset: config.subset,
    snapshot_only: config.snapshotOnly,
    dataset_file: config.datasetFile || "",
  };
}

function blockedSummary(config, primary, blockers, extra: any = {}) {
  return {
    run_id: config.runId,
    status: "blocked",
    dataset_size: extra.datasetSize || 0,
    scored: 0,
    contaminated_skipped: 0,
    parse_error_cases: extra.parseErrorCases || [],
    primary_bottleneck: primary,
    totals: {
      sum: 0,
      max: 0,
      percent: null,
      per_dim: { recall: null, accuracy: null, completeness: null },
    },
    findings: [],
    pollution_warnings: extra.pollutionWarnings || [],
    blockers,
  };
}

function main() {
  const config = parseArgs(process.argv.slice(2));
  if (config.help) {
    usage();
    return;
  }
  const root = repoRoot();
  config.runId = config.runId || utcRunId();
  const startedAt = new Date().toISOString();
  const runDir = path.join(root, "tests/eval-search/runs", config.runId);
  ensureDir(runDir);
  ensureDir(path.join(runDir, "trajectories"));

  const excluded = readExcludedUserIds(root);
  const taintedTokens = [
    ...new Set([
      ...readTaintedTokens(root),
      ...readRunTaintedTokens(runDir),
    ]),
  ];

  if (config.snapshotOnly) {
    const loaderAuthResult = runLarkJson(config.loaderProfile, ["auth", "status"]);
    const loaderAuth = loaderAuthResult.ok && loaderAuthResult.json ? loaderAuthResult.json : null;
    if (!loaderAuth || loaderAuth.ok === false) {
      const meta = makeBaseMeta(config, loaderAuth, startedAt);
      meta.status = "blocked";
      meta.notes = ["loader auth status failed", summarizeFailure(loaderAuthResult)];
      writeMeta(runDir, meta);
      const summary = blockedSummary(config, "auth", meta.notes);
      writeSummary(runDir, summary);
      printRunResult(root, runDir, summary, console.error);
      process.exitCode = 2;
      return;
    }

    let baseData;
    try {
      baseData = fetchAllBaseRows(config, runDir);
    } catch (err) {
      const meta = makeBaseMeta(config, loaderAuth, startedAt);
      meta.status = "blocked";
      meta.notes = [
        "live dataset fetch failed before dataset.jsonl could be created",
        err.message,
      ];
      writeMeta(runDir, meta);
      const summary = blockedSummary(config, "dataset_access", [
        `Cannot fetch latest evaluation dataset from Base ${config.baseToken} / table ${config.tableId} / view ${config.viewId}: ${err.message}`,
        "Cannot create a local snapshot without Base read permission.",
      ]);
      writeSummary(runDir, summary);
      printRunResult(root, runDir, summary, console.error);
      process.exitCode = 2;
      return;
    }

    const { cases, skippedEmptyQuery } = convertDataset(baseData, config.subset);
    writeDataset(runDir, cases);
    const parseErrorCases = cases
      .filter((item) => item.parse_error)
      .map((item) => item.case_id);
    const meta = makeBaseMeta(config, loaderAuth, startedAt);
    meta.status = "snapshot_ready";
    meta.dataset_size = cases.length;
    meta.cases_skipped_parse_error = parseErrorCases.length;
    meta.skipped_empty_query = skippedEmptyQuery;
    meta.notes = [
      "local dataset snapshot created",
      "remove the executor account's Base permission, then rerun with --dataset-file pointing at this dataset.jsonl",
    ];
    writeMeta(runDir, meta);
    const summary = {
      run_id: config.runId,
      status: "snapshot_ready",
      dataset_size: cases.length,
      scored: 0,
      contaminated_skipped: 0,
      parse_error_cases: parseErrorCases,
      primary_bottleneck: null,
      totals: {
        sum: 0,
        max: cases.length * 15,
        percent: null,
        per_dim: { recall: null, accuracy: null, completeness: null },
      },
      findings: [],
      pollution_warnings: [],
      blockers: [
        "blind setup has not run yet; remove Base permission and rerun with --dataset-file",
      ],
    };
    writeSummary(runDir, summary);
    console.log(
      JSON.stringify(
        {
          run_id: config.runId,
          status: "snapshot_ready",
          run_dir: path.relative(root, runDir),
          dataset_file: path.relative(root, path.join(runDir, "dataset.jsonl")),
          dataset_size: cases.length,
          parse_errors: parseErrorCases.length,
        },
        null,
        2,
      ),
    );
    return;
  }

  const executorAuthResult = runLarkJson(config.executorProfile, ["auth", "status"]);
  let executorAuth = null;
  if (executorAuthResult.ok && executorAuthResult.json) {
    executorAuth = executorAuthResult.json;
  }
  if (!executorAuth || executorAuth.ok === false) {
    const meta = makeBaseMeta(config, executorAuth, startedAt);
    meta.status = "blocked";
    meta.notes = ["executor auth status failed", summarizeFailure(executorAuthResult)];
    writeMeta(runDir, meta);
    const summary = blockedSummary(config, "auth", meta.notes);
    writeSummary(runDir, summary);
    printRunResult(root, runDir, summary, console.error);
    process.exitCode = 2;
    return;
  }

  if (excluded.includes(executorAuth.userOpenId)) {
    const blocker = `executor userOpenId ${executorAuth.userOpenId} is in excluded_user_ids`;
    const meta = makeBaseMeta(config, executorAuth, startedAt);
    meta.status = "blocked";
    meta.notes = [blocker];
    writeMeta(runDir, meta);
    const summary = blockedSummary(config, "account_isolation", [blocker]);
    writeSummary(runDir, summary);
    printRunResult(root, runDir, summary, console.error);
    process.exitCode = 2;
    return;
  }

  let cases;
  let skippedEmptyQuery = 0;
  if (config.datasetFile) {
    const loaded = readDatasetFile(root, config.datasetFile, config.subset);
    cases = loaded.cases;
  } else {
    let baseData;
    try {
      baseData = fetchAllBaseRows(config, runDir);
    } catch (err) {
      const meta = makeBaseMeta(config, executorAuth, startedAt);
      meta.status = "blocked";
      meta.notes = [
        "live dataset fetch failed before dataset.jsonl could be created",
        err.message,
      ];
      writeMeta(runDir, meta);
      const summary = blockedSummary(config, "dataset_access", [
        `Cannot fetch latest evaluation dataset from Base ${config.baseToken} / table ${config.tableId} / view ${config.viewId}: ${err.message}`,
        "Cannot perform a valid eval-search run without dataset.jsonl from the live Base.",
      ]);
      writeSummary(runDir, summary);
      printRunResult(root, runDir, summary, console.error);
      process.exitCode = 2;
      return;
    }
    const converted = convertDataset(baseData, config.subset);
    cases = converted.cases;
    skippedEmptyQuery = converted.skippedEmptyQuery;
  }
  writeDataset(runDir, cases);

  const baseProbe = executorCanReadBase(config);
  if (baseProbe.canRead !== false) {
    const blocker =
      baseProbe.canRead === true
        ? "executor profile can read the evaluation Base; this would contaminate blind search"
        : `executor Base access probe failed in an ambiguous way: ${baseProbe.failure}`;
    const meta = makeBaseMeta(config, executorAuth, startedAt);
    meta.status = "blocked";
    meta.cases_scored = 0;
    meta.cases_skipped_parse_error = cases.filter((item) => item.parse_error).length;
    meta.notes = [blocker];
    writeMeta(runDir, meta);
    const summary = blockedSummary(config, "account_isolation", [blocker], {
      datasetSize: cases.length,
      parseErrorCases: cases.filter((item) => item.parse_error).map((item) => item.case_id),
    });
    writeSummary(runDir, summary);
    printRunResult(root, runDir, summary, console.error);
    process.exitCode = 2;
    return;
  }

  const preflight = runPreflight(config, cases, taintedTokens);
  writeJson(path.join(runDir, "preflight.json"), preflight);

  const parseErrorCases = cases
    .filter((item) => item.parse_error)
    .map((item) => item.case_id);
  const contaminationCount = preflight.filter((item) => item.contamination_risk).length;
  const meta = makeBaseMeta(config, executorAuth, startedAt);
  meta.status = "ready_for_executor";
  meta.cases_scored = 0;
  meta.cases_skipped_parse_error = parseErrorCases.length;
  meta.skipped_empty_query = skippedEmptyQuery;
  meta.notes = [
    "deterministic setup completed: dataset.jsonl and preflight.json are ready",
    "AI executor and judge phases are intentionally not run by this Node setup runner",
  ];
  writeMeta(runDir, meta);

  writeSummary(runDir, {
    run_id: config.runId,
    status: "ready_for_executor",
    dataset_size: cases.length,
    scored: 0,
    contaminated_skipped: 0,
    parse_error_cases: parseErrorCases,
    primary_bottleneck: null,
    totals: {
      sum: 0,
      max: cases.length * 15,
      percent: null,
      per_dim: { recall: null, accuracy: null, completeness: null },
    },
    findings: [],
    pollution_warnings:
      contaminationCount > 0
        ? [`preflight found tainted tokens in ${contaminationCount} case(s)`]
        : [],
    blockers: [
      "executor and judge phases still require the agent workflow described in skills/eval-search/prompts",
    ],
  });

  console.log(
    JSON.stringify(
      {
        run_id: config.runId,
        status: "ready_for_executor",
        run_dir: path.relative(root, runDir),
        dataset_size: cases.length,
        parse_errors: parseErrorCases.length,
        contamination_risks: contaminationCount,
      },
      null,
      2,
    ),
  );
}

try {
  main();
} catch (err) {
  console.error(err.stack || err.message);
  process.exitCode = 1;
}
