-- Seed system categories. Re-runnable: ON CONFLICT on the partial unique
-- index (slug WHERE deleted_at IS NULL) makes this a no-op on a populated DB.
-- Icons are Lucide React names (lucide.dev/icons). Colors are 6-digit hex.

INSERT INTO categories (name, slug, icon, color, is_system) VALUES
    ('Food & Dining',     'food-and-dining',    'utensils',         '#F59E0B', TRUE),
    ('Groceries',         'groceries',          'shopping-cart',    '#84CC16', TRUE),
    ('Transportation',    'transportation',     'car',              '#3B82F6', TRUE),
    ('Gas',               'gas',                'fuel',             '#F97316', TRUE),
    ('Housing',           'housing',            'home',             '#6366F1', TRUE),
    ('Rent/Mortgage',     'rent-mortgage',      'building',         '#8B5CF6', TRUE),
    ('Utilities',         'utilities',          'zap',              '#EAB308', TRUE),
    ('Healthcare',        'healthcare',         'heart-pulse',      '#EC4899', TRUE),
    ('Insurance',         'insurance',          'shield',           '#14B8A6', TRUE),
    ('Entertainment',     'entertainment',      'film',             '#A855F7', TRUE),
    ('Shopping',          'shopping',           'shopping-bag',     '#06B6D4', TRUE),
    ('Clothing',          'clothing',           'shirt',            '#D946EF', TRUE),
    ('Education',         'education',          'graduation-cap',   '#0EA5E9', TRUE),
    ('Personal Care',     'personal-care',      'sparkles',         '#F43F5E', TRUE),
    ('Gifts & Donations', 'gifts-and-donations','gift',             '#10B981', TRUE),
    ('Travel',            'travel',             'plane',            '#22D3EE', TRUE),
    ('Subscriptions',     'subscriptions',      'repeat',           '#6B7280', TRUE),
    ('Fees & Charges',    'fees-and-charges',   'receipt',          '#EF4444', TRUE),
    ('Income',            'income',             'dollar-sign',      '#16A34A', TRUE),
    ('Transfer',          'transfer',           'arrow-left-right', '#64748B', TRUE)
ON CONFLICT (slug) WHERE deleted_at IS NULL DO NOTHING;
