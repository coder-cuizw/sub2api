-- Migration: 151_init_gateway_forwarding_settings
-- 初始化网关转发防封相关设置的默认值
--
-- 这些设置控制 OAuth 账号的防封行为：
-- 1. enable_fingerprint_unification: 是否统一指纹头（默认启用）
-- 2. enable_metadata_passthrough: 是否透传 metadata.user_id（默认关闭，强制重写）
-- 3. enable_cch_signing: 是否签名 cch 字段（默认启用）
--
-- 所有设置都可通过管理面板动态修改，此脚本仅设置初始默认值。
-- ON CONFLICT ... DO NOTHING：如果已存在则不覆盖，支持手动调整的场景。

INSERT INTO settings (key, value, updated_at)
VALUES
  ('enable_fingerprint_unification', 'true', NOW()),
  ('enable_metadata_passthrough', 'false', NOW()),
  ('enable_cch_signing', 'true', NOW())
ON CONFLICT (key) DO NOTHING;
