# -*- coding: utf-8 -*-
# backend/agents/accommodation_agent.py

from agno.agent import Agent
from agno.tools.mcp import MCPTools
from config import get_model, get_amap_key


def create_accommodation_agent() -> Agent:

    return Agent(
        name="AccommodationAdvisor",
        role="住宿顾问",
        model=get_model(),
        tools=[
            MCPTools(
                command="npx -y @amap/amap-maps-mcp-server",
                env={"AMAP_MAPS_API_KEY": get_amap_key()},
                timeout_seconds=60,
            )
        ],
        instructions=[
            "【重要】你是团队中的住宿顾问，其他同事负责天气、景点、行程、预算。"
            "你只需输出住宿推荐，直接从 ## 标题开始正文。"
            "禁止任何开场白、自我介绍、免责声明、工具调用说明（如'让我搜索''从结果看'）。",

            "最多进行2次工具调用搜索酒店信息，严禁逐个酒店单独查询。",

            "根据预算档次推荐：经济（青旅/经济连锁）、中等（精品酒店/特色民宿）、"
            "豪华（五星/度假村）。考虑景区距离、交通便利度、周边配套。",

            "输出格式：",
            "## 住宿区域推荐（2-3个区域，标注优劣势）",
            "## 精选住宿推荐（每区域1-2家，标注名称、价格、距景区距离、亮点）",
            "## 住宿小贴士（2-3条实用建议）",
            "语言亲切，使用 Markdown。",
        ],
        tool_call_limit=3,
        markdown=True,
    )
