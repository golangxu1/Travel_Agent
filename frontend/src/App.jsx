import { useState, useRef, useEffect, useCallback } from "react";
import { ConfigProvider, DatePicker, Select, InputNumber } from "antd";
import ReactMarkdown from "react-markdown";
import dayjs from "dayjs";
import zhCN from "antd/locale/zh_CN";
import "antd/dist/reset.css";
import "./App.css";
import { fetchJSON, jsonRequest, streamSSE } from "./api/client";

const { RangePicker } = DatePicker;

const TRANSPORT_OPTIONS = [
  { label: "🚗 自驾", value: "自驾" },
  { label: "🚇 公共交通", value: "公共交通" },
  { label: "🔀 混合", value: "混合" },
];

const PREFERENCE_OPTIONS = [
  { label: "🍜 美食", value: "美食" },
  { label: "🏛️ 文化", value: "文化" },
  { label: "🏔️ 景色", value: "景色" },
  { label: "🛍️ 购物", value: "购物" },
  { label: "☕ 休闲", value: "休闲" },
  { label: "🧗 探险", value: "探险" },
];

const BUDGET_OPTIONS = [
  { label: "💰 经济", value: "经济" },
  { label: "💳 中等", value: "中等" },
  { label: "💎 豪华", value: "豪华" },
  { label: "🤖 AI自动估算", value: "__auto__" },
];

const MAX_TRIP_DAYS = 31;

const NAV_ITEMS = [
  { key: "destination",   icon: "📍", label: "目的地概览" },
  { key: "weather",       icon: "🌤", label: "天气信息" },
  { key: "accommodation", icon: "🏨", label: "住宿推荐" },
  { key: "itinerary",     icon: "🗓", label: "每日行程" },
  { key: "budget",        icon: "💰", label: "预算明细" },
];

const PHASE_COLORS = { 1: "#3b82f6", 2: "#10b981", 3: "#f59e0b" };
const PHASE_LABELS = { 1: "Phase 1", 2: "Phase 2", 3: "Phase 3" };

function selectStripImages(pool) {
  if (!pool?.length) return [];
  if (pool.length <= 3) return pool.slice(0, 3);
  return [...pool].sort(() => Math.random() - 0.5).slice(0, 3);
}

export default function App() {
  const [origin, setOrigin] = useState("");
  const [destination, setDestination] = useState("");
  const [dateRange, setDateRange] = useState(null);
  const [transport, setTransport] = useState("混合");
  const [preferences, setPreferences] = useState(["景色"]);
  const [people, setPeople] = useState(2);
  const [budgetLevel, setBudgetLevel] = useState(null);

  const [page, setPage] = useState("home");
  const [planData, setPlanData] = useState({});
  const [activeNav, setActiveNav] = useState("destination");
  const [loading, setLoading] = useState(false);
  const [images, setImages] = useState([]);
  const [scenicPool, setScenicPool] = useState([]);
  const [stripImages, setStripImages] = useState([]);

  const [poiData, setPoiData] = useState(null);
  const [activeDay, setActiveDay] = useState(1);

  const [traceId, setTraceId] = useState(null);
  const [traceData, setTraceData] = useState(null);
  const [traceList, setTraceList] = useState([]);

  const [feedbackText, setFeedbackText] = useState("");
  const [regenerating, setRegenerating] = useState(null);
  const [regenDone, setRegenDone] = useState(null);

  const firstStageRef = useRef(false);
  const planAbortRef = useRef(null);
  const regenerateAbortRef = useRef(null);

  const handleSubmit = async () => {
    if (!origin.trim()) return alert("请填写出发地");
    if (!destination.trim()) return alert("请填写目的地");
    if (!dateRange) return alert("请选择出行日期");
    if (preferences.length === 0) return alert("请至少选择一个旅行偏好");
    const tripDays = dateRange[1].diff(dateRange[0], "day") + 1;
    if (tripDays > MAX_TRIP_DAYS) {
      return alert(`行程不能超过 ${MAX_TRIP_DAYS} 天，请重新选择日期`);
    }

    setPlanData({});
    setImages([]);
    setScenicPool([]);
    setStripImages([]);
    setPoiData(null);
    setActiveDay(1);
    setTraceId(null);
    setTraceData(null);
    setRegenDone(null);
    setActiveNav("destination");
    setPage("result");
    setLoading(true);
    firstStageRef.current = false;
    planAbortRef.current?.abort();
    const requestController = new AbortController();
    planAbortRef.current = requestController;

    fetchJSON(`/api/images?query=${encodeURIComponent(destination.trim())}`, { signal: requestController.signal })
      .then((data) => {
        setImages(data.images || []);
        const pool =
          data.scenic_pool?.length > 0
            ? data.scenic_pool
            : (data.images || []).slice(1);
        setScenicPool(pool);
        setStripImages(selectStripImages(pool));
      })
      .catch(() => {});

    try {
      await streamSSE("/api/plan/stream", jsonRequest({
          origin: origin.trim(),
          destination: destination.trim(),
          start_date: dateRange[0].format("YYYY-MM-DD"),
          end_date: dateRange[1].format("YYYY-MM-DD"),
          transport,
          preferences,
          people,
          budget_level: budgetLevel,
        }, requestController.signal), (parsed) => {
          if (parsed.stage === "trace" && parsed.trace_id) {
            setTraceId(parsed.trace_id);
            return;
          }
          if (parsed.stage === "itinerary_pois" && parsed.content) {
            setPoiData(parsed.content);
            return;
          }
          if (parsed.stage && parsed.content) {
            setPlanData((prev) => ({ ...prev, [parsed.stage]: parsed.content }));
            if (!firstStageRef.current) {
              firstStageRef.current = true;
              setActiveNav(parsed.stage);
            }
          }
        });

      setLoading(false);
    } catch (err) {
      if (err.name === "AbortError") return;
      console.error("Stream failed:", err);
      setLoading(false);
      alert("生成失败，请检查后端服务是否正常");
      setPage("home");
    }
  };

  const handleBack = () => {
    planAbortRef.current?.abort();
    regenerateAbortRef.current?.abort();
    planAbortRef.current = null;
    regenerateAbortRef.current = null;
    setPage("home");
    setPlanData({});
    setImages([]);
    setScenicPool([]);
    setStripImages([]);
    setPoiData(null);
    setActiveDay(1);
    setTraceId(null);
    setTraceData(null);
    setRegenDone(null);
    setRegenerating(null);
    setLoading(false);
  };

  const handleRegenerate = async (module, feedback) => {
    if (!feedback.trim() || regenerating) return;
    setRegenerating(module);
    setFeedbackText("");
    regenerateAbortRef.current?.abort();
    const requestController = new AbortController();
    regenerateAbortRef.current = requestController;

    try {
      await streamSSE("/api/plan/regenerate", jsonRequest({
          module,
          feedback: feedback.trim(),
          origin: origin.trim(),
          destination: destination.trim(),
          start_date: dateRange[0].format("YYYY-MM-DD"),
          end_date: dateRange[1].format("YYYY-MM-DD"),
          transport,
          preferences,
          people,
          budget_level: budgetLevel,
          context: planData,
        }, requestController.signal), (parsed) => {
          if (parsed.stage === "itinerary_pois" && parsed.content) {
            setPoiData(parsed.content);
            setActiveDay(1);
            return;
          }
          if (parsed.stage && parsed.content) {
            setPlanData((prev) => ({ ...prev, [parsed.stage]: parsed.content }));
          }
        });
    } catch (err) {
      if (err.name === "AbortError") return;
      console.error("Regenerate failed:", err);
      alert("重新生成失败，请重试");
      setRegenerating(null);
      return;
    }
    const label = NAV_ITEMS.find((n) => n.key === module)?.label || module;
    setRegenerating(null);
    setRegenDone(label);
  };

  const openTrace = useCallback(async (id) => {
    try {
      const data = await fetchJSON(`/api/traces/${encodeURIComponent(id)}`);
      setTraceData(data);
      setPage("trace");
    } catch {
      alert("获取追踪数据失败");
    }
  }, []);

  const openTraceList = useCallback(async () => {
    try {
      const data = await fetchJSON("/api/traces?limit=20");
      setTraceList(data.traces || []);
      setTraceData(null);
      setPage("trace");
    } catch {
      alert("获取追踪列表失败");
    }
  }, []);

  useEffect(() => {
    if (page !== "result" || scenicPool.length === 0) return undefined;
    const id = setInterval(() => {
      setStripImages(selectStripImages(scenicPool));
    }, 6500);
    return () => clearInterval(id);
  }, [page, scenicPool]);

  // ── 首页 ──────────────────────────────────────────────────────────────────
  if (page === "home") {
    return (
      <ConfigProvider locale={zhCN}>
        <div className="app">
          <header className="header">
            <span className="logo">✈️</span>
            <h1>AI 旅游规划助手</h1>
            <p>告诉我你的需求，我来帮你规划完美旅程</p>
          </header>

          <main className="main">
            <section className="form-panel">
              <div className="form-group">
                <label>📍 出发地</label>
                <input className="text-input" placeholder="例如：北京"
                  value={origin} onChange={(e) => setOrigin(e.target.value)} />
              </div>
              <div className="form-group">
                <label>🎯 目的地</label>
                <input className="text-input" placeholder="例如：成都"
                  value={destination} onChange={(e) => setDestination(e.target.value)} />
              </div>
              <div className="form-group">
                <label>📅 出行日期</label>
                <RangePicker style={{ width: "100%" }}
                  disabledDate={(d) => d && d < dayjs().startOf("day")}
                  onChange={setDateRange} placeholder={["出发日期", "返回日期"]} />
              </div>
              <div className="form-group">
                <label>🚌 出行方式</label>
                <div className="radio-group">
                  {TRANSPORT_OPTIONS.map((opt) => (
                    <button key={opt.value}
                      className={`radio-btn ${transport === opt.value ? "active" : ""}`}
                      onClick={() => setTransport(opt.value)}>
                      {opt.label}
                    </button>
                  ))}
                </div>
              </div>
              <div className="form-group">
                <label>❤️ 旅行偏好（可多选）</label>
                <div className="checkbox-group">
                  {PREFERENCE_OPTIONS.map((opt) => (
                    <label key={opt.value} className="checkbox-item">
                      <input type="checkbox"
                        checked={preferences.includes(opt.value)}
                        onChange={(e) => {
                          if (e.target.checked) setPreferences([...preferences, opt.value]);
                          else setPreferences(preferences.filter((p) => p !== opt.value));
                        }} />
                      <span>{opt.label}</span>
                    </label>
                  ))}
                </div>
              </div>
              <div className="form-row">
                <div className="form-group half">
                  <label>👥 出行人数</label>
                  <InputNumber min={1} max={20} value={people}
                    onChange={setPeople} style={{ width: "100%" }} />
                </div>
                <div className="form-group half">
                  <label>💰 预算档次</label>
                  <Select
                    style={{ width: "100%" }}
                    value={budgetLevel ?? "__auto__"}
                    onChange={(value) => setBudgetLevel(value === "__auto__" ? null : value)}
                    options={BUDGET_OPTIONS}
                  />
                </div>
              </div>
              <button className="submit-btn" onClick={handleSubmit}>
                🚀 生成旅行计划
              </button>
            </section>
          </main>

          <footer className="home-footer">
            <button className="trace-link" onClick={openTraceList}>
              📊 查看历史执行追踪
            </button>
          </footer>
        </div>
      </ConfigProvider>
    );
  }

  // ── 追踪面板 ──────────────────────────────────────────────────────────────
  if (page === "trace") {
    const detail = traceData;
    const totalMs = detail?.trace?.total_duration_ms || 1;
    const spans = detail?.spans || [];
    const totalTokensIn = spans.reduce((s, sp) => s + (sp.input_tokens || 0), 0);
    const totalTokensOut = spans.reduce((s, sp) => s + (sp.output_tokens || 0), 0);

    return (
      <ConfigProvider locale={zhCN}>
        <div className="app">
          <header className="header">
            <span className="logo">📊</span>
            <h1>执行追踪面板</h1>
            <p>每个 Agent 的耗时、token、工具调用一目了然</p>
          </header>

          <main className="trace-page">
            <div className="trace-top-bar">
              <button className="back-btn" onClick={() => {
                if (traceData && traceList.length > 0) { setTraceData(null); }
                else { setPage(Object.keys(planData).length > 0 ? "result" : "home"); }
              }}>
                ← 返回
              </button>
              {!detail && (
                <button className="back-btn" onClick={openTraceList} style={{ marginLeft: 8 }}>
                  🔄 刷新
                </button>
              )}
            </div>

            {!detail ? (
              <div className="trace-list-panel">
                <h2>历史追踪记录</h2>
                {traceList.length === 0 ? (
                  <p className="trace-empty">暂无追踪记录，先去生成一次旅行计划吧</p>
                ) : (
                  <table className="trace-table">
                    <thead>
                      <tr>
                        <th>ID</th>
                        <th>路线</th>
                        <th>总耗时</th>
                        <th>时间</th>
                        <th></th>
                      </tr>
                    </thead>
                    <tbody>
                      {traceList.map((t) => (
                        <tr key={t.id}>
                          <td><code>{t.id}</code></td>
                          <td>{t.origin} → {t.destination}</td>
                          <td>{(t.total_duration_ms / 1000).toFixed(1)}s</td>
                          <td>{t.created_at?.replace("T", " ")}</td>
                          <td>
                            <button className="trace-view-btn" onClick={() => openTrace(t.id)}>
                              查看
                            </button>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                )}
              </div>
            ) : (
              <div className="trace-detail-panel">
                <div className="trace-summary">
                  <div className="trace-card">
                    <span className="trace-card-value">{(totalMs / 1000).toFixed(1)}s</span>
                    <span className="trace-card-label">总耗时</span>
                  </div>
                  <div className="trace-card">
                    <span className="trace-card-value">{spans.length}</span>
                    <span className="trace-card-label">Agent 数</span>
                  </div>
                  <div className="trace-card">
                    <span className="trace-card-value">
                      {totalTokensIn + totalTokensOut > 0
                        ? `${((totalTokensIn + totalTokensOut) / 1000).toFixed(1)}k`
                        : "N/A"}
                    </span>
                    <span className="trace-card-label">总 Token</span>
                  </div>
                  <div className="trace-card">
                    <span className="trace-card-value">
                      {detail.trace.origin} → {detail.trace.destination}
                    </span>
                    <span className="trace-card-label">路线</span>
                  </div>
                </div>

                <h3>Agent 执行瀑布图</h3>
                <div className="waterfall">
                  <div className="waterfall-time-axis">
                    {[0, 25, 50, 75, 100].map((pct) => (
                      <span key={pct} style={{ left: `${pct}%` }}>
                        {((totalMs * pct) / 100 / 1000).toFixed(0)}s
                      </span>
                    ))}
                  </div>
                  {spans.map((sp, i) => {
                    const left = (sp.start_offset_ms / totalMs) * 100;
                    const width = Math.max((sp.duration_ms / totalMs) * 100, 3);
                    return (
                      <div key={i} className="waterfall-row">
                        <div className="waterfall-label">
                          {sp.status === "error" ? "❌" : "✅"} {sp.agent_label}
                        </div>
                        <div className="waterfall-track">
                          <div
                            className="waterfall-bar"
                            style={{
                              left: `${left}%`,
                              width: `${width}%`,
                              background: PHASE_COLORS[sp.phase] || "#888",
                            }}
                          >
                            <span>{(sp.duration_ms / 1000).toFixed(1)}s</span>
                          </div>
                        </div>
                      </div>
                    );
                  })}
                </div>

                <h3>详细数据</h3>
                <table className="trace-table">
                  <thead>
                    <tr>
                      <th>Agent</th>
                      <th>阶段</th>
                      <th>耗时</th>
                      <th>输入 Token</th>
                      <th>输出 Token</th>
                      <th>工具调用</th>
                      <th>输出字符</th>
                      <th>状态</th>
                    </tr>
                  </thead>
                  <tbody>
                    {spans.map((sp, i) => (
                      <tr key={i}>
                        <td><strong>{sp.agent_label}</strong></td>
                        <td>
                          <span className="phase-badge" style={{ background: PHASE_COLORS[sp.phase] }}>
                            {PHASE_LABELS[sp.phase]}
                          </span>
                        </td>
                        <td>{(sp.duration_ms / 1000).toFixed(1)}s</td>
                        <td>{sp.input_tokens ?? "—"}</td>
                        <td>{sp.output_tokens ?? "—"}</td>
                        <td>{sp.tool_calls_count ?? "—"}</td>
                        <td>{sp.output_chars?.toLocaleString()}</td>
                        <td>{sp.status === "success" ? "✅" : "❌"}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </main>
        </div>
      </ConfigProvider>
    );
  }

  // ── 结果页（含流式加载状态） ──────────────────────────────────────────────
  const currentContent = planData[activeNav];
  const isTabReady = activeNav in planData;
  const readyCount = NAV_ITEMS.filter((item) => item.key in planData).length;

  const heroStyle = images[0]
    ? {
        backgroundImage: `linear-gradient(135deg, rgba(30,64,175,0.72), rgba(14,165,233,0.55)), url(${images[0].url})`,
        backgroundSize: "cover",
        backgroundPosition: "center",
      }
    : undefined;

  const displayStrip =
    stripImages.length > 0
      ? stripImages
      : images.length > 1
        ? images.slice(1, 4)
        : [];

  return (
    <ConfigProvider locale={zhCN}>
      <div className="app">
        <header className="header header-hero" style={heroStyle}>
          <span className="logo">✈️</span>
          <h1>AI 旅游规划助手</h1>
          <p>
            {origin} → {destination} 旅行方案
            {loading && <span className="header-badge">生成中 {readyCount}/{NAV_ITEMS.length}</span>}
          </p>
        </header>

        {displayStrip.length > 0 && (
          <div className="image-strip">
            {displayStrip.map((img, i) => (
              <a
                key={`${img.link || img.alt}-${i}`}
                href={img.link}
                target="_blank"
                rel="noopener noreferrer"
                className="strip-item"
              >
                <img src={img.thumb} alt={img.alt} loading="lazy" />
                <span className="strip-credit">📸 {img.credit}</span>
              </a>
            ))}
          </div>
        )}

        <main className="result-page">
          <aside className="result-nav">
            <button className="back-btn" onClick={handleBack}>
              ← 重新规划
            </button>
            <nav>
              {NAV_ITEMS.map((item) => {
                const ready = item.key in planData;
                return (
                  <button key={item.key}
                    className={`nav-item ${activeNav === item.key ? "active" : ""}`}
                    onClick={() => setActiveNav(item.key)}>
                    <span className="nav-icon">{item.icon}</span>
                    <span className="nav-label">{item.label}</span>
                    <span className="nav-status">
                      {ready ? "✅" : loading ? "⏳" : ""}
                    </span>
                  </button>
                );
              })}
            </nav>
            {!loading && traceId && (
              <button className="trace-btn" onClick={() => openTrace(traceId)}>
                📊 执行追踪
              </button>
            )}
          </aside>

          <section
            className={`result-content${activeNav === "itinerary" && poiData ? " result-content--itinerary" : ""}`}
          >
            {isTabReady ? (
              <>
                {/* 目的地概览：城市全景图始终是 images[0]（与顶栏 Hero 同源） */}
                {activeNav === "destination" && images[0] && (
                  <div className="destination-map">
                    <img src={images[0].url} alt={images[0].alt || "目的地区域地图"} />
                    <span className="map-caption">🗺️ 目的地区域地图 · 高德地图</span>
                  </div>
                )}

                {/* 行程页：POI 卡片视图 */}
                {activeNav === "itinerary" && poiData ? (
                  <div className="itinerary-visual">
                    {(() => {
                      const days = [...new Set(poiData.pois.map((p) => p.day))].sort((a, b) => a - b);
                      const dayPois = poiData.pois.filter((p) => p.day === activeDay);

                      const dayMarkdownMap = {};
                      if (currentContent) {
                        const sections = currentContent.split(/(?=^## Day\s)/m);
                        for (const sec of sections) {
                          const m = sec.match(/^## Day\s*(\d+)/);
                          if (m) dayMarkdownMap[parseInt(m[1])] = sec.trim();
                        }
                      }
                      const dayText = dayMarkdownMap[activeDay] || "";

                      return (
                        <>
                          <header className="itinerary-head">
                            <p className="itinerary-kicker">行程</p>
                            <div className="day-tabs day-tabs-seg" role="tablist" aria-label="选择出行日">
                              {days.map((d) => (
                                <button
                                  key={d}
                                  type="button"
                                  role="tab"
                                  aria-selected={activeDay === d}
                                  className={`day-tab ${activeDay === d ? "active" : ""}`}
                                  onClick={() => setActiveDay(d)}
                                >
                                  第 {d} 天
                                </button>
                              ))}
                            </div>
                            <p className="itinerary-day-line">
                              本日 <strong>{dayPois.length}</strong> 处 · 导航请用卡片内「高德地图」
                            </p>
                          </header>

                          <ul className="poi-list">
                            {dayPois.map((poi, i) => {
                              const mapSearch = `https://www.amap.com/search?query=${encodeURIComponent(poi.name || "")}`;
                              const num = String(i + 1).padStart(2, "0");
                              return (
                                <li key={i} className="poi-card poi-card-split">
                                  <div className="poi-card-media">
                                    {poi.photo ? (
                                      <img
                                        src={poi.photo}
                                        alt={poi.name}
                                        onError={(e) => {
                                          e.currentTarget.style.display = "none";
                                          const ph = e.currentTarget.nextElementSibling;
                                          if (ph?.classList?.contains("poi-card-placeholder")) {
                                            ph.style.display = "flex";
                                          }
                                        }}
                                      />
                                    ) : null}
                                    <div
                                      className="poi-card-placeholder"
                                      style={{ display: poi.photo ? "none" : "flex" }}
                                      aria-hidden
                                    >
                                      <span className="poi-ph-icon">◆</span>
                                    </div>
                                    <div className="poi-media-foot">
                                      <span className="poi-media-num">{num}</span>
                                      {poi.price && (
                                        <span className={`poi-tag-price ${poi.price === "免费" ? "is-free" : ""}`}>
                                          {poi.price === "免费" ? "免费" : poi.price}
                                        </span>
                                      )}
                                    </div>
                                  </div>
                                  <div className="poi-card-main">
                                    <div className="poi-title-row">
                                      <h4 className="poi-title">{poi.name}</h4>
                                      <a
                                        className="poi-map-link"
                                        href={mapSearch}
                                        target="_blank"
                                        rel="noopener noreferrer"
                                      >
                                        高德地图
                                      </a>
                                    </div>
                                    {(poi.duration || poi.address) && (
                                      <dl className="poi-facts">
                                        {poi.duration && (
                                          <>
                                            <dt>停留</dt>
                                            <dd>{poi.duration}</dd>
                                          </>
                                        )}
                                        {poi.address && (
                                          <>
                                            <dt>地址</dt>
                                            <dd>{poi.address}</dd>
                                          </>
                                        )}
                                      </dl>
                                    )}
                                    {poi.description && (
                                      <p className="poi-blurb">{poi.description}</p>
                                    )}
                                    {poi.map_thumb ? (
                                      <details className="poi-map-fold">
                                        <summary>位置示意图</summary>
                                        <a
                                          className="poi-map-fold-img"
                                          href={mapSearch}
                                          target="_blank"
                                          rel="noopener noreferrer"
                                        >
                                          <img src={poi.map_thumb} alt={`${poi.name} 地图`} loading="lazy" />
                                        </a>
                                      </details>
                                    ) : (
                                      <p className="poi-map-miss">暂无示意图，请用「高德地图」搜索</p>
                                    )}
                                  </div>
                                </li>
                              );
                            })}
                          </ul>

                          {dayText && (
                            <details className="itinerary-text-detail itinerary-text-detail--editorial">
                              <summary>当日完整文字行程</summary>
                              <div className="markdown-result">
                                <ReactMarkdown>{dayText}</ReactMarkdown>
                              </div>
                            </details>
                          )}
                        </>
                      );
                    })()}
                  </div>
                ) : (
                  <div className="markdown-result">
                    <ReactMarkdown>{currentContent}</ReactMarkdown>
                  </div>
                )}
              </>
            ) : loading ? (
              <div className="loading-placeholder">
                <div className="loading-spinner" />
                <p>🤖 AI 正在生成{NAV_ITEMS.find((n) => n.key === activeNav)?.label}...</p>
                <p className="loading-sub">完成后会自动显示，你可以先查看已完成的模块</p>
              </div>
            ) : (
              <div className="loading-placeholder">
                <p>暂无内容</p>
              </div>
            )}

            {/* 重新生成完成提示 */}
            {regenDone && (
              <div key={regenDone} className="regen-done-banner">
                ✅ 「{regenDone}」已根据您的反馈重新生成
              </div>
            )}

            {/* 局部重新生成：反馈输入 */}
            {isTabReady && !loading && (
              <div className="feedback-bar">
                {regenerating === activeNav ? (
                  <div className="feedback-regenerating">
                    <div className="loading-spinner small" />
                    <span>正在根据你的反馈重新生成{NAV_ITEMS.find((n) => n.key === activeNav)?.label}...</span>
                  </div>
                ) : (
                  <>
                    <input
                      className="feedback-input"
                      placeholder={`对${NAV_ITEMS.find((n) => n.key === activeNav)?.label}不满意？请告诉我您的想法...`}
                      value={activeNav === regenerating ? "" : feedbackText}
                      onChange={(e) => setFeedbackText(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter" && !e.shiftKey) {
                          e.preventDefault();
                          handleRegenerate(activeNav, feedbackText);
                        }
                      }}
                      disabled={!!regenerating}
                    />
                    <button
                      className="feedback-btn"
                      onClick={() => handleRegenerate(activeNav, feedbackText)}
                      disabled={!feedbackText.trim() || !!regenerating}
                    >
                      🔄 重新生成
                    </button>
                  </>
                )}
              </div>
            )}
          </section>
        </main>
      </div>
    </ConfigProvider>
  );
}
