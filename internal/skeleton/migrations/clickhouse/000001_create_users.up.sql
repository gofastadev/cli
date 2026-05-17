-- Users table for ClickHouse.
--
-- ClickHouse does NOT support row-level triggers (it's an OLAP /
-- append-mostly engine; the MergeTree family lacks per-row firing
-- semantics that the other four drivers provide). The three
-- scaffold invariants — updated_at bump, record_version increment,
-- and the not-deletable guard — are therefore enforced at the
-- APPLICATION LAYER ONLY when running on ClickHouse. A direct
-- `clickhouse-client` UPDATE/DELETE will bypass them.
--
-- Choose another driver if DB-level intruder protection is
-- load-bearing for your data.
CREATE TABLE IF NOT EXISTS users (
    id UUID DEFAULT generateUUIDv4(),
    first_name String,
    other_names String,
    email String,
    password String,
    phone_number String,
    is_deletable Bool DEFAULT true,
    is_active Bool DEFAULT true,
    deleted_at Nullable(DateTime),
    created_at DateTime DEFAULT now(),
    updated_at DateTime DEFAULT now(),
    record_version Int64 DEFAULT 1
) ENGINE = MergeTree()
ORDER BY id;
