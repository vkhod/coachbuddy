-- The schema file used before Phase 5 step 1, kept only to test that a
-- database created from it passes through the migrations (migrations_test.go).
-- CoachBuddy PostgreSQL Schema Initialization
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- 1. Coaches (Users) Table
CREATE TABLE IF NOT EXISTS coaches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    display_name VARCHAR(120) NOT NULL,
    box_name VARCHAR(160) DEFAULT 'CrossFit Box',
    default_class_duration_min INT NOT NULL DEFAULT 60,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 2. Encrypted BYOK LLM API Keys Table (AES-256-GCM + HKDF)
CREATE TABLE IF NOT EXISTS llm_api_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    coach_id UUID NOT NULL REFERENCES coaches(id) ON DELETE CASCADE,
    provider VARCHAR(50) NOT NULL, -- 'gemini', 'openai', 'anthropic', 'groq'
    encrypted_key BYTEA NOT NULL,
    nonce BYTEA NOT NULL,
    key_hint VARCHAR(32) NOT NULL, -- e.g. '...8f9a'
    is_primary BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(coach_id, provider)
);

-- 3. Per-Agent Model & Key Routing Preferences
CREATE TABLE IF NOT EXISTS agent_model_prefs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    coach_id UUID NOT NULL REFERENCES coaches(id) ON DELETE CASCADE,
    agent_role VARCHAR(50) NOT NULL, -- 'global', 'parser', 'warmup', 'scaling', 'injury', 'timeline'
    provider VARCHAR(50) NOT NULL DEFAULT 'gemini',
    model_identifier VARCHAR(120) NOT NULL DEFAULT 'gemini/gemini-3.8-flash',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(coach_id, agent_role)
);

-- 4. Shared Session Short Links & QR Metadata
CREATE TABLE IF NOT EXISTS shared_links (
    short_code VARCHAR(32) PRIMARY KEY,
    mongo_session_id VARCHAR(64) NOT NULL, -- the plan ID (MongoDB plans._id)
    coach_id UUID NOT NULL REFERENCES coaches(id) ON DELETE CASCADE,
    include_coach_notes BOOLEAN NOT NULL DEFAULT FALSE, -- FALSE: athlete view
    qr_svg TEXT,
    expires_at TIMESTAMPTZ,
    view_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_llm_api_keys_coach ON llm_api_keys(coach_id);
CREATE INDEX IF NOT EXISTS idx_agent_prefs_coach ON agent_model_prefs(coach_id);
CREATE INDEX IF NOT EXISTS idx_shared_links_session ON shared_links(mongo_session_id);
