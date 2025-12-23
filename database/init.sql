-- Workflow Scanner Database Schema
-- Run this after CloudSQL instance is created

-- API Keys table
CREATE TABLE IF NOT EXISTS api_keys (
    id SERIAL PRIMARY KEY,
    api_key VARCHAR(64) UNIQUE NOT NULL,
    subscription_id VARCHAR(255),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    usage_count INTEGER DEFAULT 0,
    usage_limit INTEGER DEFAULT 100,
    is_active BOOLEAN DEFAULT true
);

-- Usage tracking table for detailed logs
CREATE TABLE IF NOT EXISTS api_usage (
    id SERIAL PRIMARY KEY,
    api_key VARCHAR(64) NOT NULL,
    repository VARCHAR(255),
    used_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    success BOOLEAN DEFAULT true,
    FOREIGN KEY (api_key) REFERENCES api_keys(api_key) ON DELETE CASCADE
);

-- Indexes for performance
CREATE INDEX IF NOT EXISTS idx_api_keys_key ON api_keys(api_key);
CREATE INDEX IF NOT EXISTS idx_api_keys_subscription ON api_keys(subscription_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_active ON api_keys(is_active);
CREATE INDEX IF NOT EXISTS idx_api_usage_key ON api_usage(api_key);
CREATE INDEX IF NOT EXISTS idx_api_usage_used_at ON api_usage(used_at);

-- Insert sample data for testing (optional)
-- INSERT INTO api_keys (api_key, subscription_id, usage_count, usage_limit, is_active) 
-- VALUES 
--     ('test_key_123456789abcdef', 'sub_test123', 5, 100, true),
--     ('demo_key_987654321fedcba', 'sub_demo456', 25, 100, true);