# -*- coding: utf-8 -*-
# backend/schemas.py

from pydantic import BaseModel, Field
from typing import Any, List, Optional
from enum import Enum


# ── 枚举类 ────────────────────────────────────────────────────────────────────

class TransportMode(str, Enum):
    SELF_DRIVE = "自驾"
    PUBLIC = "公共交通"
    MIXED = "混合"


class TravelStyle(str, Enum):
    FOOD = "美食"
    CULTURE = "文化"
    SCENERY = "景色"
    SHOPPING = "购物"
    RELAXATION = "休闲"
    ADVENTURE = "探险"


class BudgetLevel(str, Enum):
    ECONOMY = "经济"
    MIDDLE = "中等"
    LUXURY = "豪华"


# ── 请求体 ────────────────────────────────────────────────────────────────────

class PlanRequest(BaseModel):
    origin: str = Field(description="出发地")
    destination: str = Field(description="目的地")
    start_date: str = Field(description="出行开始日期 YYYY-MM-DD")
    end_date: str = Field(description="出行结束日期 YYYY-MM-DD")
    transport: TransportMode = Field(default=TransportMode.MIXED)
    preferences: List[TravelStyle] = Field(default=[TravelStyle.SCENERY])
    people: int = Field(default=2, ge=1, le=20)
    budget_level: Optional[BudgetLevel] = Field(default=None)


# ── 响应体：五个独立字段，对应五个 Agent ─────────────────────────────────────

class PlanResponse(BaseModel):
    success: bool

    destination: Optional[str] = Field(
        default=None,
        description="目的地概览：城市介绍、核心区域、偏好匹配推荐",
    )
    weather: Optional[str] = Field(
        default=None,
        description="天气信息：天气概况、逐日天气、出行建议",
    )
    accommodation: Optional[str] = Field(
        default=None,
        description="住宿推荐：区域推荐、精选住宿、住宿贴士",
    )
    itinerary: Optional[str] = Field(
        default=None,
        description="每日行程：按天规划的详细行程安排",
    )
    budget: Optional[str] = Field(
        default=None,
        description="预算明细：费用分项、总计、省钱建议",
    )
    itinerary_pois: Optional[Any] = Field(
        default=None,
        description="行程景点结构化数据：含坐标、图片、地图 URL",
    )

    error: Optional[str] = Field(default=None)


# ── 局部重新生成 ──────────────────────────────────────────────────────────────

class RegenerateRequest(BaseModel):
    module: str = Field(description="要重新生成的模块: weather/destination/accommodation/itinerary/budget")
    feedback: str = Field(description="用户的修改意见")
    origin: str = Field(description="出发地")
    destination: str = Field(description="目的地")
    start_date: str = Field(description="出行开始日期 YYYY-MM-DD")
    end_date: str = Field(description="出行结束日期 YYYY-MM-DD")
    transport: TransportMode = Field(default=TransportMode.MIXED)
    preferences: List[TravelStyle] = Field(default=[TravelStyle.SCENERY])
    people: int = Field(default=2, ge=1, le=20)
    budget_level: Optional[BudgetLevel] = Field(default=None)
    context: dict = Field(default={}, description="其他模块的已有输出，供依赖模块参考")