# -*- coding: utf-8 -*-
import os
import json
import time
import asyncio
import logging
from datetime import date
from pathlib import Path
from dotenv import load_dotenv

load_dotenv(Path(__file__).resolve().parent.parent / ".env", override=True)

import random
from urllib.parse import quote

import httpx
from fastapi import FastAPI, HTTPException
from fastapi.responses import StreamingResponse
from fastapi.middleware.cors import CORSMiddleware

from schemas import PlanRequest, PlanResponse, RegenerateRequest
from agents.destination_agent import create_destination_agent
from agents.weather_agent import create_weather_agent
from agents.accommodation_agent import create_accommodation_agent
from agents.itinerary_agent import create_itinerary_agent
from agents.budget_agent import create_budget_agent
from trace_store import create_trace, add_span, finish_trace, get_traces, get_trace_detail

logger = logging.getLogger(__name__)

app = FastAPI(title="旅游助手 API")

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_methods=["*"],
    allow_headers=["*"],
)


# ── 工具函数 ─────────────────────────────────────────────────────────────────

def build_prompt(req: PlanRequest) -> str:
    days = (
        date.fromisoformat(req.end_date) - date.fromisoformat(req.start_date)
    ).days + 1

    preferences_str = "、".join([p.value for p in req.preferences])
    budget_str = (
        f"预算档次：{req.budget_level.value}"
        if req.budget_level
        else "请根据真实数据估算花销"
    )

    return (
        f"请帮我规划一次旅行，具体信息如下：\n"
        f"- 出发地：{req.origin}\n"
        f"- 目的地：{req.destination}\n"
        f"- 出行日期：{req.start_date} 至 {req.end_date}（共 {days} 天）\n"
        f"- 出行方式：{req.transport.value}\n"
        f"- 旅行偏好：{preferences_str}\n"
        f"- 出行人数：{req.people} 人\n"
        f"- {budget_str}\n"
    )


async def run_agent_traced(agent, prompt, name, label, phase, trace_start):
    """运行 Agent 并收集追踪数据：耗时、token、工具调用次数"""
    start = time.time()
    start_offset = int((start - trace_start) * 1000)
    status = "success"
    input_tokens = output_tokens = tool_calls_count = None

    try:
        result = await agent.arun(prompt)
        content = result.content

        if result.metrics:
            input_tokens = result.metrics.input_tokens
            output_tokens = result.metrics.output_tokens

        if hasattr(result, "messages") and result.messages:
            tc = sum(
                len(getattr(msg, "tool_calls", None) or [])
                for msg in result.messages
            )
            if tc:
                tool_calls_count = tc

    except Exception as e:
        logger.error(f"Agent [{name}] error: {e}", exc_info=True)
        content = f"⚠️ {label}模块生成失败：{e}"
        status = "error"

    duration = int((time.time() - start) * 1000)

    span = {
        "agent_name": name,
        "agent_label": label,
        "phase": phase,
        "start_offset_ms": start_offset,
        "duration_ms": duration,
        "input_tokens": input_tokens,
        "output_tokens": output_tokens,
        "tool_calls_count": tool_calls_count,
        "output_chars": len(content),
        "status": status,
    }

    return content, span


# ── 通用：去掉 LLM 输出中的思考过程 ────────────────────────────────────────────

import re

def _strip_thinking(content: str) -> str:
    """去掉 ## 标题之前的 LLM 内部推理文本。"""
    match = re.search(r"^##\s", content, re.MULTILINE)
    if match and match.start() > 0:
        return content[match.start():].strip()
    return content


# ── POI 解析与补全 ─────────────────────────────────────────────────────────────

def _parse_itinerary(raw: str) -> tuple[str, list[dict]]:
    """从行程输出中分离 Markdown 和 POI 列表，并去掉 LLM 思考过程。"""
    marker = "---POIS---"
    if marker not in raw:
        return raw, []

    parts = raw.split(marker, 1)
    markdown = _strip_thinking(parts[0].strip())
    poi_lines = parts[1].strip()

    pois = []
    for line in poi_lines.splitlines():
        line = line.strip()
        if not line or "|" not in line:
            continue
        cols = line.split("|")
        if len(cols) < 5:
            continue
        try:
            pois.append({
                "day": int(cols[0].strip()),
                "name": cols[1].strip(),
                "duration": cols[2].strip(),
                "price": cols[3].strip(),
                "description": cols[4].strip(),
                "location": "",
                "address": "",
                "photo": "",
            })
        except (ValueError, IndexError):
            continue

    return markdown, pois


async def _enrich_pois(pois: list[dict], city: str, amap_key: str) -> list[dict]:
    """用高德 POI 搜索 API 补全坐标、地址、图片，并生成地图缩略图作为兜底。"""
    async with httpx.AsyncClient(timeout=12) as client:
        async def fetch_one(poi: dict) -> dict:
            def merge_hit(hit: dict) -> None:
                if not hit:
                    return
                loc = hit.get("location", "")
                if loc:
                    poi["location"] = loc
                addr = hit.get("address", "")
                if addr:
                    poi["address"] = addr
                photos = hit.get("photos") or []
                if photos:
                    url = photos[0].get("url", "")
                    if url and not poi.get("photo"):
                        poi["photo"] = f"http://localhost:8000/api/poi-photo?url={quote(url, safe='')}"

            try:
                attempts = [
                    {"keywords": poi["name"], "city": city, "citylimit": "true"},
                    {"keywords": poi["name"], "city": city, "citylimit": "false"},
                    {"keywords": f"{city}{poi['name']}", "city": city, "citylimit": "false"},
                ]
                for extra in attempts:
                    resp = await client.get(
                        "https://restapi.amap.com/v3/place/text",
                        params={
                            **extra,
                            "key": amap_key,
                            "extensions": "all",
                            "offset": "5",
                        },
                    )
                    data = resp.json()
                    if data.get("status") == "1" and data.get("pois"):
                        for hit in data["pois"]:
                            merge_hit(hit)
                            if poi.get("location"):
                                break
                        if poi.get("location"):
                            break

                if not poi.get("location"):
                    g = await client.get(
                        "https://restapi.amap.com/v3/geocode/geo",
                        params={
                            "address": f"{city}{poi['name']}",
                            "key": amap_key,
                            "output": "json",
                        },
                    )
                    gdata = g.json()
                    if gdata.get("status") == "1" and gdata.get("geocodes"):
                        loc = gdata["geocodes"][0].get("location", "")
                        if loc:
                            poi["location"] = loc
            except Exception as e:
                logger.debug(f"POI enrich failed for {poi['name']}: {e}")

            if poi.get("location"):
                loc = poi["location"]
                m = quote(f"large,0x2563EB,1:{loc}", safe="")
                poi["map_thumb"] = (
                    f"https://restapi.amap.com/v3/staticmap"
                    f"?location={loc}&zoom=16&size=640*280&scale=2"
                    f"&markers={m}&key={amap_key}"
                )
            return poi

        return list(await asyncio.gather(*(fetch_one(p) for p in pois)))


def _build_day_maps(pois: list[dict], amap_key: str) -> dict[str, str]:
    """为每一天生成带标注的高德静态地图 URL。"""
    from collections import defaultdict
    days: dict[int, list[dict]] = defaultdict(list)
    for p in pois:
        if p.get("location"):
            days[p["day"]].append(p)

    maps = {}
    for day, day_pois in sorted(days.items()):
        markers_parts = []
        lngs, lats = [], []
        for i, p in enumerate(day_pois, 1):
            loc = p["location"]
            markers_parts.append(f"large,0x2563EB,{i}:{loc}")
            lng, lat = loc.split(",")
            lngs.append(float(lng))
            lats.append(float(lat))

        center_lng = sum(lngs) / len(lngs)
        center_lat = sum(lats) / len(lats)
        spread = max(max(lngs) - min(lngs), max(lats) - min(lats))
        if len(day_pois) == 1:
            zoom = 15
        elif spread < 0.015:
            zoom = 14
        elif spread < 0.05:
            zoom = 13
        elif spread < 0.12:
            zoom = 12
        else:
            zoom = 11

        encoded_markers = [quote(m, safe="") for m in markers_parts]
        marker_params = "&".join(f"markers={m}" for m in encoded_markers)
        maps[str(day)] = (
            f"https://restapi.amap.com/v3/staticmap"
            f"?location={center_lng:.6f},{center_lat:.6f}"
            f"&zoom={zoom}&size=750*400&scale=2"
            f"&{marker_params}&key={amap_key}"
        )

    return maps


# ── 上下文精简 ─────────────────────────────────────────────────────────────────

def _trim_context(content: str, max_chars: int = 600) -> str:
    """截断 Agent 输出，只保留关键信息传给下游，大幅降低 input token。"""
    if len(content) <= max_chars:
        return content
    trimmed = content[:max_chars]
    last_nl = trimmed.rfind("\n")
    if last_nl > max_chars // 2:
        trimmed = trimmed[:last_nl]
    return trimmed + "\n…（详细内容已省略）"


# ── 接口1：流式生成（按 Agent 阶段逐步返回 + 追踪） ──────────────────────────

@app.post("/api/plan/stream")
async def plan_stream(req: PlanRequest):
    prompt = build_prompt(req)

    async def generate():
        trace_id = create_trace(req.origin, req.destination)
        trace_start = time.time()

        try:
            # Phase 1：天气 + 目的地 + 住宿 全部并行启动（住宿不依赖其他 Agent）
            (w_content, w_span), (d_content, d_span), (a_content, a_span) = await asyncio.gather(
                run_agent_traced(create_weather_agent(), prompt, "weather", "天气", 1, trace_start),
                run_agent_traced(create_destination_agent(), prompt, "destination", "目的地", 1, trace_start),
                run_agent_traced(create_accommodation_agent(), prompt, "accommodation", "住宿", 1, trace_start),
            )
            add_span(trace_id, **w_span)
            add_span(trace_id, **d_span)
            add_span(trace_id, **a_span)

            w_content = _strip_thinking(w_content)
            d_content = _strip_thinking(d_content)
            a_content = _strip_thinking(a_content)

            for stage, content in [("weather", w_content), ("destination", d_content), ("accommodation", a_content)]:
                data = json.dumps({"stage": stage, "content": content}, ensure_ascii=False)
                yield f"data: {data}\n\n"

            # Phase 2：行程（只需天气+目的地摘要）
            phase2_context = (
                f"\n\n参考信息摘要：\n"
                f"### 天气\n{_trim_context(w_content, 300)}\n\n"
                f"### 目的地\n{_trim_context(d_content, 500)}"
            )

            i_raw, i_span = await run_agent_traced(
                create_itinerary_agent(), prompt + phase2_context, "itinerary", "行程", 2, trace_start,
            )
            add_span(trace_id, **i_span)

            i_markdown, raw_pois = _parse_itinerary(i_raw)
            i_content = i_markdown or i_raw

            data = json.dumps({"stage": "itinerary", "content": i_content}, ensure_ascii=False)
            yield f"data: {data}\n\n"

            if raw_pois:
                amap_key = os.getenv("AMAP_API_KEY", "").strip()
                if amap_key:
                    enriched = await _enrich_pois(raw_pois, req.destination, amap_key)
                    day_maps = _build_day_maps(enriched, amap_key)
                    poi_payload = {"pois": enriched, "maps": day_maps}
                    data = json.dumps({"stage": "itinerary_pois", "content": poi_payload}, ensure_ascii=False)
                    yield f"data: {data}\n\n"

            # Phase 3：预算（用行程+住宿摘要）
            budget_prompt = (
                f"{prompt}\n\n以下是行程和住宿摘要：\n\n"
                f"### 行程安排\n{_trim_context(i_content, 800)}\n\n"
                f"### 住宿方案\n{_trim_context(a_content, 500)}"
            )

            b_content, b_span = await run_agent_traced(
                create_budget_agent(), budget_prompt, "budget", "预算", 3, trace_start,
            )
            add_span(trace_id, **b_span)
            b_content = _strip_thinking(b_content)

            data = json.dumps({"stage": "budget", "content": b_content}, ensure_ascii=False)
            yield f"data: {data}\n\n"

            # 完成追踪
            total_ms = int((time.time() - trace_start) * 1000)
            finish_trace(trace_id, total_ms)

            data = json.dumps({"stage": "trace", "trace_id": trace_id}, ensure_ascii=False)
            yield f"data: {data}\n\n"

        except Exception as e:
            error = json.dumps({"error": str(e)}, ensure_ascii=False)
            yield f"data: {error}\n\n"

        finally:
            yield "data: [DONE]\n\n"

    return StreamingResponse(generate(), media_type="text/event-stream")


# ── 接口2：非流式生成 ─────────────────────────────────────────────────────────

@app.post("/api/plan", response_model=PlanResponse)
async def plan(req: PlanRequest):
    try:
        prompt = build_prompt(req)
        trace_id = create_trace(req.origin, req.destination)
        trace_start = time.time()

        # Phase 1：天气 + 目的地 + 住宿 全部并行
        (w_content, w_span), (d_content, d_span), (a_content, a_span) = await asyncio.gather(
            run_agent_traced(create_weather_agent(), prompt, "weather", "天气", 1, trace_start),
            run_agent_traced(create_destination_agent(), prompt, "destination", "目的地", 1, trace_start),
            run_agent_traced(create_accommodation_agent(), prompt, "accommodation", "住宿", 1, trace_start),
        )
        add_span(trace_id, **w_span)
        add_span(trace_id, **d_span)
        add_span(trace_id, **a_span)

        w_content = _strip_thinking(w_content)
        d_content = _strip_thinking(d_content)
        a_content = _strip_thinking(a_content)

        # Phase 2：行程
        phase2_context = (
            f"\n\n参考信息摘要：\n"
            f"### 天气\n{_trim_context(w_content, 300)}\n\n"
            f"### 目的地\n{_trim_context(d_content, 500)}"
        )
        i_raw, i_span = await run_agent_traced(
            create_itinerary_agent(), prompt + phase2_context, "itinerary", "行程", 2, trace_start,
        )
        add_span(trace_id, **i_span)

        i_markdown, raw_pois = _parse_itinerary(i_raw)
        i_content = i_markdown or i_raw

        poi_data = None
        if raw_pois:
            amap_key = os.getenv("AMAP_API_KEY", "").strip()
            if amap_key:
                enriched = await _enrich_pois(raw_pois, req.destination, amap_key)
                day_maps = _build_day_maps(enriched, amap_key)
                poi_data = {"pois": enriched, "maps": day_maps}

        # Phase 3：预算
        budget_prompt = (
            f"{prompt}\n\n以下是行程和住宿摘要：\n\n"
            f"### 行程安排\n{_trim_context(i_content, 800)}\n\n"
            f"### 住宿方案\n{_trim_context(a_content, 500)}"
        )
        b_content, b_span = await run_agent_traced(
            create_budget_agent(), budget_prompt, "budget", "预算", 3, trace_start,
        )
        add_span(trace_id, **b_span)
        b_content = _strip_thinking(b_content)

        total_ms = int((time.time() - trace_start) * 1000)
        finish_trace(trace_id, total_ms)

        return PlanResponse(
            success=True,
            destination=d_content,
            weather=w_content,
            accommodation=a_content,
            itinerary=i_content,
            budget=b_content,
            itinerary_pois=poi_data,
        )

    except Exception as e:
        logger.error(f"生成失败：{e}", exc_info=True)
        raise HTTPException(status_code=500, detail=str(e))


# ── 接口3：局部重新生成（只重跑指定模块） ───────────────────────────────────

AGENT_FACTORY = {
    "weather": create_weather_agent,
    "destination": create_destination_agent,
    "accommodation": create_accommodation_agent,
    "itinerary": create_itinerary_agent,
    "budget": create_budget_agent,
}

MODULE_LABELS = {
    "weather": "天气",
    "destination": "目的地",
    "accommodation": "住宿",
    "itinerary": "行程",
    "budget": "预算",
}


@app.post("/api/plan/regenerate")
async def plan_regenerate(req: RegenerateRequest):

    if req.module not in AGENT_FACTORY:
        raise HTTPException(status_code=400, detail=f"不支持的模块: {req.module}")

    plan_req = PlanRequest(
        origin=req.origin,
        destination=req.destination,
        start_date=req.start_date,
        end_date=req.end_date,
        transport=req.transport,
        preferences=req.preferences,
        people=req.people,
        budget_level=req.budget_level,
    )
    base_prompt = build_prompt(plan_req)

    async def generate():
        try:
            prompt = base_prompt

            if req.module == "itinerary":
                w = req.context.get("weather", "")
                d = req.context.get("destination", "")
                if w or d:
                    prompt += (
                        f"\n\n参考信息摘要：\n"
                        f"### 天气\n{_trim_context(w, 300)}\n\n"
                        f"### 目的地\n{_trim_context(d, 500)}"
                    )

            elif req.module == "budget":
                i = req.context.get("itinerary", "")
                a = req.context.get("accommodation", "")
                if i or a:
                    prompt = (
                        f"{base_prompt}\n\n以下是行程和住宿摘要：\n\n"
                        f"### 行程安排\n{_trim_context(i, 800)}\n\n"
                        f"### 住宿方案\n{_trim_context(a, 500)}"
                    )

            prompt += f"\n\n【用户追加要求】请根据以下反馈重新生成：{req.feedback}"

            agent = AGENT_FACTORY[req.module]()
            trace_start = time.time()
            content, span = await run_agent_traced(
                agent, prompt, req.module, MODULE_LABELS[req.module], 0, trace_start,
            )
            content = _strip_thinking(content)

            if req.module == "itinerary":
                i_markdown, raw_pois = _parse_itinerary(content)
                content = i_markdown or content
                data = json.dumps({"stage": "itinerary", "content": content}, ensure_ascii=False)
                yield f"data: {data}\n\n"

                if raw_pois:
                    amap_key = os.getenv("AMAP_API_KEY", "").strip()
                    if amap_key:
                        enriched = await _enrich_pois(raw_pois, req.destination, amap_key)
                        day_maps = _build_day_maps(enriched, amap_key)
                        poi_payload = {"pois": enriched, "maps": day_maps}
                        data = json.dumps({"stage": "itinerary_pois", "content": poi_payload}, ensure_ascii=False)
                        yield f"data: {data}\n\n"
            else:
                data = json.dumps({"stage": req.module, "content": content}, ensure_ascii=False)
                yield f"data: {data}\n\n"

        except Exception as e:
            error = json.dumps({"error": str(e)}, ensure_ascii=False)
            yield f"data: {error}\n\n"

        finally:
            yield "data: [DONE]\n\n"

    return StreamingResponse(generate(), media_type="text/event-stream")


# ── 追踪 API ─────────────────────────────────────────────────────────────────

@app.get("/api/traces")
def list_traces(limit: int = 20):
    return {"traces": get_traces(limit)}


@app.get("/api/traces/{trace_id}")
def trace_detail(trace_id: str):
    detail = get_trace_detail(trace_id)
    if not detail:
        raise HTTPException(status_code=404, detail="Trace not found")
    return detail


# ── POI 图片代理（解决 CORS / 混合内容问题） ──────────────────────────────

from fastapi.responses import Response

@app.get("/api/poi-photo")
async def poi_photo_proxy(url: str):
    try:
        async with httpx.AsyncClient(timeout=10, follow_redirects=True) as client:
            resp = await client.get(url)
            if resp.status_code == 200:
                ct = resp.headers.get("content-type", "image/jpeg")
                return Response(content=resp.content, media_type=ct)
    except Exception:
        pass
    raise HTTPException(status_code=404, detail="Photo not available")


# ── 目的地地图（高德静态地图） ──────────────────────────────────────────────

@app.get("/api/images")
async def search_images(query: str, count: int = 4, pool_limit: int = 24):
    amap_key = os.getenv("AMAP_API_KEY", "").strip()
    if not amap_key:
        return {"images": [], "scenic_pool": [], "location": None}

    try:
        async with httpx.AsyncClient(timeout=12) as client:
            geo_resp = await client.get(
                "https://restapi.amap.com/v3/geocode/geo",
                params={"address": query, "key": amap_key, "output": "json"},
            )
            geo_data = geo_resp.json()
            if geo_data.get("status") != "1" or not geo_data.get("geocodes"):
                return {"images": [], "scenic_pool": [], "location": None}

            location = geo_data["geocodes"][0]["location"]

            # 第一张：城市地图（用于 hero 背景）
            base = "https://restapi.amap.com/v3/staticmap"
            map_params = f"location={location}&zoom=11&size=750*400&scale=2&key={amap_key}"
            map_entry = {
                "url": f"{base}?{map_params}",
                "thumb": f"{base}?location={location}&zoom=11&size=400*200&scale=1&key={amap_key}",
                "alt": f"{query} - 城市全景",
                "credit": "高德地图",
                "link": f"https://www.amap.com/search?query={query}",
            }

            search_configs = [
                {"keywords": "风景名胜", "types": "110000"},
                {"keywords": "景点", "types": "110000"},
                {"keywords": "旅游景区", "types": ""},
            ]
            scenic_pool: list[dict] = []
            seen_names: set[str] = set()

            def add_scenic(name: str, photo_url: str) -> None:
                if name in seen_names or not photo_url:
                    return
                seen_names.add(name)
                enc = quote(photo_url, safe="")
                scenic_pool.append({
                    "url": f"http://localhost:8000/api/poi-photo?url={enc}",
                    "thumb": f"http://localhost:8000/api/poi-photo?url={enc}",
                    "alt": name,
                    "credit": name,
                    "link": f"https://www.amap.com/search?query={name}",
                })

            for cfg in search_configs:
                if len(scenic_pool) >= pool_limit:
                    break
                params = {
                    "keywords": cfg["keywords"],
                    "city": query,
                    "citylimit": "true",
                    "key": amap_key,
                    "extensions": "all",
                    "offset": "25",
                }
                if cfg["types"]:
                    params["types"] = cfg["types"]
                poi_resp = await client.get(
                    "https://restapi.amap.com/v3/place/text", params=params,
                )
                poi_data = poi_resp.json()
                if poi_data.get("status") == "1" and poi_data.get("pois"):
                    for poi in poi_data["pois"]:
                        if len(scenic_pool) >= pool_limit:
                            break
                        name = poi.get("name", query)
                        photos = poi.get("photos") or []
                        if not photos:
                            continue
                        photo_url = photos[0].get("url", "")
                        add_scenic(name, photo_url)

            random.shuffle(scenic_pool)
            strip_n = max(0, min(len(scenic_pool), count - 1))
            images = [map_entry, *scenic_pool[:strip_n]]

        return {"images": images, "scenic_pool": scenic_pool, "location": location}

    except Exception as e:
        logger.error(f"Amap static map error: {e}")
        return {"images": [], "scenic_pool": [], "location": None}


# ── 健康检查 ──────────────────────────────────────────────────────────────────

@app.get("/health")
def health():
    return {"status": "ok"}
