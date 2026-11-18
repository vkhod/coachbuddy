# agents-python

Builds a class plan from a WOD screenshot and the coach's comment (who attends today, injuries, warm-up wishes), on Google ADK with LiteLLM. Rules and layout: [AGENTS.md](AGENTS.md).

```mermaid
flowchart LR
    P["WodParserAgent<br/>screenshot -> wod"] --> W["WarmupPlannerAgent<br/>(MCP: get_recent_coaching_history)"]
    P --> S["ScalingAgent<br/>tiers, grid, cues"]
    W --> T["TimelineAgent<br/>(MCP: verify_timeline_math)"]
    S --> T
    W --> I["InjuryAgent<br/>only when the comment names injuries"]
    S --> I
```

Each section is checked against the contract in `schema/`, gets one repair, or is reported as failed with the reason. There is no offline or canned mode: every plan comes from a real model with the coach's key.
