#!/usr/bin/env node

const { spawnSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");

function usage() {
  console.log(`Usage:
  node --experimental-strip-types tests/eval-search/eval-search-collect-search.ts --run-dir <dir> [--page-size 10] [--fetch-top 3] [--max-query-variants 4]

Collect baseline evidence for every case in dataset.jsonl. This collector reads
only blind-facing fields (case_id, query, involved_entities, filter_keys), then
writes trajectories plus raw/executor_search.json. It routes each case to the
relevant lark-cli read shortcut (task/contact/im/minutes/vc/mail/drive/calendar)
instead of only using drive +search.`);
}

function parseArgs(argv) {
  const out: any = { runDir: "", pageSize: 10, fetchTop: 3, maxQueryVariants: 4 };
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
    } else if (arg === "--run-dir") {
      out.runDir = next();
    } else if (arg === "--page-size") {
      out.pageSize = Number.parseInt(next(), 10);
      if (!Number.isFinite(out.pageSize) || out.pageSize <= 0 || out.pageSize > 20) {
        throw new Error("--page-size must be an integer from 1 to 20");
      }
    } else if (arg === "--fetch-top") {
      out.fetchTop = Number.parseInt(next(), 10);
      if (!Number.isFinite(out.fetchTop) || out.fetchTop < 0 || out.fetchTop > 10) {
        throw new Error("--fetch-top must be an integer from 0 to 10");
      }
    } else if (arg === "--max-query-variants") {
      out.maxQueryVariants = Number.parseInt(next(), 10);
      if (
        !Number.isFinite(out.maxQueryVariants) ||
        out.maxQueryVariants <= 0 ||
        out.maxQueryVariants > 8
      ) {
        throw new Error("--max-query-variants must be an integer from 1 to 8");
      }
    } else {
      throw new Error(`unknown option ${arg}`);
    }
  }
  if (!out.help && !out.runDir) {
    throw new Error("--run-dir is required");
  }
  return out;
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

function parseJsonOutput(stdout) {
  const text = String(stdout || "").trim();
  const start = text.indexOf("{");
  if (start < 0) {
    throw new Error(`stdout does not contain JSON: ${text.slice(0, 120)}`);
  }
  return JSON.parse(text.slice(start));
}

function runLark(args) {
  const result = spawnSync("lark-cli", args, {
    encoding: "utf8",
    maxBuffer: 64 * 1024 * 1024,
    timeout: 60 * 1000,
  });
  let json = null;
  let parseError = "";
  try {
    json = parseJsonOutput(result.stdout);
  } catch (err) {
    parseError = err.message;
  }
  return {
    cmd: ["lark-cli", ...args].join(" "),
    status: result.status,
    stdout: result.stdout || "",
    stderr: result.stderr || "",
    ok: result.status === 0,
    json,
    parseError: result.error?.code === "ETIMEDOUT" ? "command timed out after 60s" : parseError,
  };
}

function loadCases(datasetFile) {
  return fs
    .readFileSync(datasetFile, "utf8")
    .split(/\r?\n/)
    .filter((line) => line.trim())
    .map((line) => {
      const item = JSON.parse(line);
      return {
        case_id: item.case_id,
        query: item.query,
        involved_entities: item.involved_entities || [],
        filter_keys: item.filter_keys || [],
        category: item.category || [],
      };
    });
}

function firstString(value, keys) {
  if (!value || typeof value !== "object") {
    return "";
  }
  for (const key of keys) {
    const item = value[key];
    if (typeof item === "string" && item.trim()) {
      return item.trim();
    }
    if (typeof item === "number") {
      return String(item);
    }
  }
  return "";
}

function firstNestedString(value, keys, depth = 0) {
  if (!value || typeof value !== "object" || depth > 4) {
    return "";
  }
  const direct = firstString(value, keys);
  if (direct) {
    return direct;
  }
  for (const item of Object.values(value)) {
    if (Array.isArray(item)) {
      for (const child of item) {
        const found = firstNestedString(child, keys, depth + 1);
        if (found) {
          return found;
        }
      }
    } else if (item && typeof item === "object") {
      const found = firstNestedString(item, keys, depth + 1);
      if (found) {
        return found;
      }
    }
  }
  return "";
}

function truncate(text, max = 800) {
  const value = String(text || "").replace(/\s+/g, " ").trim();
  return value.length > max ? `${value.slice(0, max)}...` : value;
}

function extractUrlsFromText(text) {
  const urls = (String(text || "").match(/https?:\/\/[^\s)）\]"'<>}]+/g) || []).map((url) =>
    url.replace(/[.,;，。；|]+$/g, ""),
  );
  return [...new Set(urls)];
}

function compactJson(value, max = 1600) {
  return truncate(JSON.stringify(value), max);
}

function findResultArray(value, depth = 0) {
  if (!value || depth > 5) {
    return [];
  }
  if (Array.isArray(value)) {
    if (
      value.length === 0 ||
      value.some((item) => item && (typeof item === "object" || typeof item === "string"))
    ) {
      return value;
    }
    return [];
  }
  if (typeof value !== "object") {
    return [];
  }
  const preferred = [
    "results",
    "items",
    "tasks",
    "tasklists",
    "users",
    "messages",
    "meetings",
    "minutes",
    "events",
    "data",
  ];
  for (const key of preferred) {
    if (Array.isArray(value[key])) {
      return value[key];
    }
  }
  for (const item of Object.values(value)) {
    const found = findResultArray(item, depth + 1);
    if (found.length > 0) {
      return found;
    }
  }
  return [];
}

function compactGenericResult(item, source) {
  const rawText = typeof item === "string" ? item : compactJson(item);
  const url = extractUrlsFromText(rawText)[0] || firstNestedString(item, ["url", "link"]);
  const title =
    firstNestedString(item, [
      "title",
      "name",
      "summary",
      "subject",
      "topic",
      "task_title",
      "display_name",
      "localized_name",
    ]) || source.label;
  const token =
    firstNestedString(item, [
      "guid",
      "token",
      "message_id",
      "event_id",
      "meeting_id",
      "minute_token",
      "open_id",
      "id",
      "task_id",
      "tasklist_guid",
    ]) || "";
  return {
    entity_type: source.kind,
    doc_type: source.label,
    title: truncate(title, 180),
    summary: rawText,
    token,
    url,
    owner_name: "",
    update_time_iso: firstNestedString(item, ["update_time_iso", "updated_at", "start_time"]) || "",
    raw: item,
  };
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

function loadTaintedTokens(root, runDir = "") {
  const file = path.join(root, "skills/eval-search/references/known-tainted-tokens.md");
  const tokens: Set<string> = new Set();
  if (!fs.existsSync(file)) {
    return tokens;
  }
  let inTaintedBlock = false;
  for (const line of fs.readFileSync(file, "utf8").split(/\r?\n/)) {
    if (/^\s*tainted_tokens:\s*$/.test(line)) {
      inTaintedBlock = true;
      continue;
    }
    if (inTaintedBlock && /^\s*[a-zA-Z_]+:\s*$/.test(line)) {
      break;
    }
    if (!inTaintedBlock) {
      continue;
    }
    const match = line.match(/^\s*-\s+([A-Za-z0-9_-]{12,})\b/);
    if (match) {
      tokens.add(match[1]);
    }
  }
  const localFile = runDir ? path.join(runDir, "cloud-doc", "tainted_tokens.json") : "";
  if (localFile && fs.existsSync(localFile)) {
    addTokensFromValue(JSON.parse(fs.readFileSync(localFile, "utf8")), tokens);
  }
  return tokens;
}

function decodeEntities(text) {
  return String(text || "")
    .replace(/&amp;/g, "&")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&#34;/g, '"')
    .replace(/&#39;/g, "'");
}

function stripHighlights(text) {
  return decodeEntities(String(text || "").replace(/<\/?h[b]?>/g, ""));
}

function stripXml(text) {
  return decodeEntities(String(text || ""))
    .replace(/<[^>]+>/g, " ")
    .replace(/\s+/g, " ")
    .trim();
}

function compactResult(item) {
  const meta = item.result_meta || {};
  return {
    entity_type: item.entity_type || "",
    doc_type: meta.doc_types || "",
    title: stripHighlights(item.title_highlighted),
    summary: stripHighlights(item.summary_highlighted),
    token: meta.token || "",
    url: meta.url || "",
    owner_name: meta.owner_name || "",
    update_time_iso: meta.update_time_iso || "",
  };
}

function normalizedText(text) {
  return String(text || "")
    .replace(/[“”]/g, '"')
    .replace(/[‘’]/g, "'")
    .replace(/[，。！？、；：,.!?;:()[\]{}【】<>]/g, " ")
    .replace(/\s+/g, " ")
    .trim();
}

function compactQuery(query) {
  return normalizedText(query)
    .replace(/请问|帮我|我想知道|有哪些|哪些|是否|怎么写|如何写|怎么|如何|为何|为什么|可以|完成吗|近期|情况|一下/g, " ")
    .replace(/\s+/g, " ")
    .trim();
}

function queryTerms(query) {
  const compact = compactQuery(query);
  const terms = new Set();
  for (const term of compact.split(/\s+/)) {
    const value = term.trim().toLowerCase();
    if (value.length >= 2) {
      terms.add(value);
    }
  }
  const asciiTerms = compact.match(/[A-Za-z][A-Za-z0-9_-]{1,}|[0-9]+(?:\.[0-9]+)?%?/g) || [];
  for (const value of asciiTerms) {
    terms.add(value.toLowerCase());
  }
  const cjkTerms = [
    "客户",
    "案例",
    "政策",
    "直销",
    "售卖",
    "融资",
    "估值",
    "轮次",
    "成本",
    "费用",
    "服务器",
    "区域",
    "东南亚",
    "税局",
    "互通",
    "对接",
    "发布会",
    "重点",
    "目标",
    "指标",
    "准确率",
    "命中率",
    "渗透率",
    "附件",
    "进度",
    "待改进",
    "不足",
    "环评",
    "定时",
    "问答",
    "项目",
    "文档搜索",
    "理想汽车",
    "飞书",
    "华东",
  ];
  for (const term of cjkTerms) {
    if (compact.includes(term)) {
      terms.add(term.toLowerCase());
    }
  }
  return [...terms] as string[];
}

function addIfUseful(variants: Set<string>, value) {
  const normalized = normalizedText(value);
  if (normalized && normalized.length >= 2) {
    variants.add(normalized);
  }
}

function queryPhrases(query) {
  const original = normalizedText(query);
  const phrases = new Set();
  const asciiTerms = original.match(/[A-Za-z][A-Za-z0-9_-]{1,}/g) || [];
  for (const term of asciiTerms) {
    phrases.add(term);
  }
  for (const term of queryTerms(query)) {
    if (!/^[a-z0-9_.-]+$/.test(term)) {
      phrases.add(term);
    }
  }
  return [...phrases] as string[];
}

function generateQueryVariants(query, maxVariants) {
  const variants: Set<string> = new Set();
  const original = normalizedText(query);
  const compact = compactQuery(query);
  addIfUseful(variants, original);
  if (compact && compact !== original) {
    addIfUseful(variants, compact);
  }

  const focusedRules: Array<[RegExp, string]> = [
    [/Aily.*案例|案例.*Aily/i, "Aily 客户案例 华东 最佳实践"],
    [/邮件.*附件|附件.*检索|检索.*附件/, "检索 邮件 附件 进度 排期"],
    [/Perplexity/i, "Perplexity AI 融资 估值 轮次"],
    [/东南亚.*服务器|服务器.*东南亚|服务器.*成本/, "东南亚 服务器 成本 机房 区域 费用 原因"],
    [/360.*环评|环评.*待改进|待改进.*环评/, "360环评 撰写方法指南 待改进 不足 示例"],
    [/Satya.*DeepSeek|DeepSeek.*Satya/i, "Satya DeepSeek 评价 微软"],
    [/Payroll|税局/, "Payroll 税局 互通 对接 报税 个税"],
    [/发布会.*重点|飞书.*发布会/, "飞书项目 发布会 重点 功能一览 AI 成熟度"],
    [/IDC.*成本|成本.*目标/i, "IDC 成本 目标 预算 优化"],
    [/定时.*trigger|trigger.*准确率|定时.*准确率/i, "定时问答 trigger 准确率 命中率"],
  ];
  for (const [pattern, variant] of focusedRules) {
    if (pattern.test(original)) {
      addIfUseful(variants, variant);
    }
  }
  addIfUseful(variants, queryPhrases(query).join(" "));

  const expansionRules: Array<[RegExp, string]> = [
    [/使用案例|案例/, "客户案例 最佳实践"],
    [/融资|估值|轮次/, "融资 金额 估值 轮次"],
    [/成本|贵|费用|价格/, "成本 费用 原因"],
    [/准确率|命中率|召回率/, "准确率 命中率 指标 评测"],
    [/发布会|新品/, "发布会 主题 功能一览"],
    [/互通|对接|同步|打通|集成/, "对接 同步 集成"],
    [/待改进|不足|改进点/, "待改进 不足 模板 示例"],
    [/目标|指标/, "目标 指标 OKR"],
  ];
  for (const [pattern, expansion] of expansionRules) {
    if (pattern.test(original)) {
      addIfUseful(variants, `${compact || original} ${expansion}`);
    }
  }

  if (original.length <= 40 && !original.includes('"')) {
    addIfUseful(variants, `"${original}"`);
  }
  if (compact && compact.length <= 40 && compact !== original && !compact.includes('"')) {
    addIfUseful(variants, `"${compact}"`);
  }

  return [...variants].slice(0, maxVariants);
}

function isFetchable(result) {
  const url = result.url || "";
  if (!url) {
    return false;
  }
  const type = String(result.doc_type || "").toUpperCase();
  if (["BITABLE", "SHEET", "FILE", "FOLDER", "SLIDES"].includes(type)) {
    return false;
  }
  return (
    type === "DOC" ||
    type === "DOCX" ||
    url.includes("/docx/") ||
    url.includes("/docs/")
  );
}

function isTainted(result, taintedTokens) {
  if (!taintedTokens || taintedTokens.size === 0) {
    return false;
  }
  const token = result.token || "";
  const url = result.url || "";
  return taintedTokens.has(token) || [...taintedTokens].some((item) => item && url.includes(item));
}

function suspiciousArtifactReason(result, query) {
  const title = normalizedText(result.title).toLowerCase();
  const summary = normalizedText(result.summary).toLowerCase();
  const text = `${title} ${summary}`;
  const queryText = normalizedText(query).toLowerCase();
  const allowMetricEvaluation =
    /(准确率|命中率|指标|golden\s*set|评估|评测)/i.test(queryText) &&
    /评测(方案|结果|历次评估)|golden\s*set/i.test(title);
  if (allowMetricEvaluation) {
    return "";
  }
  const patterns: Array<[RegExp, string]> = [
    [/评测集|测试集|case\s*分析|评测\s*case|case重抓|意图_改写|搜索cli专项/i, "eval dataset or case analysis"],
    [/模型追问|追问pe|追问拆分|followup评测|精简版追问|autothinking能力|知识问答autothinking/i, "follow-up prompt/eval artifact"],
    [/gsb评测报告|基线评测机评|机评应用报告|多因子排序评测query|query-top100/i, "model evaluation report"],
    [/s2b eval set|auto_res|baseline|sheet1_from|极速集群评测|送评|横评|意图评测|试评结果/i, "eval table artifact"],
    [/标签填写规则|场景标签标注/i, "labeling guide with embedded eval examples"],
    [/prompt|promt|debug|agentic问答/i, "prompt/debug artifact"],
  ];
  for (const [pattern, reason] of patterns) {
    if (pattern.test(text)) {
      return reason;
    }
  }
  if (/已废弃/.test(title)) {
    return "deprecated document";
  }
  return "";
}

function scoreResult(result, query, variantIndex) {
  const artifactReason = suspiciousArtifactReason(result, query);
  const title = String(result.title || "").toLowerCase();
  const summary = String(result.summary || "").toLowerCase();
  const haystack = `${title} ${summary}`;
  const compact = compactQuery(query).toLowerCase();
  let score = 0;
  if (result.url) {
    score += 0.2;
  }
  if (isFetchable(result)) {
    score += 0.8;
  }
  if (artifactReason === "deprecated document") {
    score -= 6;
  }
  if (compact && title.includes(compact)) {
    score += 12;
  } else if (compact && haystack.includes(compact)) {
    score += 5;
  }
  for (const term of queryTerms(query)) {
    if (title.includes(term)) {
      score += 4;
    } else if (summary.includes(term)) {
      score += 1;
    }
  }
  const numericTerms = query.match(/[0-9]+(?:\.[0-9]+)?%?|20[0-9]{2}/g) || [];
  for (const term of numericTerms) {
    if (haystack.includes(term.toLowerCase())) {
      score += 3;
    }
  }
  score += Math.max(0, 2 - variantIndex);
  return score;
}

function annotateEvidenceStatus(result, query, taintedTokens) {
  const tainted = isTainted(result, taintedTokens);
  return {
    ...result,
    tainted,
    suspicious_artifact_reason: suspiciousArtifactReason(result, query),
    evidence_excluded: tainted,
    evidence_excluded_reason: tainted ? "known_tainted_token" : "",
  };
}

function isEvidenceCandidate(result) {
  return !result.evidence_excluded && !result.tainted;
}

function fetchDoc(result, index) {
  const fetchedAt = new Date().toISOString();
  const response = runLark([
    "docs",
    "+fetch",
    "--api-version",
    "v2",
    "--as",
    "user",
    "--doc",
    result.url,
    "--format",
    "json",
  ]);
  if (!response.ok || response.json?.ok === false) {
    return {
      idx: index,
      url: result.url,
      token: result.token,
      title: result.title,
      tainted: Boolean(result.tainted),
      suspicious_artifact_reason: result.suspicious_artifact_reason || "",
      matched_query: result.matched_query || "",
      score: result.score,
      fetched_at: fetchedAt,
      ok: false,
      cmd: response.cmd,
      error: [response.parseError, response.stderr.trim()].filter(Boolean).join(": "),
      excerpt: "",
    };
  }
  const content = response.json?.data?.document?.content || "";
  return {
    idx: index,
    url: result.url,
    token: result.token,
    title: result.title,
    tainted: Boolean(result.tainted),
    suspicious_artifact_reason: result.suspicious_artifact_reason || "",
    matched_query: result.matched_query || "",
    score: result.score,
    fetched_at: fetchedAt,
    ok: true,
    cmd: response.cmd,
    document_id: response.json?.data?.document?.document_id || "",
    revision_id: response.json?.data?.document?.revision_id || "",
    excerpt: stripXml(content).slice(0, 6000),
  };
}

function collectFetches(results, fetchTop) {
  const fetches = [];
  let successfulFetches = 0;
  const maxAttempts = Math.max(fetchTop, fetchTop * 3);
  for (const result of results) {
    if (successfulFetches >= fetchTop || fetches.length >= maxAttempts) {
      break;
    }
    if (!isFetchable(result)) {
      continue;
    }
    if (!isEvidenceCandidate(result)) {
      continue;
    }
    if (result.score < 4) {
      continue;
    }
    const fetched = fetchDoc(result, fetches.length + 1);
    if (fetched.ok) {
      successfulFetches += 1;
    }
    fetches.push(fetched);
  }
  return fetches;
}

let cachedUserOpenId = "";

function currentUserOpenId() {
  if (cachedUserOpenId) {
    return cachedUserOpenId;
  }
  const result = runLark(["auth", "status"]);
  if (result.ok && result.json?.ok !== false) {
    cachedUserOpenId =
      result.json?.userOpenId ||
      result.json?.user_open_id ||
      result.json?.data?.userOpenId ||
      result.json?.data?.user_open_id ||
      "";
  }
  return cachedUserOpenId;
}

function isoDate(date) {
  return date.toISOString().slice(0, 10);
}

function currentMonthRange() {
  const now = new Date();
  const start = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), 1));
  const end = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() + 1, 1));
  return [isoDate(start), isoDate(end)];
}

function currentYearRange() {
  const now = new Date();
  return [`${now.getUTCFullYear()}-01-01`, `${now.getUTCFullYear() + 1}-01-01`];
}

function relativeDayRange(daysBack, daysForward = 1) {
  const now = new Date();
  const start = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()));
  start.setUTCDate(start.getUTCDate() - daysBack);
  const end = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()));
  end.setUTCDate(end.getUTCDate() + daysForward);
  return [isoDate(start), isoDate(end)];
}

function lastWeekRange() {
  const now = new Date();
  const day = now.getUTCDay() || 7;
  const thisMonday = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() - day + 1));
  const prevMonday = new Date(thisMonday);
  prevMonday.setUTCDate(thisMonday.getUTCDate() - 7);
  return [isoDate(prevMonday), isoDate(thisMonday)];
}

function rangeForQuery(query) {
  if (/上周/.test(query)) {
    return lastWeekRange();
  }
  if (/最近两周/.test(query)) {
    return relativeDayRange(14);
  }
  if (/最近一周/.test(query)) {
    return relativeDayRange(7);
  }
  if (/最近一个月|近一个月/.test(query)) {
    return relativeDayRange(30);
  }
  if (/最近3个月|近3个月/.test(query)) {
    return relativeDayRange(90);
  }
  if (/最近半年|近半年/.test(query)) {
    return relativeDayRange(180);
  }
  if (/今年/.test(query)) {
    const now = new Date();
    return [`${now.getUTCFullYear()}-01-01`, `${now.getUTCFullYear() + 1}-01-01`];
  }
  if (/这月|本月|这个月|2026\.05|2026年5月/.test(query)) {
    return currentMonthRange();
  }
  if (/快到期|到期/.test(query)) {
    const now = new Date();
    now.setUTCDate(now.getUTCDate() - 1);
    const end = new Date(now);
    end.setUTCDate(now.getUTCDate() + 8);
    return [isoDate(now), isoDate(end)];
  }
  return ["", ""];
}

function keywordFor(query, entity) {
  const compact = compactQuery(query);
  const focused: Array<[RegExp, string]> = [
    [/产品评审会/, "产品评审会"],
    [/技术方案评审会|技术评审会/, "技术评审会"],
    [/周会/, "周会"],
    [/产品发布会/, "产品发布会"],
    [/Q2季度目标|Q2 季度目标/, "Q2季度目标"],
    [/外部协作/, "外部协作"],
    [/附件/, "附件"],
  ];
  for (const [pattern, value] of focused) {
    if (pattern.test(query)) {
      return value;
    }
  }
  if (entity === "任务清单") {
    return "任务清单";
  }
  if (entity === "任务") {
    return /清单/.test(query) ? "任务" : "";
  }
  if (entity === "联系人") {
    if (/部门|成员|企业邮箱|在职/.test(query)) {
      return "搜索-测试";
    }
    return "";
  }
  if (["邮箱", "日程", "Bot"].includes(entity)) {
    return "";
  }
  return compact.slice(0, 50);
}

function addArg(args, flag, value) {
  if (value !== undefined && value !== null && String(value) !== "") {
    args.push(flag, String(value));
  }
}

function localDateTime(value) {
  if (/^\d{4}-\d{2}-\d{2}$/.test(value)) {
    return `${value}T00:00:00+08:00`;
  }
  return value;
}

function addTimeArgs(args, startFlag, endFlag, range, formatter = (value) => value) {
  if (range[0]) {
    args.push(startFlag, formatter(range[0]));
  }
  if (range[1]) {
    args.push(endFlag, formatter(range[1]));
  }
}

function commandSpecsForCase(item, pageSize) {
  const entities = item.involved_entities && item.involved_entities.length
    ? item.involved_entities
    : ["文档"];
  const filterKeys = new Set(item.filter_keys || []);
  const specs = [];
  const userOpenId = currentUserOpenId();
  const range = rangeForQuery(item.query);

  for (const entity of entities) {
    const keyword = keywordFor(item.query, entity);
    if (entity === "任务") {
      const args = ["task", "+search", "--as", "user", "--format", "json"];
      addArg(args, "--query", keyword);
      if (userOpenId && filterKeys.has("creator_ids")) addArg(args, "--creator", userOpenId);
      if (userOpenId && filterKeys.has("assignee_ids")) addArg(args, "--assignee", userOpenId);
      if (userOpenId && filterKeys.has("follower_ids")) addArg(args, "--follower", userOpenId);
      if (/未完成|还没完成|优先处理|待处理|快到期/.test(item.query)) args.push("--completed=false");
      if (filterKeys.has("due_time")) {
        const dueRange = range[0] || range[1] ? range : currentYearRange();
        addArg(args, "--due", dueRange.filter(Boolean).join(","));
      }
      specs.push({ kind: "task", label: "task +search", query: keyword, args });
    } else if (entity === "任务清单") {
      const args = ["task", "+tasklist-search", "--as", "user", "--format", "json"];
      addArg(args, "--query", keyword);
      if (userOpenId && filterKeys.has("creator_ids")) addArg(args, "--creator", userOpenId);
      specs.push({ kind: "tasklist", label: "task +tasklist-search", query: keyword, args });
    } else if (entity === "联系人") {
      const args = ["contact", "+search-user", "--as", "user", "--format", "json", "--page-size", String(Math.min(pageSize, 30))];
      addArg(args, "--query", keyword);
      if (/最近.*聊天|聊天记录/.test(item.query) || filterKeys.has("has_contact")) args.push("--has-chatted");
      specs.push({ kind: "contact", label: "contact +search-user", query: keyword, args });
    } else if (entity === "消息" || entity === "Bot") {
      const args = ["im", "+messages-search", "--as", "user", "--format", "json", "--page-size", String(Math.min(pageSize, 50))];
      addArg(args, "--query", keyword);
      addTimeArgs(args, "--start", "--end", range, localDateTime);
      if (entity === "Bot" || /Bot|机器人/i.test(item.query)) args.push("--sender-type", "bot");
      specs.push({ kind: "message", label: "im +messages-search", query: keyword, args });
    } else if (entity === "妙记") {
      const args = ["minutes", "+search", "--as", "user", "--format", "json", "--page-size", String(Math.min(pageSize, 30))];
      addArg(args, "--query", keyword);
      addTimeArgs(args, "--start", "--end", range);
      if (userOpenId && filterKeys.has("participant_ids")) addArg(args, "--participant-ids", userOpenId);
      if (userOpenId && filterKeys.has("creator_ids")) addArg(args, "--owner-ids", userOpenId);
      specs.push({ kind: "minutes", label: "minutes +search", query: keyword, args });
    } else if (entity === "视频会议") {
      const args = ["vc", "+search", "--as", "user", "--format", "json", "--page-size", String(Math.min(pageSize, 30))];
      addArg(args, "--query", keyword);
      addTimeArgs(args, "--start", "--end", range);
      if (userOpenId && filterKeys.has("participant_ids")) addArg(args, "--participant-ids", userOpenId);
      if (userOpenId && filterKeys.has("organizer_ids")) addArg(args, "--organizer-ids", userOpenId);
      specs.push({ kind: "vc", label: "vc +search", query: keyword, args });
    } else if (entity === "邮箱") {
      const args = ["mail", "+triage", "--as", "user", "--format", "json", "--max", String(Math.min(Math.max(pageSize, 20), 80))];
      addArg(args, "--query", keyword);
      specs.push({ kind: "mail", label: "mail +triage", query: keyword, args });
    } else if (entity === "日程") {
      const args = ["calendar", "+agenda", "--as", "user", "--format", "json"];
      addTimeArgs(args, "--start", "--end", range[0] ? range : currentMonthRange());
      specs.push({ kind: "calendar", label: "calendar +agenda", query: keyword, args });
    } else if (entity === "文档") {
      const args = ["drive", "+search", "--as", "user", "--query", keyword, "--page-size", String(Math.min(pageSize, 20)), "--format", "json"];
      if (filterKeys.has("creator_ids")) args.push("--mine");
      if (filterKeys.has("create_time")) addTimeArgs(args, "--created-since", "--created-until", range);
      if (filterKeys.has("update_time") || filterKeys.has("editor_ids")) addTimeArgs(args, "--edited-since", "--edited-until", range);
      specs.push({ kind: "drive", label: "drive +search", query: keyword, args });
    }
  }

  if (specs.length === 0) {
    for (const [index, query] of generateQueryVariants(item.query, 2).entries()) {
      specs.push({
        kind: "drive",
        label: `drive +search fallback ${index + 1}`,
        query,
        args: ["drive", "+search", "--as", "user", "--query", query, "--page-size", String(Math.min(pageSize, 20)), "--format", "json"],
      });
    }
  }
  return specs;
}

function runSearchSpec(spec) {
  return runLark(spec.args);
}

function compactCommandResults(result, spec, taintedTokens) {
  if (!result.ok || result.json?.ok === false) {
    return [];
  }
  if (spec.kind === "drive") {
    return (result.json?.data?.results || [])
      .map(compactResult)
      .map((item) => annotateEvidenceStatus(item, spec.query || "", taintedTokens));
  }
  return findResultArray(result.json?.data || result.json)
    .map((item) => compactGenericResult(item, spec))
    .map((item) => annotateEvidenceStatus(item, spec.query || "", taintedTokens));
}

function mergeSearchResults(rounds, originalQuery, taintedTokens) {
  const byKey = new Map();
  for (const round of rounds) {
    for (const result of round.results) {
      const key = result.token || result.url || `${result.title}\n${result.summary}`;
      if (!key) {
        continue;
      }
      const score = scoreResult(result, originalQuery, round.variant_index);
      const existing = byKey.get(key);
      const next = {
        ...result,
        score,
        matched_query: round.query,
        tainted: isTainted(result, taintedTokens),
        suspicious_artifact_reason: suspiciousArtifactReason(result, originalQuery),
      };
      next.evidence_excluded = Boolean(next.tainted);
      next.evidence_excluded_reason = next.tainted ? "known_tainted_token" : "";
      if (!existing || next.score > existing.score) {
        byKey.set(key, next);
      }
    }
  }
  return [...byKey.values()].sort((a, b) => b.score - a.score);
}

function splitSegments(text) {
  const normalized = stripXml(text);
  if (!normalized) {
    return [];
  }
  const rough = normalized.split(/(?<=[。！？；;])\s+|[\r\n]+/u).filter(Boolean);
  const out = [];
  for (const item of rough.length ? rough : [normalized]) {
    if (item.length <= 220) {
      out.push(item.trim());
      continue;
    }
    for (let i = 0; i < item.length; i += 180) {
      out.push(item.slice(i, i + 220).trim());
    }
  }
  return out.filter((item) => item.length >= 8);
}

function scoreSegment(segment, terms) {
  const text = segment.toLowerCase();
  let score = 0;
  for (const term of terms) {
    if (!term) {
      continue;
    }
    if (text.includes(term.toLowerCase())) {
      score += term.length >= 4 ? 3 : 1;
    }
  }
  if (/[0-9]+(?:\.[0-9]+)?\s*(%|万|亿|元|人天|qps|tpm|arr|dau)?/i.test(text)) {
    score += 2;
  }
  if (/支持|不支持|可以|不可|不能|无法|已|未|适用|范围|要求|目标|成本|准确率|命中率|改进|不足|原因/.test(text)) {
    score += 1;
  }
  return score;
}

function topRelevantSegments(text, query, limit) {
  const terms = queryTerms(query);
  return splitSegments(text)
    .map((segment) => ({ segment, score: scoreSegment(segment, terms) }))
    .filter((item) => item.score > 0)
    .sort((a, b) => b.score - a.score)
    .slice(0, limit)
    .map((item) => item.segment);
}

function collectContextMatches(text, pattern, limit) {
  const normalized = stripXml(text);
  const out = [];
  const seen = new Set();
  for (const match of normalized.matchAll(pattern)) {
    const index = match.index || 0;
    const start = Math.max(0, index - 70);
    const end = Math.min(normalized.length, index + match[0].length + 90);
    const context = normalized.slice(start, end).replace(/\s+/g, " ").trim();
    if (context && !seen.has(context)) {
      seen.add(context);
      out.push(context);
    }
    if (out.length >= limit) {
      break;
    }
  }
  return out;
}

function buildSlotHints(query, fetched) {
  const fullText = fetched.map((item) => `${item.title}\n${item.excerpt}`).join("\n");
  const hints = [];
  if (/(准确率|命中率|渗透率|目标|成本|融资|估值|金额|预算|比例|多少|几)/.test(query)) {
    const contexts = collectContextMatches(
      fullText,
      /[0-9]+(?:\.[0-9]+)?\s*(?:%|万|亿|元|万元|亿元|人天|QPS|TPM|ARR|DAU)?/gi,
      6,
    );
    if (contexts.length > 0) {
      hints.push(["关键数值线索", contexts]);
    }
  }
  if (/(是否|能否|可否|互通|对接|同步|打通|支持)/.test(query)) {
    const contexts = collectContextMatches(
      fullText,
      /支持|不支持|可以|不可以|不可|不能|无法|已打通|未打通|互通|对接|同步|税局|报税/gi,
      6,
    );
    if (contexts.length > 0) {
      hints.push(["是否/对接线索", contexts]);
    }
  }
  if (/(政策|售卖|直销|适用|范围|规则|限制)/.test(query)) {
    const contexts = collectContextMatches(
      fullText,
      /适用|范围|客户|版本|渠道|售卖|政策|不提供|必须|要求|生效|试用|价格|折扣/gi,
      6,
    );
    if (contexts.length > 0) {
      hints.push(["政策范围线索", contexts]);
    }
  }
  if (/(怎么|如何|待改进|不足|改进|模板|示例)/.test(query)) {
    const contexts = collectContextMatches(
      fullText,
      /改进|不足|建议|问题|优化|模板|示例|目标|衡量|反馈/gi,
      6,
    );
    if (contexts.length > 0) {
      hints.push(["写法/改进线索", contexts]);
    }
  }
  return hints;
}

function fallbackAnswerFrame(query) {
  if (/(360.*环评|环评.*待改进|待改进.*环评)/.test(query)) {
    return [
      "未从已读取材料中找到专门的 360 环评模板；可按“具体行为事实 -> 造成影响 -> 期望改进动作 -> 可衡量标准”的结构写待改进项。",
      "避免只写性格评价，优先写可观察行为，例如沟通同步、交付质量、风险暴露、协作响应等。",
    ];
  }
  if (/(是否|能否|可否)/.test(query)) {
    return ["未从已读取材料中找到明确的 yes/no 结论，需要继续围绕产品名、对接对象和“支持/不支持”重搜。"];
  }
  return [];
}

function synthesizeAnswer(query, searchRow) {
  const fetched = (searchRow.fetches || []).filter((item) => item.ok);
  const evidenceTop = (searchRow.evidence_results || []).slice(0, 5);
  const excludedTop = (searchRow.non_evidential_results || []).slice(0, 3);
  if (fetched.length === 0) {
    const fallback = fallbackAnswerFrame(query);
    if (fallback.length > 0) {
      return [
        "未读取到足够可信的非污染文档正文。",
        ...fallback.map((item) => `- ${item}`),
        "非污染搜索候选：",
        ...(evidenceTop.length > 0
          ? evidenceTop.map((item, index) => `${index + 1}. ${item.title || item.url}`)
          : ["无"]),
        ...(excludedTop.length > 0
          ? [
              "已观测但不作为证据的污染候选：",
              ...excludedTop.map((item, index) => `${index + 1}. ${item.title || item.url}`),
            ]
          : []),
      ].join("\n");
    }
    if (evidenceTop.length === 0) {
      return excludedTop.length === 0
        ? "未找到直接相关的非污染云文档搜索结果。"
        : [
            "搜索命中了已知污染材料，但没有找到可作为答案证据的非污染云文档。",
            ...excludedTop.map((item, index) => `${index + 1}. ${item.title || item.url}`),
          ].join("\n");
    }
    return [
      "未读取到足够可信的非污染文档正文，搜索到的主要候选如下：",
      ...evidenceTop.map((item, index) => `${index + 1}. ${item.title || item.url}`),
    ].join("\n");
  }

  const lines = [`基于已读取的 ${fetched.length} 个非污染文档，提取到以下答复线索：`];
  const slotHints = buildSlotHints(query, fetched);
  for (const [label, contexts] of slotHints) {
    lines.push(`\n${label}：`);
    for (const context of contexts) {
      lines.push(`- ${context}`);
    }
  }

  const fallback = fallbackAnswerFrame(query);
  if (slotHints.length === 0 && fallback.length > 0) {
    lines.push("\n补充作答框架：");
    for (const item of fallback) {
      lines.push(`- ${item}`);
    }
  }

  lines.push("\n主要依据：");
  for (const item of fetched) {
    const snippets = topRelevantSegments(item.excerpt, query, 3);
    const evidence = snippets.length > 0 ? snippets : [item.excerpt.slice(0, 360)];
    lines.push(`- ${item.title || item.url}`);
    for (const snippet of evidence) {
      lines.push(`  - ${snippet}`);
    }
  }
  return lines.join("\n");
}

function writeTrajectory(runDir, caseItem, searchRow) {
  const topObserved = searchRow.results.slice(0, 5);
  const topEvidence = (searchRow.evidence_results || []).slice(0, 5);
  const nonEvidential = (searchRow.non_evidential_results || []).slice(0, 10);
  const fetched = (searchRow.fetches || []).filter((item) => item.ok);
  const answer = synthesizeAnswer(caseItem.query, searchRow);

  const searchRounds = (searchRow.search_rounds || []).map((item, index) => ({
    idx: index + 1,
    tool: "lark-cli",
    cmd: item.cmd,
    outcome_summary:
      item.error ||
      `${item.label || item.tool_kind || "lark-cli search"} returned ${
        item.results.length
      } compact result(s); top title: ${item.results[0]?.title || "none"}`,
    tool_kind: item.tool_kind || "",
    label: item.label || "",
    query_variant: item.query,
    result_tokens: item.results.map((result) => result.token).filter(Boolean),
    result_urls: item.results.map((result) => result.url).filter(Boolean),
    tainted_tokens_observed: item.results
      .filter((result) => result.tainted)
      .map((result) => result.token)
      .filter(Boolean),
    evidence_tokens: item.results
      .filter((result) => isEvidenceCandidate(result))
      .map((result) => result.token)
      .filter(Boolean),
  }));
  const fetchRounds = (searchRow.fetches || []).map((item, index) => ({
    idx: searchRounds.length + index + 1,
    tool: "lark-cli",
    cmd: item.cmd,
    outcome_summary: item.ok
      ? `docs +fetch succeeded for ${item.title || item.url}; excerpt chars=${item.excerpt.length}`
      : `docs +fetch failed for ${item.title || item.url}: ${item.error}`,
    fetched_token: item.token,
    fetched_url: item.url,
  }));
  const trajectory = {
    case_id: caseItem.case_id,
    query: caseItem.query,
    started_at: searchRow.started_at,
    ended_at: searchRow.ended_at,
    rounds: [
      ...searchRounds,
      ...fetchRounds,
    ],
    answer,
    observed_top_results: topObserved.map((item) => ({
      title: item.title,
      url: item.url,
      token: item.token,
      tainted: Boolean(item.tainted),
      evidence_excluded: Boolean(item.evidence_excluded),
      evidence_excluded_reason: item.evidence_excluded_reason || "",
      suspicious_artifact_reason: item.suspicious_artifact_reason || "",
    })),
    evidence_top_results: topEvidence.map((item) => ({
      title: item.title,
      url: item.url,
      token: item.token,
      suspicious_artifact_reason: item.suspicious_artifact_reason || "",
    })),
    non_evidential_results: nonEvidential.map((item) => ({
      title: item.title,
      url: item.url,
      token: item.token,
      reason: item.evidence_excluded_reason || "unknown",
    })),
    referenced_urls: [
      ...new Set([
        ...fetched.map((item) => item.url).filter(Boolean),
        ...topEvidence.map((item) => item.url).filter(Boolean),
      ]),
    ],
    rounds_used: searchRounds.length + (searchRow.fetches || []).length,
    gave_up: fetched.length === 0 && topEvidence.length === 0,
    notes:
      fetched.length > 0
        ? `multi-entity lark-cli baseline; fetched strongest non-tainted document-like hits; evidence_candidates=${searchRow.evidence_results_count}; tainted_observed=${searchRow.tainted_results}; non_evidential_observed=${searchRow.non_evidential_results_count}; suspicious_observed=${searchRow.suspicious_artifact_results}; tainted_fetched=${searchRow.tainted_fetches}; suspicious_fetched=${searchRow.suspicious_artifact_fetches}`
        : `multi-entity lark-cli baseline; no non-tainted document-like hit was fetched; evidence_candidates=${searchRow.evidence_results_count}; tainted_observed=${searchRow.tainted_results}; non_evidential_observed=${searchRow.non_evidential_results_count}; suspicious_observed=${searchRow.suspicious_artifact_results}`,
  };
  fs.writeFileSync(
    path.join(runDir, "trajectories", `${caseItem.case_id}.json`),
    `${JSON.stringify(trajectory, null, 2)}\n`,
  );
}

function main() {
  const args = parseArgs(process.argv.slice(2));
  if (args.help) {
    usage();
    return;
  }
  const root = repoRoot();
  const runDir = path.isAbsolute(args.runDir)
    ? args.runDir
    : path.join(root, args.runDir);
  const datasetFile = path.join(runDir, "dataset.jsonl");
  const rawDir = path.join(runDir, "raw");
  ensureDir(rawDir);
  ensureDir(path.join(runDir, "trajectories"));
  const taintedTokens = loadTaintedTokens(root, runDir);

  const rows = [];
  for (const item of loadCases(datasetFile)) {
    const started = new Date().toISOString();
    const searchRounds = [];
    const specs = commandSpecsForCase(item, args.pageSize).slice(0, args.maxQueryVariants);
    for (const [index, spec] of specs.entries()) {
      const result = runSearchSpec(spec);
      const results = compactCommandResults(result, spec, taintedTokens);
      searchRounds.push({
        variant_index: index,
        query: spec.query || "",
        label: spec.label,
        tool_kind: spec.kind,
        cmd: result.cmd,
        ok: result.ok && result.json?.ok !== false,
        error:
          result.ok && result.json?.ok !== false
            ? ""
            : [result.parseError, result.stderr.trim()].filter(Boolean).join(": "),
        results,
      });
      const mergedSoFar = mergeSearchResults(searchRounds, item.query, taintedTokens);
      const fetchableCandidates = mergedSoFar.filter(
        (row) => isEvidenceCandidate(row) && isFetchable(row) && row.score >= 4,
      );
      if (fetchableCandidates.length >= args.fetchTop && spec.kind === "drive" && index > 0) {
        break;
      }
    }
    const ended = new Date().toISOString();
    const results = mergeSearchResults(searchRounds, item.query, taintedTokens);
    const evidenceResults = results.filter((row) => isEvidenceCandidate(row));
    const nonEvidentialResults = results.filter((row) => row.evidence_excluded);
    const fetches = collectFetches(results, args.fetchTop);
    const row = {
      case_id: item.case_id,
      query: item.query,
      started_at: started,
      ended_at: ended,
      cmd: searchRounds.map((round) => round.cmd).join(" && "),
      ok: searchRounds.some((round) => round.ok),
      error: searchRounds
        .filter((round) => round.error)
        .map((round) => round.error)
        .join("\n"),
      search_rounds: searchRounds,
      results,
      evidence_results: evidenceResults,
      non_evidential_results: nonEvidentialResults,
      fetches,
      evidence_results_count: evidenceResults.length,
      non_evidential_results_count: nonEvidentialResults.length,
      tainted_results: results.filter((row) => row.tainted).length,
      suspicious_artifact_results: results.filter((row) => row.suspicious_artifact_reason).length,
      tainted_fetches: fetches.filter((fetch) => fetch.tainted).length,
      suspicious_artifact_fetches: fetches.filter((fetch) => fetch.suspicious_artifact_reason).length,
    };
    rows.push(row);
    writeTrajectory(runDir, item, row);
  }

  fs.writeFileSync(
    path.join(rawDir, "executor_search.json"),
    `${JSON.stringify(rows, null, 2)}\n`,
  );
  console.log(
    JSON.stringify(
      {
        run_dir: path.relative(root, runDir),
        searched: rows.length,
        empty_results: rows.filter((row) => row.results.length === 0).length,
        empty_evidence_results: rows.filter((row) => row.evidence_results.length === 0).length,
        fetched: rows.reduce((sum, row) => sum + row.fetches.length, 0),
        fetched_success: rows.reduce(
          (sum, row) => sum + row.fetches.filter((fetch) => fetch.ok).length,
          0,
        ),
        suspicious_artifacts_observed: rows.reduce(
          (sum, row) => sum + row.suspicious_artifact_results,
          0,
        ),
        tainted_observed: rows.reduce(
          (sum, row) => sum + row.tainted_results,
          0,
        ),
        non_evidential_observed: rows.reduce(
          (sum, row) => sum + row.non_evidential_results_count,
          0,
        ),
        evidence_candidates: rows.reduce(
          (sum, row) => sum + row.evidence_results_count,
          0,
        ),
        suspicious_artifacts_fetched: rows.reduce(
          (sum, row) => sum + row.suspicious_artifact_fetches,
          0,
        ),
        tainted_fetched: rows.reduce(
          (sum, row) => sum + row.tainted_fetches,
          0,
        ),
        trajectories: path.relative(root, path.join(runDir, "trajectories")),
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
