# -*- coding: utf-8 -*-
# backend/tools/budget_tools.py

from agno.tools import tool
from typing import Optional


# ── 费用基准表 ────────────────────────────────────────────────────────────────
# 每人每天的参考费用，分三个档次，单位：人民币（元）
# 这里的数字是国内旅游的经验均值，可根据实际情况调整

BUDGET_TEMPLATE = {
    "经济": {
        "住宿":     150,   # 青旅/经济型酒店
        "餐饮":     80,    # 街边小吃/快餐
        "市内交通": 30,    # 公交/地铁为主
        "景点门票": 60,    # 选择性进景区
        "购物杂费": 50,
    },
    "中等": {
        "住宿":     400,   # 连锁酒店/精品民宿
        "餐饮":     150,   # 当地特色餐厅
        "市内交通": 60,    # 地铁+偶尔打车
        "景点门票": 120,
        "购物杂费": 150,
    },
    "豪华": {
        "住宿":     1200,  # 四五星酒店
        "餐饮":     400,   # 品质餐厅
        "市内交通": 200,   # 专车/出租为主
        "景点门票": 200,
        "购物杂费": 500,
    },
}

# 大交通往返参考费用（人均，按档次区分）
TRANSPORT_TEMPLATE = {
    "经济": 500,    # 火车/经济舱机票
    "中等": 1200,   # 普通机票
    "豪华": 3500,   # 商务舱/高铁一等座
}


# ── 工具函数 ──────────────────────────────────────────────────────────────────

@tool(
    name="estimate_budget_by_template",
    description=(
        "当无法从真实数据获取花销时，根据目的地、天数、人数、"
        "消费档次估算旅行总预算，返回详细分项明细。"
        "消费档次可选：经济、中等、豪华。"
    ),
)
def estimate_budget_by_template(
    destination: str,
    days: int,
    people: int,
    budget_level: Optional[str] = "中等",
) -> str:
    """
    Args:
        destination:  目的地名称，如「成都」「云南」
        days:         出行天数
        people:       出行人数
        budget_level: 消费档次，「经济」/「中等」/「豪华」，默认「中等」
    """

    # 档次容错：传入值不在模板里时回退到「中等」
    if budget_level not in BUDGET_TEMPLATE:
        budget_level = "中等"

    daily_costs = BUDGET_TEMPLATE[budget_level]

    # 每项费用 = 单价 × 天数 × 人数
    breakdown = {
        item: price * days * people
        for item, price in daily_costs.items()
    }

    # 大交通单独计算：只乘人数，不乘天数（往返是固定花销）
    transport_per_person = TRANSPORT_TEMPLATE[budget_level]
    breakdown["大交通（往返）"] = transport_per_person * people

    # 应急备用金：总额的 10%
    subtotal = sum(breakdown.values())
    breakdown["应急备用金（10%）"] = int(subtotal * 0.1)

    total = sum(breakdown.values())

    # ── 拼接输出 ──────────────────────────────────────────────────────────────
    lines = [
        f"📊 {destination} 预算估算（{budget_level}档次）",
        f"   {people} 人 · {days} 天",
        f"{'─' * 32}",
    ]

    for item, amount in breakdown.items():
        # 每人每天均摊（大交通和备用金不参与均摊显示）
        if item in ("大交通（往返）", "应急备用金（10%）"):
            lines.append(f"  {item:<14} ¥{amount:>7,}")
        else:
            per_day = amount // (days * people)
            lines.append(f"  {item:<14} ¥{amount:>7,}  （¥{per_day}/人/天）")

    lines += [
        f"{'─' * 32}",
        f"  {'预估总费用':<14} ¥{total:>7,}",
        f"  {'人均费用':<14} ¥{total // people:>7,}",
        "",
        "⚠️  此为模板估算，仅供参考。实际费用以高德查询结果为准。",
    ]

    return "\n".join(lines)