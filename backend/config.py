# backend/config.py

import os
from dotenv import load_dotenv

load_dotenv()


def get_model():
    """
    统一的模型获取函数，所有 Agent 都调这里。
    
    使用前请在 .env 中配置：
      MODEL_PROVIDER = openai / deepseek / anthropic / ollama ...
      MODEL_ID       = gpt-4o / deepseek-chat / claude-3-5-sonnet ...
      MODEL_API_KEY  = 你的 key
    """
    provider = os.getenv("MODEL_PROVIDER", "openai").lower()
    model_id = os.getenv("MODEL_ID")
    api_key  = os.getenv("MODEL_API_KEY")

    if not model_id:
        raise ValueError("请在 .env 中配置 MODEL_ID")
    if not api_key:
        raise ValueError("请在 .env 中配置 MODEL_API_KEY")

    if provider == "openai":
        from agno.models.openai import OpenAIChat
        return OpenAIChat(id=model_id, api_key=api_key)

    elif provider == "deepseek":
        from agno.models.deepseek import DeepSeek
        return DeepSeek(id=model_id, api_key=api_key)

    elif provider == "anthropic":
        from agno.models.anthropic import Claude
        return Claude(id=model_id, api_key=api_key)

    else:
        raise ValueError(f"不支持的 MODEL_PROVIDER: {provider}，可选 openai / deepseek / anthropic")


def get_amap_key() -> str:
    """获取高德地图 API Key"""
    amap_key = os.getenv("AMAP_API_KEY")
    if not amap_key:
        raise ValueError("请在 .env 中配置 AMAP_API_KEY")
    return amap_key