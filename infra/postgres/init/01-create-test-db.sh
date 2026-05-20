#!/usr/bin/env bash
# Provision a sibling test database alongside the dev database on first
# Postgres-volume creation. Docker's official `postgres` image runs every
# *.sh / *.sql file in /docker-entrypoint-initdb.d once, only when the data
# directory is empty.
#
# Why this exists: see #183. Sharing one DB between `docker compose up`
# and `make test` lets test fixtures bleed into the dev UI (e.g. categories
# named `AlertCat-50-102333.896245` showing up in the user's dropdown).
# Each env gets its own database; the dev one is created by Postgres from
# POSTGRES_DB, and this script adds the test one.
set -euo pipefail

psql -v ON_ERROR_STOP=1 --username "${POSTGRES_USER}" --dbname "${POSTGRES_DB}" <<-EOSQL
	CREATE DATABASE offbook_test;
	GRANT ALL PRIVILEGES ON DATABASE offbook_test TO ${POSTGRES_USER};
EOSQL
