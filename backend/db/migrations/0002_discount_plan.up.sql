-- 测试用 5U 折扣套餐。幂等：重复执行 / 已存在 monthly 等不受影响。
INSERT INTO membership_plans(code, name, duration_days, amount, currency) VALUES
  ('discount5', 'Super 5U 折扣体验', 30, 5.00, 'USD')
ON CONFLICT (code) DO NOTHING;
