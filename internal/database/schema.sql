-- Utilizaremos UUIDs como PK para garantir segurança (anti-enumeração) e 
-- escalabilidade em ambientes distribuídos (sharding).
-- IDs externos (FITID do OFX) continuam como TEXT.

CREATE TABLE IF NOT EXISTS families (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    login         TEXT        NOT NULL UNIQUE,
    password_hash TEXT        NOT NULL,
    family_id     UUID        NOT NULL REFERENCES families(id) ON DELETE CASCADE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Membros de uma família (relação N:N users<->families)
CREATE TABLE IF NOT EXISTS family_members (
    family_id UUID NOT NULL REFERENCES families(id) ON DELETE CASCADE,
    user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (family_id, user_id)
);

CREATE TABLE IF NOT EXISTS tags (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT        NOT NULL,
    color      TEXT        NOT NULL DEFAULT '#6B7280',
    icon       TEXT        NOT NULL DEFAULT 'tag',
    family_id  UUID        REFERENCES families(id) ON DELETE CASCADE, -- NULL = tag de sistema
    is_system  BOOLEAN     NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS accounts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT        NOT NULL,
    institution TEXT        NOT NULL,
    family_id   UUID        NOT NULL REFERENCES families(id) ON DELETE CASCADE,
    created_by  UUID        NOT NULL REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Controle de acesso de conta: se não houver linhas aqui para uma account, ela é pública à família.
-- Se houver linhas, apenas os user_ids listados têm acesso.
CREATE TABLE IF NOT EXISTS account_allowed_users (
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (account_id, user_id)
);

CREATE TABLE IF NOT EXISTS transactions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    fitid       TEXT        NOT NULL,
    type        TEXT        NOT NULL,  -- DEBIT | CREDIT
    date_posted TIMESTAMPTZ NOT NULL,
    amount      NUMERIC(15,2) NOT NULL,
    name        TEXT        NOT NULL DEFAULT '',
    memo        TEXT        NOT NULL DEFAULT '',
    account_id  UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    family_id   UUID        NOT NULL REFERENCES families(id) ON DELETE CASCADE,
    created_by  UUID        NOT NULL REFERENCES users(id),
    imported_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (fitid, account_id, family_id)
);

-- Tags de uma transação (N:N)
CREATE TABLE IF NOT EXISTS transaction_tags (
    transaction_id UUID NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    tag_id         UUID NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (transaction_id, tag_id)
);

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT        NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked    BOOLEAN     NOT NULL DEFAULT false
);

CREATE TABLE IF NOT EXISTS blocklist (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash TEXT        NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL
);

-- Estado serializado do classificador Naive Bayes por família
-- Os mapas (class_docs, class_word_counts, etc.) ficam como JSONB
CREATE TABLE IF NOT EXISTS classifier_states (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    family_id         UUID        UNIQUE, -- NULL = estado global
    total_docs        INT         NOT NULL DEFAULT 0,
    class_docs        JSONB       NOT NULL DEFAULT '{}',
    class_word_counts JSONB       NOT NULL DEFAULT '{}',
    class_total_words JSONB       NOT NULL DEFAULT '{}',
    vocabulary        JSONB       NOT NULL DEFAULT '[]',
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Índices de performance
CREATE INDEX IF NOT EXISTS idx_transactions_family_date ON transactions(family_id, date_posted DESC);
CREATE INDEX IF NOT EXISTS idx_transactions_account ON transactions(account_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_blocklist_expires ON blocklist(expires_at);
CREATE INDEX IF NOT EXISTS idx_tags_family ON tags(family_id);
