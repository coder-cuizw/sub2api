-- Migration: 150_seed_env_tls_fingerprint
-- 内置 TLS 指纹模板：「Linux x64 Node.js v22.22.2」。
-- 采集自 sub2api 实际运行环境（Linux x64 / Node.js v22.22.2，OpenSSL 3.5.5），
-- 作为 Claude 伪装可直接选用的内置指纹之一，无需手动从采集站粘贴 YAML。
--
-- 与早期 Node v22.17.1 采集结果的差异：OpenSSL 3.5 默认启用后量子混合组
--   X25519MLKEM768(4588)，它同时出现在 supported_groups 与 key_share 首位；
--   signature_algorithms 新增 ML-DSA(0x0904/0x0905/0x0906)。
-- 所有数组以 JSONB uint16 十进制存储，顺序敏感（影响 JA3/JA4）。
--
-- ON CONFLICT (name) DO NOTHING：重复执行 / 已手动建过同名模板时不覆盖用户数据。

INSERT INTO tls_fingerprint_profiles (
    name, description, enable_grease,
    cipher_suites, curves, point_formats, signature_algorithms,
    alpn_protocols, supported_versions, key_share_groups, psk_modes, extensions
)
VALUES (
    'Linux x64 Node.js v22.22.2',
    '采集自 sub2api 运行环境（Linux x64 / Node.js v22.22.2，OpenSSL 3.5.5）。含后量子组 X25519MLKEM768。',
    false,
    '[4866,4867,4865,49199,49195,49200,49196,158,49191,103,49192,107,163,159,52393,52392,52394,49325,49311,49245,49249,49239,49235,162,49324,49310,49244,49248,49238,49234,49188,106,49187,64,49162,49172,57,56,49161,49171,51,50,157,49309,49233,156,49308,49232,61,60,53,47]'::jsonb,
    '[4588,29,23,30,24,25,256,257]'::jsonb,
    '[0,1,2]'::jsonb,
    '[2309,2310,2308,1027,1283,1539,2055,2056,2074,2075,2076,2057,2058,2059,2052,2053,2054,1025,1281,1537,771,769,770,1026,1282,1538]'::jsonb,
    '["http/1.1"]'::jsonb,
    '[772,771]'::jsonb,
    '[4588,29]'::jsonb,
    '[1]'::jsonb,
    '[65281,0,11,10,35,16,22,23,13,43,45,51]'::jsonb
)
ON CONFLICT (name) DO NOTHING;
