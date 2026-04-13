# -*- coding: utf-8 -*-
# backend/agents/destination_agent.py

from agno.agent import Agent
from agno.tools.mcp import MCPTools
from config import get_model, get_amap_key


def create_destination_agent() -> Agent:

    return Agent(
        name="DestinationExpert",
        role="目的地专家",
        model=get_model(),
        tools=[
            MCPTools(
                command="npx -y @amap/amap-maps-mcp-server",
                env={"AMAP_MAPS_API_KEY": get_amap_key()},
                timeout_seconds=60,
            )
        ],
        instructions=[
            "【重要】你是团队中的目的地专家，其他同事负责天气、住宿、行程、预算。"
            "你只需输出目的地介绍，直接从 ## 标题开始正文。"
            "禁止任何开场白、自我介绍、免责声明、工具调用说明（如'让我搜索''从结果看'）。",

            "用高德工具搜索当地的热门景区、特色街区、地标建筑。"
            "根据用户的旅行偏好，优先推荐符合偏好的地点。",

            "输出格式：",
            "## 城市印象（3-4句话介绍城市氛围、文化底蕴和独特体验）",
            "## 核心区域（4-5个区域，每个写3-4句：位置、氛围、代表景点、适合什么游客）",
            "## 偏好匹配推荐（8-10个具体地点，标注所属区域、推荐理由和最佳游览时间）",
            "## 当季玩法（结合出行日期，推荐2-3个当季特色活动）",
            "语言生动有温度，像朋友分享经验。使用 Markdown。",
        ],
        tool_call_limit=3,
        markdown=True,
    )