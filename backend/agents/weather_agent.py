# -*- coding: utf-8 -*-
# backend/agents/weather_agent.py

from agno.agent import Agent
from agno.tools.mcp import MCPTools
from config import get_model, get_amap_key


def create_weather_agent() -> Agent:

    return Agent(
        name="WeatherAdvisor",
        role="天气顾问",
        model=get_model(),
        tools=[
            MCPTools(
                command="npx -y @amap/amap-maps-mcp-server",
                env={"AMAP_MAPS_API_KEY": get_amap_key()},
                timeout_seconds=60,
            )
        ],
        instructions=[
            "【重要】你是团队中的天气顾问，其他同事负责景点、住宿、行程、预算。"
            "你只需输出天气相关内容，直接从 ## 标题开始正文。"
            "禁止任何开场白、自我介绍、免责声明、工具调用说明（如'让我查询''从结果看'）。",

            "收到信息后，只用一次工具调用查询该城市的天气预报。",

            "重点关注：降雨（影响户外行程）、温度范围（穿衣指导）、"
            "日温差（超过10度提醒）、极端天气（高温/强风/暴雨）。",
            "天气建议要结合旅行场景：有雨安排室内、晴天安排户外、高温天避开正午。",

            "输出格式：",
            "## 天气概况（1-2句话总结）",
            "## 逐日天气（每天一行：日期·天气·温度·提示）",
            "## 出行建议（穿衣、必带物品、特殊天气应对）",
            "语言简洁有人情味，使用 Markdown。",
        ],
        tool_call_limit=2,

        markdown=True,
    )