-- 测试用 0.5U 折扣套餐。幂等：重复执行 / 已存在 monthly 等不受影响。
INSERT INTO membership_plans(code, name, duration_days, amount, currency) VALUES
  ('discount5', 'Super 0.5U 折扣体验', 30, 0.50, 'USD')
ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name, amount = EXCLUDED.amount;
