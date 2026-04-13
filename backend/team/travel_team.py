# -*- coding: utf-8 -*-
# backend/team/travel_team.py

from agno.team import Team
from agno.db.sqlite import SqliteDb
from config import get_model
from agents.destination_agent import create_destination_agent
from agents.itinerary_agent import create_itinerary_agent
from agents.weather_agent import create_weather_agent
from agents.budget_agent import create_budget_agent


def create_travel_team() -> Team:

    db = SqliteDb(
        db_url="sqlite:///travel.db",
        session_table="travel_sessions",
    )

    return Team(
        name="TravelAssistant",
        mode="coordinate",
        model=get_model(),

        members=[
            create_weather_agent(),
            create_destination_agent(),
            create_itinerary_agent(),
            create_budget_agent(),
        ],

        db=db,
        add_team_history_to_members=True,
        num_team_history_runs=3,
        add_datetime_to_context=True,
        show_members_responses=True,
        markdown=False,  # 关闭markdown，输出纯JSON

        instructions=[
            # 严格要求输出格式
            "你必须只输出一个JSON对象，JSON前后不能有任何文字，包括问候语、说明、解释等。",
            "不要输出```json、不要输出任何前缀或后缀，直接输出JSON。",

            # 工作流程
            "收到旅行请求后，按顺序协调四个专家：",
            "1. WeatherAdvisor：查询出行日期的天气",
            "2. DestinationExpert：介绍目的地，结合天气和偏好推荐",
            "3. ItineraryPlanner：规划每日行程，参考天气和目的地",
            "4. BudgetAdvisor：估算花销，基于行程计算费用",

            # 输出格式（严格）
            """收到所有专家结果后，整合输出以下JSON格式，使用Markdown格式填充各字段内容：
{
  "destination": "目的地概览的Markdown内容，包含城市介绍、核心区域、偏好匹配推荐",
  "weather": "天气信息的Markdown内容，包含天气概况、逐日天气表格、出行建议",
  "itinerary": "每日行程的Markdown内容，按Day1/Day2格式详细列出",
  "budget": "预算明细的Markdown内容，包含费用分项表格、总计、省钱建议"
}""",

            "每个字段的内容要详细完整，使用Markdown格式（标题、列表、表格等）。",
            "每个字段的内容控制在300字以内，简洁精炼，突出重点，不要长篇大论。",
            "JSON中的字符串值里的换行用 \\n 表示，双引号用 \\\" 转义。",
        ],
    )