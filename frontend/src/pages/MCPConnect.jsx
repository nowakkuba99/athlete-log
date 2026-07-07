import { useState, useEffect } from "react";
import { Copy, Check, ChevronRight, Terminal, Download, Zap, Code2, Bot, MousePointer2 } from "lucide-react";
import { getAuthToken } from "../api/client";

function useMCPEndpoint() {
  return `${window.location.origin}/api/v1/mcp`;
}

function useServerStatus(endpoint) {
  const [ok, setOk] = useState(null);
  useEffect(() => {
    const token = getAuthToken();
    const headers = { "Content-Type": "application/json" };
    if (token) headers["Authorization"] = "Bearer " + token;
    fetch(endpoint, {
      method: "POST",
      headers,
      body: JSON.stringify({ jsonrpc: "2.0", id: 1, method: "tools/list", params: {} }),
    })
      .then((r) => setOk(r.ok))
      .catch(() => setOk(false));
  }, [endpoint]);
  return ok;
}

// ── Primitives ────────────────────────────────────────────────────────────────

function CopyBtn({ value, className = "" }) {
  const [copied, setCopied] = useState(false);
  function copy() {
    navigator.clipboard.writeText(value).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1400);
    });
  }
  return (
    <button
      onClick={copy}
      title={copied ? "Copied!" : "Copy"}
      className={[
        "flex items-center justify-center w-7 h-7 rounded-lg transition-colors shrink-0",
        copied
          ? "text-emerald-500"
          : "text-slate-400 hover:text-slate-600 dark:hover:text-slate-200 hover:bg-slate-100 dark:hover:bg-slate-700",
        className,
      ].join(" ")}
    >
      {copied ? <Check size={13} /> : <Copy size={13} />}
    </button>
  );
}

function CodeBlock({ code }) {
  return (
    <div className="relative group">
      <pre className="text-xs font-mono leading-relaxed bg-slate-950 dark:bg-black text-slate-200 rounded-xl p-4 overflow-x-auto whitespace-pre-wrap break-all pr-10">
        {code}
      </pre>
      <div className="absolute top-2 right-2 opacity-0 group-hover:opacity-100 transition-opacity">
        <CopyBtn value={code} />
      </div>
    </div>
  );
}

function Collapsible({ label, children, defaultOpen = false }) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <div className="pt-2">
      <button
        onClick={() => setOpen((v) => !v)}
        className="flex items-center gap-1.5 text-xs text-slate-400 hover:text-slate-600 dark:hover:text-slate-300 transition-colors"
      >
        <ChevronRight
          size={12}
          className="transition-transform duration-150"
          style={{ transform: open ? "rotate(90deg)" : "rotate(0deg)" }}
        />
        {label}
      </button>
      {open && <div className="mt-3">{children}</div>}
    </div>
  );
}

// ── Action buttons ────────────────────────────────────────────────────────────

function InstallButton({ label, href }) {
  return (
    <a
      href={href}
      className="inline-flex items-center gap-2 px-4 py-2 rounded-xl text-sm font-semibold transition-all btn-accent shrink-0"
      rel="noopener noreferrer"
    >
      {label}
    </a>
  );
}

function DownloadButton() {
  const [state, setState] = useState("idle");
  async function download() {
    setState("loading");
    try {
      const token = getAuthToken();
      const headers = {};
      if (token) headers["Authorization"] = "Bearer " + token;
      const res = await fetch("/api/v1/mcp.mcpb", { headers });
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = "athlete-log.mcpb";
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
      setState("done");
      setTimeout(() => setState("idle"), 3000);
    } catch {
      setState("idle");
    }
  }
  if (state === "done") {
    return (
      <span className="inline-flex items-center gap-2 px-4 py-2 rounded-xl text-sm font-semibold text-emerald-600 dark:text-emerald-400 shrink-0">
        <Check size={14} /> Open the downloaded file
      </span>
    );
  }
  return (
    <button
      onClick={download}
      disabled={state === "loading"}
      className="inline-flex items-center gap-2 px-4 py-2 rounded-xl text-sm font-semibold transition-all btn-accent disabled:opacity-50 shrink-0"
    >
      <Download size={14} />
      {state === "loading" ? "Downloading…" : "Download bundle"}
    </button>
  );
}

// ── Tool row ──────────────────────────────────────────────────────────────────

function ToolRow({ icon, name, badge, description, action, manual, last }) {
  return (
    <div className={`py-5 ${!last ? "border-b border-slate-100 dark:border-slate-800" : ""}`}>
      {/* Name row: icon + name + badge + action button */}
      <div className="flex items-center gap-3">
        <div className="w-8 h-8 rounded-xl bg-slate-100 dark:bg-slate-800 flex items-center justify-center shrink-0 text-slate-500 dark:text-slate-400">
          {icon}
        </div>
        <div className="flex items-center gap-2 flex-1 min-w-0">
          <span className="text-sm font-semibold text-slate-800 dark:text-slate-100 truncate">{name}</span>
          {badge && (
            <span className="text-[10px] font-semibold uppercase tracking-wider px-1.5 py-0.5 rounded-md shrink-0 bg-[color-mix(in_srgb,var(--accent)_12%,transparent)] text-[var(--accent)]">
              {badge}
            </span>
          )}
        </div>
        {action && <div className="shrink-0">{action}</div>}
      </div>
      {/* Description + manual setup, indented to align with name */}
      <div className="ml-11 mt-1.5 space-y-1">
        <p className="text-xs text-slate-400 dark:text-slate-500 leading-relaxed">{description}</p>
        {manual}
      </div>
    </div>
  );
}

// ── Main page ─────────────────────────────────────────────────────────────────

export default function MCPConnect() {
  const endpoint = useMCPEndpoint();
  const token    = getAuthToken();
  const serverOk = useServerStatus(endpoint);

  // Config snippets
  const cursorConfig = JSON.stringify(
    {
      mcpServers: {
        "athlete-log": {
          type: "http",
          url: endpoint,
          ...(token ? { headers: { Authorization: `Bearer ${token}` } } : {}),
        },
      },
    },
    null, 2,
  );

  const claudeDesktopConfig = JSON.stringify(
    {
      mcpServers: {
        "athlete-log": {
          command: "npx",
          args: ["-y", "mcp-remote", endpoint, "--allow-http", "--transport", "http-only"],
          ...(token ? { env: { MCP_REMOTE_AUTH: `Bearer ${token}` } } : {}),
        },
      },
    },
    null, 2,
  );

  const claudeCodeCmd = token
    ? `claude mcp add --scope user --transport http athlete-log ${endpoint} \\\n  --header "Authorization: Bearer ${token}"`
    : `claude mcp add --scope user --transport http athlete-log ${endpoint}`;

  const claudeCodeDeepLink =
    "claude-cli://open?q=" + encodeURIComponent(claudeCodeCmd.replace(/\s*\\\n\s*/g, " "));

  const codexCmd = token
    ? `codex mcp add athlete-log --url ${endpoint} \\\n  --header "Authorization: Bearer ${token}"`
    : `codex mcp add athlete-log --url ${endpoint}`;

  const vsCodeConfig = JSON.stringify(
    {
      "mcp.servers": {
        "athlete-log": {
          type: "http",
          url: endpoint,
          ...(token ? { headers: { Authorization: `Bearer ${token}` } } : {}),
        },
      },
    },
    null, 2,
  );

  const cursorDeepLink =
    "cursor://anysphere.cursor-deeplink/mcp/install?name=athlete-log&config=" +
    encodeURIComponent(btoa(JSON.stringify({
      type: "http",
      url: endpoint,
      ...(token ? { headers: { Authorization: `Bearer ${token}` } } : {}),
    })));

  return (
    <div className="space-y-6 max-w-2xl">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-bold text-slate-800 dark:text-slate-100">Connect AI</h1>
        <p className="text-sm text-slate-400 dark:text-slate-500 mt-1">
          Give Claude, Cursor, or any AI tool direct read access to your training data.
        </p>
      </div>

      {/* Endpoint card */}
      <div className="bg-white dark:bg-slate-900 rounded-2xl border border-slate-200 dark:border-slate-700 p-4 space-y-3">
        <div className="flex items-center justify-between">
          <span className="text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-500">
            MCP Endpoint
          </span>
          {serverOk === true && (
            <span className="flex items-center gap-1.5 text-xs font-medium text-emerald-500">
              <span className="w-1.5 h-1.5 rounded-full bg-emerald-500" />
              Server running
            </span>
          )}
        </div>
        <div className="flex items-center gap-2">
          <code className="flex-1 text-xs font-mono bg-slate-50 dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-xl px-3 py-2.5 text-slate-700 dark:text-slate-300 break-all">
            {endpoint}
          </code>
          <CopyBtn value={endpoint} />
        </div>
        {token && (
          <div className="flex items-center gap-2 text-xs bg-amber-50 dark:bg-amber-900/20 rounded-xl px-3 py-2.5 border border-amber-100 dark:border-amber-800/40">
            <span className="font-semibold text-amber-600 dark:text-amber-400">Auth required</span>
            <span className="text-amber-500/80 dark:text-amber-500/60">— token is embedded in the configs below.</span>
            <CopyBtn value={token} className="ml-auto" />
          </div>
        )}
      </div>

      {/* One-click install */}
      <div className="bg-white dark:bg-slate-900 rounded-2xl border border-slate-200 dark:border-slate-700 overflow-hidden">
        <div className="px-5 pt-4 pb-1 border-b border-slate-100 dark:border-slate-800">
          <p className="text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-500">
            One-click install
          </p>
        </div>
        <div className="px-5">
          <ToolRow
            icon={<MousePointer2 size={16} />}
            name="Cursor"
            badge="1-click"
            description="Opens the Cursor MCP installer — confirm to add Athlete Log."
            action={<InstallButton label="Install in Cursor" href={cursorDeepLink} />}
            manual={
              <Collapsible label="Manual setup">
                <p className="text-xs text-slate-400 dark:text-slate-500 mb-2">Add to <code className="bg-slate-100 dark:bg-slate-800 px-1 rounded">~/.cursor/mcp.json</code>:</p>
                <CodeBlock code={cursorConfig} />
              </Collapsible>
            }
          />
          <ToolRow
            icon={<Bot size={16} />}
            name="Claude Desktop"
            badge="bundle"
            description="Downloads a bundle file — open it and Claude Desktop shows a native install dialog."
            action={<DownloadButton />}
            manual={
              <Collapsible label="Manual setup">
                <p className="text-xs text-slate-400 dark:text-slate-500 mb-2">
                  Open <strong className="text-slate-600 dark:text-slate-300">Settings → Developer → Edit Config</strong> and add to <code className="bg-slate-100 dark:bg-slate-800 px-1 rounded">mcpServers</code>:
                </p>
                <CodeBlock code={claudeDesktopConfig} />
              </Collapsible>
            }
          />
          <ToolRow
            icon={<Zap size={16} />}
            name="Claude Code"
            badge="1-click"
            description="Opens Claude Code with the install command pre-filled — press Enter to confirm."
            action={<InstallButton label="Install in Claude Code" href={claudeCodeDeepLink} />}
            manual={
              <Collapsible label="Manual setup">
                <p className="text-xs text-slate-400 dark:text-slate-500 mb-2">Run in your terminal:</p>
                <CodeBlock code={claudeCodeCmd} />
              </Collapsible>
            }
            last
          />
        </div>
      </div>

      {/* Manual config */}
      <div className="bg-white dark:bg-slate-900 rounded-2xl border border-slate-200 dark:border-slate-700 overflow-hidden">
        <div className="px-5 pt-4 pb-1 border-b border-slate-100 dark:border-slate-800">
          <p className="text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-500">
            Manual config
          </p>
        </div>
        <div className="px-5">
          <ToolRow
            icon={<Code2 size={16} />}
            name="VS Code"
            description="Works with Cline, Continue, GitHub Copilot Agent, and other MCP extensions."
            manual={
              <Collapsible label="Show config" defaultOpen>
                <p className="text-xs text-slate-400 dark:text-slate-500 mb-2">Add to <code className="bg-slate-100 dark:bg-slate-800 px-1 rounded">settings.json</code> or the extension's MCP config file:</p>
                <CodeBlock code={vsCodeConfig} />
              </Collapsible>
            }
          />
          <ToolRow
            icon={<Terminal size={16} />}
            name="Codex (OpenAI CLI)"
            description="Run once in your terminal to register the server globally."
            manual={
              <Collapsible label="Show command" defaultOpen>
                <CodeBlock code={codexCmd} />
              </Collapsible>
            }
            last
          />
        </div>
      </div>

      {/* Footer */}
      <p className="text-xs text-slate-400 dark:text-slate-500 px-1">
        Transport: <strong className="font-medium text-slate-500 dark:text-slate-400">Streamable HTTP</strong>.
        Any MCP-compatible client can connect using the endpoint above.
      </p>
    </div>
  );
}
