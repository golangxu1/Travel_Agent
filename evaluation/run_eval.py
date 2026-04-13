# -*- coding: utf-8 -*-
"""
AI 旅游规划助手 — 自动化测评脚本
自动跑 10 个测试样本，采集：性能效率 + POI 命中率
"""

import json
import time
import httpx
import asyncio
from datetime import datetime

API_BASE = "http://localhost:8000"

# ── 10 个测试样本 ──────────────────────────────────────────────────────────────

SAMPLES = [
    {"origin": "北京", "destination": "成都", "start_date": "2026-05-01", "end_date": "2026-05-04", "transport": "混合", "preferences": ["美食"], "people": 2, "budget_level": "中等"},
    {"origin": "上海", "destination": "三亚", "start_date": "2026-05-10", "end_date": "2026-05-14", "transport": "混合", "preferences": ["景色"], "people": 2, "budget_level": "豪华"},
    {"origin": "广州", "destination": "西安", "start_date": "2026-05-01", "end_date": "2026-05-03", "transport": "公共交通", "preferences": ["文化"], "people": 2, "budget_level": "经济"},
    {"origin": "深圳", "destination": "丽江", "start_date": "2026-05-05", "end_date": "2026-05-08", "transport": "混合", "preferences": ["休闲"], "people": 2, "budget_level": "中等"},
    {"origin": "杭州", "destination": "重庆", "start_date": "2026-05-01", "end_date": "2026-05-03", "transport": "公共交通", "preferences": ["美食"], "people": 2, "budget_level": "经济"},
    {"origin": "成都", "destination": "北京", "start_date": "2026-05-10", "end_date": "2026-05-14", "transport": "混合", "preferences": ["文化"], "people": 2, "budget_level": "中等"},
    {"origin": "南京", "destination": "厦门", "start_date": "2026-05-01", "end_date": "2026-05-03", "transport": "公共交通", "preferences": ["景色"], "people": 2, "budget_level": "经济"},
    {"origin": "武汉", "destination": "桂林", "start_date": "2026-05-05", "end_date": "2026-05-08", "transport": "自驾", "preferences": ["景色"], "people": 2, "budget_level": "中等"},
    {"origin": "北京", "destination": "大理", "start_date": "2026-05-10", "end_date": "2026-05-14", "transport": "混合", "preferences": ["休闲"], "people": 2, "budget_level": "豪华"},
    {"origin": "上海", "destination": "长沙", "start_date": "2026-05-01", "end_date": "2026-05-03", "transport": "公共交通", "preferences": ["美食"], "people": 2, "budget_level": "经济"},
]


async def run_one_sample(client: httpx.AsyncClient, idx: int, sample: dict) -> dict:
    """跑一个样本，通过 SSE 流式接口采集数据"""
    route = f"{sample['origin']} → {sample['destination']}"
    print(f"\n{'='*60}")
    print(f"[{idx+1}/10] {route} ({sample['start_date']} ~ {sample['end_date']})")
    print(f"{'='*60}")

    result = {
        "index": idx + 1,
        "route": route,
        "stages_received": [],
        "first_stage_time": None,
        "total_time": None,
        "trace_id": None,
        "poi_total": 0,
        "poi_location_hit": 0,
        "poi_photo_hit": 0,
        "poi_address_hit": 0,
        "error": None,
    }

    start = time.time()
    first_stage_time = None

    try:
        async with client.stream(
            "POST",
            f"{API_BASE}/api/plan/stream",
            json=sample,
            timeout=300,
        ) as resp:
            if resp.status_code != 200:
                result["error"] = f"HTTP {resp.status_code}"
                return result

            buffer = ""
            async for chunk in resp.aiter_text():
                buffer += chunk
                lines = buffer.split("\n")
                buffer = lines.pop()

                for line in lines:
                    if not line.startswith("data: "):
                        continue
                    payload = line[6:].strip()
                    if payload == "[DONE]":
                        continue

                    try:
                        parsed = json.loads(payload)
                    except json.JSONDecodeError:
                        continue

                    if parsed.get("error"):
                        print(f"  SSE error: {parsed['error']}")
                        continue

                    # trace_id
                    if parsed.get("stage") == "trace" and parsed.get("trace_id"):
                        result["trace_id"] = parsed["trace_id"]
                        continue

                    # POI 数据
                    if parsed.get("stage") == "itinerary_pois" and parsed.get("content"):
                        pois = parsed["content"].get("pois", [])
                        result["poi_total"] = len(pois)
                        for p in pois:
                            if p.get("location"):
                                result["poi_location_hit"] += 1
                            if p.get("photo"):
                                result["poi_photo_hit"] += 1
                            if p.get("address"):
                                result["poi_address_hit"] += 1
                        print(f"  POI: {len(pois)} 个景点, 坐标命中 {result['poi_location_hit']}, 图片命中 {result['poi_photo_hit']}")
                        continue

                    # 普通模块
                    if parsed.get("stage") and parsed.get("content"):
                        stage = parsed["stage"]
                        result["stages_received"].append(stage)
                        elapsed = round(time.time() - start, 1)
                        if first_stage_time is None:
                            first_stage_time = elapsed
                            result["first_stage_time"] = elapsed
                        print(f"  +{elapsed}s  {stage} 完成 ({len(parsed['content'])} 字)")

    except Exception as e:
        result["error"] = str(e)
        print(f"  ERROR: {e}")

    result["total_time"] = round(time.time() - start, 1)
    print(f"  总耗时: {result['total_time']}s")
    return result


async def fetch_trace_detail(client: httpx.AsyncClient, trace_id: str) -> dict | None:
    """获取追踪详情"""
    try:
        resp = await client.get(f"{API_BASE}/api/traces/{trace_id}", timeout=10)
        if resp.status_code == 200:
            return resp.json()
    except Exception:
        pass
    return None


async def main():
    print("=" * 60)
    print("AI 旅游规划助手 — 自动化测评")
    print(f"开始时间: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    print(f"测试样本: {len(SAMPLES)} 个")
    print("=" * 60)

    # 先检查后端是否可用
    async with httpx.AsyncClient() as client:
        try:
            resp = await client.get(f"{API_BASE}/health", timeout=5)
            if resp.status_code != 200:
                print("ERROR: 后端服务未启动，请先运行后端")
                return
            print("后端服务正常\n")
        except Exception:
            print("ERROR: 无法连接后端服务 (localhost:8000)")
            print("请先启动后端: cd backend && uvicorn main:app --reload")
            return

    results = []

    # 逐个跑（不并行，避免后端压力过大）
    async with httpx.AsyncClient() as client:
        for i, sample in enumerate(SAMPLES):
            result = await run_one_sample(client, i, sample)
            results.append(result)

            # 如果有 trace_id，获取详细 token 数据
            if result.get("trace_id"):
                detail = await fetch_trace_detail(client, result["trace_id"])
                if detail and detail.get("spans"):
                    spans = detail["spans"]
                    result["input_tokens"] = sum(s.get("input_tokens") or 0 for s in spans)
                    result["output_tokens"] = sum(s.get("output_tokens") or 0 for s in spans)
                    result["total_tokens"] = result["input_tokens"] + result["output_tokens"]
                    result["tool_calls"] = sum(s.get("tool_calls_count") or 0 for s in spans)

                    # 各阶段耗时
                    phase1_spans = [s for s in spans if s.get("phase") == 1]
                    phase2_spans = [s for s in spans if s.get("phase") == 2]
                    phase3_spans = [s for s in spans if s.get("phase") == 3]
                    result["phase1_ms"] = max((s.get("duration_ms") or 0) for s in phase1_spans) if phase1_spans else 0
                    result["phase2_ms"] = max((s.get("duration_ms") or 0) for s in phase2_spans) if phase2_spans else 0
                    result["phase3_ms"] = max((s.get("duration_ms") or 0) for s in phase3_spans) if phase3_spans else 0
                    # 串行理论耗时 = 所有 agent 耗时之和
                    result["serial_ms"] = sum((s.get("duration_ms") or 0) for s in spans)

    # ── 汇总报告 ──────────────────────────────────────────────────────────────

    print("\n")
    print("=" * 60)
    print("测评报告")
    print("=" * 60)

    success_results = [r for r in results if not r.get("error")]
    n = len(success_results)

    if n == 0:
        print("所有样本都失败了，请检查后端服务")
        return

    print(f"\n成功样本: {n}/{len(results)}")

    # 性能
    avg_total = sum(r["total_time"] for r in success_results) / n
    avg_first = sum(r.get("first_stage_time") or 0 for r in success_results) / n

    print(f"\n--- 性能效率 ---")
    print(f"平均总耗时:     {avg_total:.1f}s")
    print(f"平均首屏到达:   {avg_first:.1f}s")

    token_results = [r for r in success_results if r.get("total_tokens")]
    if token_results:
        avg_tokens = sum(r["total_tokens"] for r in token_results) / len(token_results)
        avg_input = sum(r["input_tokens"] for r in token_results) / len(token_results)
        avg_output = sum(r["output_tokens"] for r in token_results) / len(token_results)
        avg_tools = sum(r.get("tool_calls") or 0 for r in token_results) / len(token_results)
        print(f"平均Token消耗:  {avg_tokens:.0f} ({avg_input:.0f} in + {avg_output:.0f} out)")
        print(f"平均工具调用:   {avg_tools:.1f} 次")

    # 并行加速比
    speedup_results = [r for r in success_results if r.get("serial_ms") and r.get("total_time")]
    if speedup_results:
        avg_speedup = sum(r["serial_ms"] / (r["total_time"] * 1000) for r in speedup_results) / len(speedup_results)
        print(f"平均并行加速比: {avg_speedup:.2f}x")

    # POI
    poi_results = [r for r in success_results if r["poi_total"] > 0]
    if poi_results:
        total_pois = sum(r["poi_total"] for r in poi_results)
        total_loc = sum(r["poi_location_hit"] for r in poi_results)
        total_photo = sum(r["poi_photo_hit"] for r in poi_results)
        total_addr = sum(r["poi_address_hit"] for r in poi_results)
        print(f"\n--- POI 命中率 ---")
        print(f"总POI数:        {total_pois}")
        print(f"坐标命中率:     {total_loc}/{total_pois} = {total_loc/total_pois*100:.1f}%")
        print(f"图片命中率:     {total_photo}/{total_pois} = {total_photo/total_pois*100:.1f}%")
        print(f"地址命中率:     {total_addr}/{total_pois} = {total_addr/total_pois*100:.1f}%")

    # 各样本明细
    print(f"\n--- 各样本明细 ---")
    print(f"{'编号':<4} {'路线':<16} {'耗时':>6} {'首屏':>6} {'Token':>8} {'POI':>4} {'坐标%':>6} {'状态':<6}")
    print("-" * 60)
    for r in results:
        if r.get("error"):
            print(f"{r['index']:<4} {r['route']:<16} {'—':>6} {'—':>6} {'—':>8} {'—':>4} {'—':>6} {'FAIL':<6}")
        else:
            tok = f"{r.get('total_tokens', 0)//1000}k" if r.get('total_tokens') else "—"
            poi_rate = f"{r['poi_location_hit']/r['poi_total']*100:.0f}%" if r['poi_total'] > 0 else "—"
            print(f"{r['index']:<4} {r['route']:<16} {r['total_time']:>5.1f}s {r.get('first_stage_time', 0):>5.1f}s {tok:>8} {r['poi_total']:>4} {poi_rate:>6} {'OK':<6}")

    # 保存 JSON 结果
    output = {
        "eval_time": datetime.now().isoformat(),
        "sample_count": len(SAMPLES),
        "success_count": n,
        "summary": {
            "avg_total_time_s": round(avg_total, 1),
            "avg_first_stage_s": round(avg_first, 1),
        },
        "results": results,
    }

    if token_results:
        output["summary"]["avg_total_tokens"] = round(avg_tokens)
        output["summary"]["avg_input_tokens"] = round(avg_input)
        output["summary"]["avg_output_tokens"] = round(avg_output)

    if poi_results:
        output["summary"]["poi_total"] = total_pois
        output["summary"]["poi_location_rate"] = round(total_loc / total_pois * 100, 1)
        output["summary"]["poi_photo_rate"] = round(total_photo / total_pois * 100, 1)
        output["summary"]["poi_address_rate"] = round(total_addr / total_pois * 100, 1)

    with open("evaluation/eval_results.json", "w", encoding="utf-8") as f:
        json.dump(output, f, ensure_ascii=False, indent=2)

    print(f"\n详细结果已保存: evaluation/eval_results.json")
    print("测评完成!")


if __name__ == "__main__":
    asyncio.run(main())
