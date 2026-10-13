import { useState, useRef, useEffect } from "react";
import { BrainCircuit, X, Send, Loader2 } from "lucide-react";
import { useAISettings } from "../settings";

const PROMPT_CARDS = [
  { label: "Analyze my fitness", prompt: "analyze_fitness", desc: "Trends & areas to improve" },
  { label: "Recovery check",     prompt: "recovery_check",  desc: "Acute vs chronic load" },
  { label: "Predict a race",     prompt: "predict_race",    desc: "Estimate finish time", arg: { name: "distance", placeholder: "e.g. half marathon" } },
  { label: "Training plan",      prompt: "create_training_plan", desc: "Goal-based schedule", arg: { name: "goal", placeholder: "e.g. sub-4h marathon" } },
];

export default function AIPanel() {
  const [open, setOpen] = useState(false);
  const { endpoint, apiKey, model } = useAISettings();

  // Don't render if no endpoint has been configured
  if (!endpoint) return null;
  const [messages, setMessages] = useState([]);
  const [input, setInput]       = useState("");
  const [loading, setLoading]   = useState(false);
  const [argValues, setArgValues] = useState({});
  const bottomRef = useRef(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages, loading]);

  async function fetchMCPPrompt(promptName, args) {
    const token = localStorage.getItem("av_token");
    const headers = { "Content-Type": "application/json" };
    if (token) headers["Authorization"] = "Bearer " + token;

    const res = await fetch("/api/v1/mcp", {
      method: "POST",
      headers,
      body: JSON.stringify({
        jsonrpc: "2.0", id: 1, method: "prompts/get",
        params: { name: promptName, arguments: args },
      }),
    });
    const data = await res.json();
    if (data.error) throw new Error(data.error.message);
    return data.result?.messages?.[0]?.content?.text ?? "";
  }

  async function sendToLLM(systemPrompt) {
    const url = endpoint.replace(/\/$/, "") + "/chat/completions";
    const headers = { "Content-Type": "application/json" };
    if (apiKey) headers["Authorization"] = "Bearer " + apiKey;

    const body = {
      model,
      stream: true,
      messages: [
        { role: "system", content: "You are an expert running coach and data analyst. Be concise and practical." },
        ...messages.map((m) => ({ role: m.role, content: m.content })),
        { role: "user", content: systemPrompt },
      ],
    };

    const res = await fetch(url, { method: "POST", headers, body: JSON.stringify(body) });
    if (!res.ok) throw new Error(`LLM error ${res.status}: ${await res.text()}`);

    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let fullText = "";

    setMessages((prev) => [...prev, { role: "assistant", content: "" }]);

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      const chunk = decoder.decode(value);
      for (const line of chunk.split("\n")) {
        if (!line.startsWith("data: ") || line === "data: [DONE]") continue;
        try {
          const delta = JSON.parse(line.slice(6)).choices?.[0]?.delta?.content ?? "";
          fullText += delta;
          setMessages((prev) => {
            const next = [...prev];
            next[next.length - 1] = { role: "assistant", content: fullText };
            return next;
          });
        } catch { /* ignore malformed SSE */ }
      }
    }
  }

  async function handlePromptCard(card) {
    const args = {};
    if (card.arg) {
      const val = argValues[card.prompt] ?? "";
      if (!val.trim()) return;
      args[card.arg.name] = val.trim();
    }
    setLoading(true);
    try {
      const userPrompt = await fetchMCPPrompt(card.prompt, args);
      setMessages((prev) => [...prev, { role: "user", content: `[${card.label}] ${args[card.arg?.name] ?? ""}`.trim() }]);
      await sendToLLM(userPrompt);
    } catch (err) {
      setMessages((prev) => [...prev, { role: "assistant", content: `Error: ${err.message}` }]);
    } finally {
      setLoading(false);
    }
  }

  async function handleSend(e) {
    e.preventDefault();
    if (!input.trim() || loading) return;
    const userMsg = input.trim();
    setInput("");
    setMessages((prev) => [...prev, { role: "user", content: userMsg }]);
    setLoading(true);
    try {
      await sendToLLM(userMsg);
    } catch (err) {
      setMessages((prev) => [...prev, { role: "assistant", content: `Error: ${err.message}` }]);
    } finally {
      setLoading(false);
    }
  }

  return (
    <>
      {/* Floating trigger button */}
      <button
        onClick={() => setOpen((v) => !v)}
        className="fixed bottom-6 right-6 z-40 w-11 h-11 rounded-full shadow-lg flex items-center justify-center transition-all hover:scale-110 btn-accent"
        title="AI Assistant"
      >
        <BrainCircuit size={18} />
      </button>

      {/* Panel */}
      {open && (
        <div className="fixed bottom-20 right-6 z-40 w-96 max-h-[70vh] flex flex-col rounded-2xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 shadow-2xl overflow-hidden">
          {/* Header */}
          <div className="flex items-center justify-between px-4 py-3 border-b border-slate-100 dark:border-slate-800">
            <div className="flex items-center gap-2">
              <BrainCircuit size={16} style={{ color: "var(--accent)" }} />
              <span className="text-sm font-semibold text-slate-700 dark:text-slate-200">AI Assistant</span>
              <span className="text-xs text-slate-400 dark:text-slate-500">{model}</span>
            </div>
            <button onClick={() => setOpen(false)} className="text-slate-400 hover:text-slate-600 dark:hover:text-slate-300">
              <X size={16} />
            </button>
          </div>

          {/* Messages */}
          <div className="flex-1 overflow-y-auto p-3 space-y-2 min-h-0">
            {messages.length === 0 && !loading && (
              <div className="space-y-2">
                <p className="text-xs text-slate-400 dark:text-slate-500 text-center py-2">Choose a prompt or type a question</p>
                {PROMPT_CARDS.map((card) => (
                  <div key={card.prompt} className="rounded-xl border border-slate-100 dark:border-slate-800 p-3 space-y-2 hover:border-slate-300 dark:hover:border-slate-600 transition-colors">
                    <div className="flex items-center justify-between">
                      <div>
                        <p className="text-xs font-medium text-slate-700 dark:text-slate-200">{card.label}</p>
                        <p className="text-xs text-slate-400 dark:text-slate-500">{card.desc}</p>
                      </div>
                    </div>
                    {card.arg && (
                      <input
                        type="text"
                        value={argValues[card.prompt] ?? ""}
                        onChange={(e) => setArgValues((v) => ({ ...v, [card.prompt]: e.target.value }))}
                        placeholder={card.arg.placeholder}
                        className="w-full text-xs border border-slate-200 dark:border-slate-700 rounded-lg px-2 py-1.5 dark:bg-slate-800 dark:text-slate-100 focus:outline-none focus:ring-1 focus:ring-[var(--accent)]"
                      />
                    )}
                    <button
                      onClick={() => handlePromptCard(card)}
                      disabled={loading || (card.arg && !(argValues[card.prompt] ?? "").trim())}
                      className="w-full text-xs py-1.5 rounded-lg btn-accent disabled:opacity-40 transition-colors"
                    >
                      Run
                    </button>
                  </div>
                ))}
              </div>
            )}

            {messages.map((m, i) => (
              <div key={i} className={`flex ${m.role === "user" ? "justify-end" : "justify-start"}`}>
                <div className={[
                  "max-w-[85%] rounded-xl px-3 py-2 text-xs leading-relaxed whitespace-pre-wrap",
                  m.role === "user"
                    ? "bg-accent text-white"
                    : "bg-slate-100 dark:bg-slate-800 text-slate-700 dark:text-slate-200",
                ].join(" ")}>
                  {m.content || <span className="opacity-50">…</span>}
                </div>
              </div>
            ))}

            {loading && messages[messages.length - 1]?.role !== "assistant" && (
              <div className="flex justify-start">
                <div className="bg-slate-100 dark:bg-slate-800 rounded-xl px-3 py-2">
                  <Loader2 size={14} className="animate-spin text-slate-400" />
                </div>
              </div>
            )}
            <div ref={bottomRef} />
          </div>

          {/* Input */}
          <form onSubmit={handleSend} className="flex gap-2 px-3 py-2 border-t border-slate-100 dark:border-slate-800">
            <input
              type="text"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder="Ask anything about your training…"
              disabled={loading}
              className="flex-1 text-xs border border-slate-200 dark:border-slate-700 rounded-lg px-3 py-2 dark:bg-slate-800 dark:text-slate-100 focus:outline-none focus:ring-1 focus:ring-[var(--accent)] disabled:opacity-50"
            />
            <button
              type="submit"
              disabled={!input.trim() || loading}
              className="w-8 h-8 rounded-lg flex items-center justify-center btn-accent disabled:opacity-40 shrink-0"
            >
              {loading ? <Loader2 size={13} className="animate-spin" /> : <Send size={13} />}
            </button>
          </form>

          {messages.length > 0 && (
            <div className="px-3 pb-2 text-center">
              <button onClick={() => setMessages([])} className="text-xs text-slate-400 hover:text-slate-600 dark:hover:text-slate-300">
                Clear chat
              </button>
            </div>
          )}
        </div>
      )}
    </>
  );
}
